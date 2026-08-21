package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const miniMaxH3VideoPollInterval = 5 * time.Second

// runMiniMaxH3VideoTask keeps the H3 protocol separate from generic NewAPI
// video requests: H3 accepts public references as JSON and uploaded files as
// multipart data under different field names.
func runMiniMaxH3VideoTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	id := resumedProviderRequestID(ctx)
	if id == "" {
		var err error
		id, err = submitMiniMaxH3VideoTask(ctx, input)
		if err != nil {
			return nil, err
		}
	}
	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		result, status, err := queryMiniMaxH3VideoTask(ctx, input, id)
		if err != nil {
			return nil, err
		}
		if result != nil {
			return result, nil
		}
		if status == "failed" || status == "cancelled" || status == "canceled" {
			return nil, fmt.Errorf("MiniMax H3 视频生成失败（任务 %s）", id)
		}
		if err := sleepContext(ctx, miniMaxH3VideoPollInterval); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("MiniMax H3 视频生成超时（任务 %s）", id)
}

func submitMiniMaxH3VideoTask(ctx context.Context, input canvasGenerationInput) (string, error) {
	if strings.TrimSpace(input.Config.Model) != "minimax_h3" {
		return "", errors.New("MiniMax H3 渠道的模型标识必须是 minimax_h3")
	}
	if len(input.ReferenceImages) > 9 || len(input.ReferenceVideos) > 3 || len(input.ReferenceAudios) > 3 {
		return "", errors.New("MiniMax H3 最多支持 9 张参考图、3 段参考视频和 3 段参考音频")
	}
	if strings.TrimSpace(input.Prompt) == "" {
		return "", errors.New("MiniMax H3 视频提示词不能为空")
	}
	if miniMaxH3FirstLastFrame(input) {
		if len(input.ReferenceImages) != 2 {
			return "", errors.New("MiniMax H3 首尾帧模式必须且只能传入 2 张参考图")
		}
		if len(input.ReferenceVideos) != 0 || len(input.ReferenceAudios) != 0 {
			return "", errors.New("MiniMax H3 首尾帧模式不能同时传入参考视频或参考音频")
		}
	}
	seconds, err := miniMaxH3Seconds(input.Config.VideoSeconds)
	if err != nil {
		return "", err
	}

	var created map[string]interface{}
	if miniMaxH3CanUseJSON(input) {
		if err := postJSON(ctx, input.Config, "/videos", miniMaxH3JSONRequest(input, seconds), &created); err != nil {
			return "", err
		}
	} else {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		writeField(writer, "model", "minimax_h3")
		writeField(writer, "prompt", strings.TrimSpace(input.Prompt))
		writeField(writer, "prompt_enhance", strconv.FormatBool(miniMaxH3PromptEnhance(input)))
		writeField(writer, "seconds", strconv.Itoa(seconds))
		writeField(writer, "size", miniMaxH3Size(input.Config.Size, input.Config.VQuality))
		if miniMaxH3FirstLastFrame(input) {
			writeField(writer, "mode", "first_last_frame")
		}
		imageField := "input_reference"
		if miniMaxH3FirstLastFrame(input) {
			imageField = "images"
		}
		for _, image := range input.ReferenceImages {
			if err := writeMiniMaxH3Reference(writer, imageField, image); err != nil {
				return "", fmt.Errorf("读取 MiniMax H3 参考图片失败：%w", err)
			}
		}
		for _, video := range input.ReferenceVideos {
			if err := writeMiniMaxH3Reference(writer, "reference_video", video); err != nil {
				return "", fmt.Errorf("读取 MiniMax H3 参考视频失败：%w", err)
			}
		}
		for _, audio := range input.ReferenceAudios {
			if err := writeMiniMaxH3Reference(writer, "reference_audio", audio); err != nil {
				return "", fmt.Errorf("读取 MiniMax H3 参考音频失败：%w", err)
			}
		}
		if err := writer.Close(); err != nil {
			return "", err
		}
		if err := postForm(ctx, input.Config, "/videos", writer.FormDataContentType(), body, &created); err != nil {
			return "", err
		}
	}
	if data, ok := created["data"].(map[string]interface{}); ok {
		created = data
	}
	id := firstNonEmptyString(stringField(created, "id"), stringField(created, "task_id"), stringField(created, "request_id"))
	if id == "" {
		return "", errors.New("MiniMax H3 接口没有返回任务 ID")
	}
	return id, nil
}

func queryMiniMaxH3VideoTask(ctx context.Context, input canvasGenerationInput, id string) (map[string]interface{}, string, error) {
	var payload map[string]interface{}
	if err := getJSON(ctx, input.Config, "/videos/"+url.PathEscape(id), &payload); err != nil {
		return nil, "", err
	}
	state := payload
	if data, ok := payload["data"].(map[string]interface{}); ok {
		state = data
	}
	status := strings.ToLower(strings.TrimSpace(stringField(state, "status")))
	switch status {
	case "completed", "succeeded", "success", "done":
		if resultURL := miniMaxH3ResultURL(state); resultURL != "" {
			data, mimeType, err := getProviderExternalBinary(withProviderRequestKind(ctx, "download"), input.Config, resultURL)
			if err == nil {
				mimeType = normalizedMediaMimeType(mimeType, data)
				return map[string]interface{}{"mode": "video", "video": map[string]interface{}{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType}}, status, nil
			}
			content, contentMimeType, contentErr := getBinary(ctx, input.Config, "/videos/"+url.PathEscape(id)+"/content")
			if contentErr == nil {
				contentMimeType = normalizedMediaMimeType(contentMimeType, content)
				return map[string]interface{}{"mode": "video", "video": map[string]interface{}{"dataUrl": dataURL(contentMimeType, content), "mimeType": contentMimeType}}, status, nil
			}
			return nil, status, fmt.Errorf("MiniMax H3 视频结果下载失败（任务 %s）：%w；内容接口回退失败：%v", id, err, contentErr)
		}
		data, mimeType, err := getBinary(ctx, input.Config, "/videos/"+url.PathEscape(id)+"/content")
		if err != nil {
			return nil, status, fmt.Errorf("MiniMax H3 任务 %s 已完成但无法下载成片：%w", id, err)
		}
		mimeType = normalizedMediaMimeType(mimeType, data)
		return map[string]interface{}{"mode": "video", "video": map[string]interface{}{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType}}, status, nil
	case "failed", "cancelled", "canceled":
		return nil, status, errors.New(defaultString(miniMaxH3FailureMessage(state), "MiniMax H3 视频生成失败"))
	default:
		return nil, status, nil
	}
}

func miniMaxH3Seconds(value string) (int, error) {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds < 1 || seconds > 300 {
		return 0, errors.New("MiniMax H3 视频时长必须在 1 到 300 秒之间")
	}
	return seconds, nil
}

func miniMaxH3Size(value string, quality string) string {
	value = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "×", "x")))
	if strings.Contains(value, "x") {
		return value
	}
	isHighResolution := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(quality)), "p") == "1080"
	if isHighResolution {
		switch value {
		case "1:1":
			return "1440x1440"
		case "2:3":
			return "1184x1760"
		case "3:2":
			return "1760x1184"
		case "3:4":
			return "1248x1664"
		case "4:3":
			return "1664x1248"
		case "9:16":
			return "1088x1920"
		case "21:9":
			return "2208x960"
		default:
			return "1920x1080"
		}
	}
	switch value {
	case "1:1":
		return "1024x1024"
	case "2:3":
		return "832x1248"
	case "3:2":
		return "1248x832"
	case "3:4":
		return "896x1184"
	case "4:3":
		return "1184x896"
	case "9:16":
		return "768x1376"
	case "21:9":
		return "1568x672"
	default:
		return "1376x768"
	}
}

func miniMaxH3PromptEnhance(input canvasGenerationInput) bool {
	if input.Config.VideoPromptEnhance != "" {
		value, err := strconv.ParseBool(strings.TrimSpace(input.Config.VideoPromptEnhance))
		return err == nil && value
	}
	if input.Metadata != nil {
		if value, ok := input.Metadata["h3PromptEnhance"].(bool); ok {
			return value
		}
		if value, ok := input.Metadata["h3PromptEnhance"].(string); ok {
			parsed, err := strconv.ParseBool(strings.TrimSpace(value))
			return err == nil && parsed
		}
	}
	return false
}

func miniMaxH3FirstLastFrame(input canvasGenerationInput) bool {
	return strings.EqualFold(strings.TrimSpace(metadataString(input.Metadata, "videoEditOperation")), "first_last_frame")
}

func miniMaxH3CanUseJSON(input canvasGenerationInput) bool {
	for _, media := range append(append(input.ReferenceImages, input.ReferenceVideos...), input.ReferenceAudios...) {
		if !isPublicMediaURL(strings.TrimSpace(firstNonEmpty(media.URL, media.DataURL))) {
			return false
		}
	}
	return true
}

func miniMaxH3JSONRequest(input canvasGenerationInput, seconds int) map[string]interface{} {
	body := map[string]interface{}{
		"model":          "minimax_h3",
		"prompt":         strings.TrimSpace(input.Prompt),
		"prompt_enhance": miniMaxH3PromptEnhance(input),
		"seconds":        seconds,
		"size":           miniMaxH3Size(input.Config.Size, input.Config.VQuality),
	}
	addURLs := func(single string, plural string, media []providerMedia) {
		if len(media) == 0 {
			return
		}
		values := make([]string, 0, len(media))
		for _, item := range media {
			values = append(values, strings.TrimSpace(firstNonEmpty(item.URL, item.DataURL)))
		}
		if len(values) == 1 {
			body[single] = values[0]
			return
		}
		body[plural] = values
	}
	if miniMaxH3FirstLastFrame(input) {
		images := make([]string, 0, len(input.ReferenceImages))
		for _, image := range input.ReferenceImages {
			images = append(images, strings.TrimSpace(firstNonEmpty(image.URL, image.DataURL)))
		}
		body["mode"] = "first_last_frame"
		body["images"] = images
	} else {
		addURLs("input_reference", "images", input.ReferenceImages)
	}
	addURLs("reference_video", "reference_videos", input.ReferenceVideos)
	addURLs("reference_audio", "reference_audios", input.ReferenceAudios)
	return body
}

func writeMiniMaxH3Reference(writer *multipart.Writer, field string, media providerMedia) error {
	value := strings.TrimSpace(firstNonEmpty(media.DataURL, media.URL))
	if isPublicMediaURL(value) {
		return writer.WriteField(field, value)
	}
	return writeMediaPart(writer, field, media)
}

func miniMaxH3ResultURL(state map[string]interface{}) string {
	if metadata, ok := state["metadata"].(map[string]interface{}); ok {
		if value := strings.TrimSpace(stringField(metadata, "url")); isPublicMediaURL(value) {
			return value
		}
	}
	return newAPIVideoResultURL(state)
}

func miniMaxH3FailureMessage(state map[string]interface{}) string {
	if value := strings.TrimSpace(stringField(state, "error")); value != "" {
		return value
	}
	if failure, ok := state["error"].(map[string]interface{}); ok {
		return firstNonEmptyString(stringField(failure, "message"), stringField(failure, "detail"))
	}
	return firstNonEmptyString(stringField(state, "message"), stringField(state, "msg"))
}
