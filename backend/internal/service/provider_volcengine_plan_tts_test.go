package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunVolcenginePlanTTSTask(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v3/plan/tts/unidirectional" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "test-key" {
			t.Fatalf("X-Api-Key = %q", got)
		}
		if got := r.Header.Get("X-Api-Resource-Id"); got != "seed-tts-2.0" {
			t.Fatalf("X-Api-Resource-Id = %q", got)
		}
		if got := r.Header.Get("X-Api-Sequence"); got != "1" {
			t.Fatalf("X-Api-Sequence = %q", got)
		}
		if !strings.Contains(r.Header.Get("X-Api-Request-Id"), "-") {
			t.Fatalf("X-Api-Request-Id = %q", r.Header.Get("X-Api-Request-Id"))
		}
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		params := body["req_params"].(map[string]interface{})
		if params["speaker"] != volcenginePlanTTSDefaultSpeaker {
			t.Fatalf("speaker = %q", params["speaker"])
		}
		if params["text"] != "你好" {
			t.Fatalf("text = %q", params["text"])
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(volcenginePlanTTSResponse{Code: 0, Data: base64.StdEncoding.EncodeToString([]byte("ID3test-mp3"))})
	}))
	defer server.Close()

	result, err := runVolcenginePlanTTSTask(context.Background(), canvasGenerationInput{
		Prompt: "你好",
		Config: providerConfig{BaseURL: server.URL, APIKey: "test-key", Model: "seed-tts-2.0", AudioVoice: "alloy", AudioFormat: "mp3"},
	})
	if err != nil {
		t.Fatalf("runVolcenginePlanTTSTask() error = %v", err)
	}
	audio := result["audio"].(map[string]interface{})
	if !strings.HasPrefix(audio["dataUrl"].(string), "data:audio/mpeg;base64,") {
		t.Fatalf("dataUrl = %q", audio["dataUrl"])
	}
}

func TestVolcenginePlanTTSSpeedAndURL(t *testing.T) {
	if got := volcenginePlanTTSSpeechRate("1.2"); got != 20 {
		t.Fatalf("speed = %d", got)
	}
	if got := volcenginePlanTTSSpeechRate("0.1"); got != -50 {
		t.Fatalf("slow speed = %d", got)
	}
	if got := volcenginePlanTTSURL("https://ark.cn-beijing.volces.com/api/plan/v3"); got != volcenginePlanTTSEndpoint {
		t.Fatalf("Ark Plan URL = %q", got)
	}
}

func TestVolcenginePlanTTSAudioDataAcceptsPlainBase64(t *testing.T) {
	data, err := volcenginePlanTTSAudioData([]byte(base64.StdEncoding.EncodeToString([]byte("ID3test-mp3"))))
	if err != nil {
		t.Fatalf("volcenginePlanTTSAudioData() error = %v", err)
	}
	if string(data) != "ID3test-mp3" {
		t.Fatalf("data = %q", data)
	}
}
