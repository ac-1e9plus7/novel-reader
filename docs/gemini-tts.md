# Gemini TTS 接入说明

最后核对：2026-10-18。本文介绍如何通过 **OpenRouter → Gemini TTS** 使用 Gemini 声音；推荐配置模板采用这条路径，项目没有把 OpenRouter 密钥用于 Google 原生接口。

## 当前配置

推荐配置模板 `config/deepseek-gemini.env.example` 中的语音部分：

```dotenv
TTS_PROVIDER=openrouter
TTS_BASE_URL=https://openrouter.ai/api/v1
TTS_API_KEY=填写OpenRouter密钥
TTS_MODEL=google/gemini-3.8-flash-tts
TTS_VOICE=Aoede
```

项目调用 `/audio/speech`，提交 `model`、`input`、`voice`。Gemini 请求 PCM；按响应头中的 `rate` / `channels` 封装 WAV，再执行已有 FLAC 缓存流程。风格通过 `provider.options.google-ai-studio.speech_metadata.style` 单独发送，避免把朗读说明读成正文。接口入口见 [OpenRouter 官方教程](https://openrouter.ai/blog/tutorials/text-to-speech/)，模型 ID 见[模型页面](https://openrouter.ai/google/gemini-3.8-flash-tts/)。

`TTS_VOICE` 留空时的默认规则只适用于 `TTS_PROVIDER=openrouter` 且模型以 `google/gemini-` 开头；其他 OpenRouter 模型仍使用 `marin`。完整优先级见[服务配置](configuration.md)。

## 女性声音优先

| 声音 | 项目用途 |
| --- | --- |
| Aoede | 当前推荐女性声音，官方风格 Breezy；替换用户认为听感偏中性的 Kore |
| Sulafat | 女性备选，适合较温暖的语气 |
| Kore | 旧默认，官方标注女性，但用户试听认为偏中性，因此不再作为项目默认 |

性别依据 [Google Cloud 官方声音表](https://docs.cloud.google.com/text-to-speech/docs/gemini-tts#voice_options)，风格依据 [Gemini TTS 官方说明](https://ai.google.dev/gemini-api/docs/generate-content/speech-generation#voice-options)。官方性别分类不等于用户实际听感，具体效果以日语试听为准。

修改声音后缓存键随之变化，新生成使用新声音，旧音频在被替换前仍可读取；要替换旧 Kore 音频，可重新点击「准备整章语音」。

## Google 原生 API 的区别

Google 文档给出的原生入口为 `POST https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`，认证头是 `x-goog-api-key`。Gemini 3.8 示例把正文放入 `contents[].parts[].text`，语气放入 `speech_metadata.style`，并设置 `generationConfig.responseModalities=["AUDIO"]` 与 `speechConfig.voiceConfig.voice`。响应音频位于 `candidates[].content.parts[].inlineData.data`，需 Base64 解码。

该请求结构与 OpenRouter 的 `/audio/speech` 不同。当前项目已实现、真实验证的路径是 OpenRouter；不能把 Google 原生地址直接填入现有 OpenRouter 适配器。参见 [Google 原生 TTS 文档](https://ai.google.dev/gemini-api/docs/generate-content/speech-generation)。

## 真实验证

真实接口验证需要显式授权和 `-confirm-paid`，不能因为机器上有密钥就自动调用。工具、验证原则和历史结果见[真实验证记录](verification.md)。

## 相关文档

- [服务配置](configuration.md)
- [真实验证记录](verification.md)
- [架构与数据流](architecture.md)
