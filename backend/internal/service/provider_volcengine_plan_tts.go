package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
)

const (
	volcenginePlanTTSDefaultSpeaker = "zh_female_vv_uranus_bigtts"
	volcenginePlanTTSDefaultModel   = "seed-tts-2.0"
	volcenginePlanTTSEndpoint       = "https://openspeech.bytedance.com/api/v3/plan/tts/unidirectional"
)

type volcenginePlanTTSResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func runVolcenginePlanTTSTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	requestID, err := volcenginePlanTTSRequestID()
	if err != nil {
		return nil, err
	}
	format := volcenginePlanTTSFormat(input.Config.AudioFormat)
	resourceID := defaultString(input.Config.Model, volcenginePlanTTSDefaultModel)
	body := map[string]interface{}{
		"user": map[string]interface{}{"uid": "open-ai-canvas"},
		"req_params": map[string]interface{}{
			"text":    strings.TrimSpace(input.Prompt),
			"speaker": volcenginePlanTTSSpeaker(input.Config.AudioVoice),
			"audio_params": map[string]interface{}{
				"format":      format,
				"sample_rate": 24000,
				"speech_rate": volcenginePlanTTSSpeechRate(input.Config.AudioSpeed),
			},
		},
	}
	var response volcenginePlanTTSResponse
	if err := postVolcenginePlanTTS(ctx, input.Config, resourceID, requestID, body, &response); err != nil {
		return nil, err
	}
	if response.Code != 0 {
		return nil, fmt.Errorf("火山 Agent Plan 语音合成失败：%s", defaultString(response.Message, "上游返回错误"))
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(response.Data))
	if err != nil {
		return nil, errors.New("火山 Agent Plan 返回的音频数据无效")
	}
	mimeType, err := validateGeneratedAudio("", data, format)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"mode": "audio", "audio": map[string]interface{}{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType, "format": format}}, nil
}

func postVolcenginePlanTTS(ctx context.Context, config providerConfig, resourceID string, requestID string, body interface{}, target interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, volcenginePlanTTSURL(config.BaseURL), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", config.APIKey)
	req.Header.Set("X-Api-Resource-Id", resourceID)
	req.Header.Set("X-Api-Request-Id", requestID)
	req.Header.Set("X-Api-Sequence", "1")
	ApplyOutboundHeaders(req, config.Headers)
	return doJSON(req, target)
}

func volcenginePlanTTSURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" || strings.Contains(strings.ToLower(base), "ark.cn-beijing.volces.com") {
		return volcenginePlanTTSEndpoint
	}
	if strings.HasSuffix(base, "/api/v3/plan/tts/unidirectional") {
		return base
	}
	if strings.HasSuffix(base, "/api/v3") {
		return base + "/plan/tts/unidirectional"
	}
	return base + "/api/v3/plan/tts/unidirectional"
}

func volcenginePlanTTSSpeaker(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "alloy", "echo", "fable", "onyx", "nova", "shimmer":
		return volcenginePlanTTSDefaultSpeaker
	default:
		return value
	}
}

func volcenginePlanTTSFormat(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "wav", "pcm", "mp3":
		return strings.ToLower(strings.TrimSpace(value))
	case "opus", "ogg", "ogg_opus":
		return "ogg_opus"
	default:
		return "mp3"
	}
}

func volcenginePlanTTSSpeechRate(value string) int {
	speed := parseFloat(value, 1)
	rate := int(math.Round((speed - 1) * 100))
	if rate < -50 {
		return -50
	}
	if rate > 100 {
		return 100
	}
	return rate
}

func volcenginePlanTTSRequestID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("生成火山请求 ID 失败：%w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	hexValue := hex.EncodeToString(value[:])
	return hexValue[:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:], nil
}
