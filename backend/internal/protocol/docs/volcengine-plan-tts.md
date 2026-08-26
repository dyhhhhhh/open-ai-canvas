# 火山 Agent Plan 语音协议

该协议适配 Agent Plan 的 Seed-TTS 2.0 文本转语音服务。它是同步单向合成接口，不属于 OpenAI Speech 兼容协议。

## 接口与鉴权

{{OPERATIONS}}

```http
POST https://openspeech.bytedance.com/api/v3/plan/tts/unidirectional
X-Api-Key: <CHANNEL_API_KEY>
X-Api-Resource-Id: <MODEL_ID>
X-Api-Request-Id: <UUID>
X-Api-Sequence: 1
Content-Type: application/json
```

## 模型与能力

模型标识通过 `X-Api-Resource-Id` 提交，默认可使用渠道已配置的 Seed-TTS 资源标识。发音人通过 `audioVoice` 映射到请求中的 `req_params.speaker`；音频格式、采样率与语速也由模型能力和渠道配置共同约束。该协议只覆盖文本转语音，不承担语音识别、实时双向会话或供应商账户授权。

## 参数与响应

{{PARAMETERS}}

运行时发送 `user.uid`、`req_params.text`、`req_params.speaker` 与音频格式、采样率、语速。账户可能以 JSON、NDJSON、Base64 或二进制返回音频；平台会统一校验并持久化为受控资源。

## 官方与渠道说明

Agent Plan 的专用 Base URL 和可用资源标识以火山方舟控制台及账户文档为准，不能与标准 Ark 或 OpenAI 兼容接口混用。`X-Api-Key` 只由服务端从系统渠道配置读取，绝不会下发到前端、任务消息、诊断包或协议文档。账户限额、音色清单和返回编码变化时，应通过渠道配置和服务端适配器升级处理。

{{CONTRACT}}
