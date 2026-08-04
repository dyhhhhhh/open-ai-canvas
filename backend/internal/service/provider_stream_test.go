package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestRunTextTaskStreamHandlesResponsesAndChatCompletionsEvents(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	tests := []struct {
		name          string
		interfaceType string
		path          string
		events        string
	}{
		{
			name:          "responses",
			interfaceType: string(model.ChannelInterfaceOpenAIResponse),
			path:          "/v1/responses",
			events: "event: response.output_text.delta\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"第一\"}\n\n" +
				"event: response.output_text.delta\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"段\"}\n\n" +
				"data: [DONE]\n\n",
		},
		{
			name:          "chat completions",
			interfaceType: string(model.ChannelInterfaceChatCompletion),
			path:          "/v1/chat/completions",
			events: "data: {\"choices\":[{\"delta\":{\"content\":\"第一\"}}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{\"content\":\"段\"}}]}\n\n" +
				"data: [DONE]\n\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != test.path {
					t.Errorf("path = %q, want %q", request.URL.Path, test.path)
				}
				var body map[string]any
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if stream, _ := body["stream"].(bool); !stream {
					t.Fatal("stream flag was not sent")
				}
				writer.Header().Set("Content-Type", "text/event-stream")
				_, _ = writer.Write([]byte(test.events))
			}))
			defer server.Close()

			var chunks []string
			result, err := runTextTaskStream(context.Background(), canvasGenerationInput{
				Mode:   "text",
				Prompt: "写一句话",
				Config: providerConfig{BaseURL: server.URL + "/v1", APIKey: "test-key", Model: "test-model", InterfaceType: test.interfaceType},
			}, func(delta string) error {
				chunks = append(chunks, delta)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(chunks, ""); got != "第一段" {
				t.Fatalf("deltas = %q", got)
			}
			if text, _ := result["text"].(string); text != "第一段" {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestProcessCanvasTextTaskFlushesPartialDraftBeforeStreamCompletes(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	firstDeltaSent := make(chan struct{})
	finishStream := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"已保存的草稿\"}}]}\n\n")
		writer.(http.Flusher).Flush()
		close(firstDeltaSent)
		<-finishStream
		_, _ = fmt.Fprint(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"续写\"}}]}\n\n")
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()

	svc, db := newTaskStreamTestService(t)
	input, err := json.Marshal(canvasGenerationInput{
		Mode:   "text",
		Prompt: "测试可恢复文本",
		Config: providerConfig{BaseURL: server.URL + "/v1", APIKey: "test-key", Model: "test-model", InterfaceType: string(model.ChannelInterfaceChatCompletion)},
	})
	if err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "streaming-text-task", UserID: "user-1", Type: "canvas_text", Status: model.TaskStatusRunning, Prompt: "测试可恢复文本", InputJSON: string(input), Attempts: 1}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	type completion struct {
		result map[string]interface{}
		err    error
	}
	done := make(chan completion, 1)
	go func() {
		result, runErr := svc.processCanvasTextGenerationTask(context.Background(), task)
		done <- completion{result: result, err: runErr}
	}()
	<-firstDeltaSent

	deadline := time.Now().Add(2 * time.Second)
	for {
		stream, streamErr := svc.TaskTextStream(task.UserID, task.ID, 0)
		if streamErr == nil && len(stream.Chunks) == 1 && stream.Chunks[0].Delta == "已保存的草稿" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("partial draft was not persisted before stream completion: stream=%#v error=%v", stream, streamErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
	close(finishStream)
	completed := <-done
	if completed.err != nil {
		t.Fatal(completed.err)
	}
	if text, _ := completed.result["text"].(string); text != "已保存的草稿续写" {
		t.Fatalf("result = %#v", completed.result)
	}
}
