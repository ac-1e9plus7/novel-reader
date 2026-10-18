# Gemini TTS 接入说明

核对日期：2026-10-18。当前项目使用 **OpenRouter → Gemini TTS**，没有把 OpenRouter 密钥用于 Google 原生接口。

## 当前项目配置

```dotenv
TTS_PROVIDER=openrouter
TTS_BASE_URL=https://openrouter.ai/api/v1
TTS_API_KEY=填写OpenRouter密钥
TTS_MODEL=google/gemini-3.8-flash-tts
TTS_VOICE=Kore
```

项目调用 `/audio/speech`，提交 `model`、`input`、`voice`，Gemini 请求 PCM；按响应头中的 rate/channels 封装 WAV，再执行已有 FLAC 缓存流程。风格通过 `provider.options.google-ai-studio.speech_metadata.style` 单独发送，避免把朗读说明读成正文。接口入口见 [OpenRouter 官方教程](https://openrouter.ai/blog/tutorials/text-to-speech/)，模型 ID 见 [模型页面](https://openrouter.ai/google/gemini-3.8-flash-tts/)。

## 女性声音优先

| 声音 | 项目用途 |
| --- | --- |
| Kore | 当前默认女性声音，适合清晰稳定的叙述 |
| Aoede | 女性备选，适合较轻松的语气 |
| Sulafat | 女性备选，适合较温暖的语气 |

性别依据 [Google Cloud 官方声音表](https://docs.cloud.google.com/text-to-speech/docs/gemini-tts#voice_options)，风格依据 [Gemini TTS 官方说明](https://ai.google.dev/gemini-api/docs/generate-content/speech-generation#voice-options)。用途是本项目的选择建议，不表示声线在所有语言中效果一致；具体效果以日语试听为准。修改声音后缓存键随之变化，新生成使用新声音，旧音频在被替换前仍可读取。

## Google 原生 API 的区别

Google 文档给出的原生入口为 `POST https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`，认证头是 `x-goog-api-key`。Gemini 3.8 示例把正文放入 `contents[].parts[].text`，语气放入 `speech_metadata.style`，并设置 `generationConfig.responseModalities=["AUDIO"]` 与 `speechConfig.voiceConfig.voice`。响应音频位于 `candidates[].content.parts[].inlineData.data`，需 Base64 解码。

该请求结构与 OpenRouter 的 `/audio/speech` 不同。当前项目已实现、真实验证的路径以验证报告为准；不支持把 Google 原生地址直接填入现有 OpenRouter 适配器。参见 [Google 原生 TTS 文档](https://ai.google.dev/gemini-api/docs/generate-content/speech-generation)。

## 显式真实验证

构建验证工具后，在项目根目录执行：

```powershell
Push-Location backend
try { go build -o ../bin/novel-probe.exe ./cmd/probe } finally { Pop-Location }
.\bin\novel-probe.exe -confirm-paid -out data/probes/my-probe
```

这会产生实际模型费用，工具必须显式加 `-confirm-paid`。固定使用约 40 字的中性日文，分别创建一次翻译和一次 TTS 任务；HTTP 层仍按项目重试规则处理临时错误。数据写入独立目录，不占用用户书架中的第一章。重复使用已有输出目录会被拒绝，避免无意重做收费调用。

输出包含独立 SQLite、音频缓存、便于试听的 `sample.wav` 和不含密钥的 `report.json`。报告记录任务结果、耗时、译文和音频时长。验证工具不读取用户章节作为样本。

### 本次真实验证记录

2026-10-18 使用 `jlpt-workflow/.env.bak` 的模型配置进行一次中性样本验证，密钥仅导入本项目忽略提交的 `.env`。未导入用户提交的小说原文，也没有替换用户的第一章。

| 项目 | 结果 |
| --- | --- |
| 翻译 | `deepseek-flash`，Chat Completions，成功，任务耗时 1753 ms |
| 日语朗读 | `google/gemini-3.8-flash-tts`，Kore，成功，任务耗时 15534 ms |
| 音频 | 8.2 秒，24kHz / 16 位 / 单声道，WAV 播放副本可用 |
| 存储 | 独立 SQLite 与 FLAC 缓存，无损校验通过 |

本地结果位于 `data/probes/deepseek-gemini-20261018/`。这证明该次服务调用和格式处理成功，不代表已全面评估翻译准确度、朗读自然度或所有模型。耗时包括本地任务轮询，不是纯模型首字延迟。
