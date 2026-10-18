# 服务配置

配置在根目录 `.env` 中，后端启动时读取；操作系统环境变量优先于 `.env`。密钥不会发送给浏览器，不使用 `VITE_*` 保存密钥。

## 对照翻译

```dotenv
LLM_API_STYLE=responses
RESPONSES_BASE_URL=https://api.openai.com/v1
RESPONSES_API_KEY=填写服务密钥
RESPONSES_MODEL=填写服务可用的模型名称
RESPONSES_STRUCTURED_OUTPUT=true
RESPONSES_TIMEOUT_SECONDS=300
TRANSLATION_BATCH_CHARS=2400
```

服务需兼容 `POST /responses`。请求包含 `store=false`、固定来源 ID、上下文、术语表和 JSON Schema；服务不支持 Schema 时设 `RESPONSES_STRUCTURED_OUTPUT=false`，服务端仍进行 JSON 和覆盖校验。模型必须返回 `status=completed`。

### DeepSeek / Responses（默认）

使用 [DeepSeek + Gemini 配置模板](../config/deepseek-gemini.env.example)：

```dotenv
LLM_API_STYLE=responses
RESPONSES_BASE_URL=https://api.deepseek.com
RESPONSES_API_KEY=填写DeepSeek密钥
RESPONSES_MODEL=deepseek-flash
RESPONSES_STRUCTURED_OUTPUT=true
```

DeepSeek 官方支持 `POST /responses` 和 `text.format=json_schema`，本项目默认使用该协议。请求通过 `instructions` 和 `input` 传入任务，通过 `reasoning.effort=none` 关闭思考模式；仅解析 `output_text`，不把 reasoning 内容混入译文。结果必须为 `status=completed`，随后进行 ID 覆盖校验。参考 [DeepSeek Responses 指南](https://api-docs.deepseek.com/guides/responses_api/) 和 [接口参数](https://api-docs.deepseek.com/api/create-response/)。

此前把 DeepSeek 模板设为 Chat Completions 并不意味着它不支持 Responses；当前已纠正默认配置。`LLM_API_STYLE=chat_completions` 仍可显式选择，使用 `/chat/completions`、JSON object 模式及 `finish_reason=stop` 校验。失败时不会自动切换协议重复调用。

每批目标默认不超过 2400 日文字符；不是精确 token 数。原段超过 900 Unicode 字符时内部拆分，保留原文位置和自然段编号。返回上限为 12000 tokens；截断、缺段、重复 ID、空译文会使本批失败。可调小批次后创建新任务。

同一翻译任务内顺序执行批次，携带前文模型摘要和邻接原文参考。摘要最长保存 500 字，术语表修改会使旧翻译任务失效；已有译文保持可读。模型拒绝、JSON 错误和语义问题不会自动反复请求；可手动重试或校对。

## 日语朗读

**OpenAI 兼容 Speech 接口**：

```dotenv
TTS_PROVIDER=openai
TTS_BASE_URL=https://api.openai.com/v1
TTS_API_KEY=填写语音服务密钥
TTS_MODEL=gpt-4o-mini-tts
TTS_VOICE=marin
```

调用 `POST /audio/speech`，请求 WAV。除 `tts-1` / `tts-1-hd` 外附带独立朗读指令，不把说明文字拼入正文。实际模型、声音和指令字段支持情况以你配置的服务为准。

**OpenRouter**：

```dotenv
TTS_PROVIDER=openrouter
TTS_BASE_URL=https://openrouter.ai/api/v1
TTS_API_KEY=填写服务密钥
TTS_MODEL=填写支持语音输出的模型名称
TTS_VOICE=填写对应声音名称
```

本项目的 Gemini 配置默认使用女性声音 **Aoede**（Breezy），备选 Sulafat（Warm）。`TTS_VOICE` 未填时，Gemini / OpenRouter 自动选择 Aoede，OpenAI 仍选择 marin；显式填写的声音优先。声音性别与官方风格说明、原生 Gemini API 与 OpenRouter 的区别见 [Gemini TTS 接入说明](gemini-tts.md)。

`google/gemini-` 前缀请求 PCM；从响应 Content-Type 的 `rate` 和 `channels` 读取元数据，缺失时拒绝猜测。其他模型请求 MP3，由 FFmpeg 解码为 PCM WAV。`google/gemini-3.8` 和 `openai/` 前缀分别附带对应 provider options。自定义服务如协议不同，需要扩展适配器并补协议测试。

**Azure Speech**：

```dotenv
TTS_PROVIDER=azure
AZURE_SPEECH_KEY=填写密钥
AZURE_SPEECH_REGION=japaneast
AZURE_SPEECH_VOICE=ja-JP-NanamiNeural
```

区域端点使用 SSML，文本和声音名经过 XML 转义，请求 24kHz、16 位、单声道 WAV。

## 运行参数

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `LISTEN_ADDR` | `127.0.0.1:8787` | Go HTTP 监听地址 |
| `DATA_DIR` | `./data` | SQLite 与音频目录 |
| `FRONTEND_DIR` | `./frontend/dist` | 构建后的前端 |
| `RESPONSES_TIMEOUT_SECONDS` | 300 | 5–1800 秒，包含本次传输重试等待 |
| `TTS_TIMEOUT_SECONDS` | 180 | 5–1800 秒 |
| `TRANSLATION_BATCH_CHARS` | 2400 | 900–12000，单批目标日文字符数 |
| `TTS_CONCURRENCY` | 2 | 1–8，全局同时合成上限 |
| `FFMPEG_PATH` | `ffmpeg` | 可执行文件名称或完整路径 |

数值超出范围或格式无效时使用默认值。服务有 3 个任务工作协程；每个任务内顺序执行片段。TTS 并发上限不是单个整章任务的并发数量。

429 和 5xx 最多重试两次，尊重 1–30 秒的整数 `Retry-After`，否则等待 1 / 2 秒；认证、参数错误直接反馈。网络断连与超时不自动重试，用户可以选择重试任务。服务未返回 token 用量时保存空值，不能认定没有计费。

音频先写 WAV，再由单独压缩协程生成 FLAC。逐样本核验无损、数据库发布成功后才删除 WAV。播放器请求 WAV 兼容表示；磁盘缓存为 FLAC 时由纯 Go 解码，不依赖 FFmpeg。播放倍速不重新合成。

当前 FLAC 解码依赖可能对 24kHz 文件输出其上游样本覆盖提示；该提示本身不是解码失败。应用仍会核对 PCM 是否完全一致，失败则保留 WAV。
