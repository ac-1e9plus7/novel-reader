# 服务配置

最后核对：2026-10-18。配置在根目录 `.env` 中，后端启动时读取；操作系统环境变量优先于 `.env`。密钥不会发送给浏览器，不使用 `VITE_*` 保存密钥。

## 配置来源与优先级

“默认”在本文中分三种含义，部署前请先确认自己使用的是哪一种：

| 来源 | 内容 | 何时生效 |
| --- | --- | --- |
| 程序内置默认 | OpenAI 兼容地址、空模型/密钥、`gpt-4o-mini-tts`、OpenAI 声音 `marin` | 变量未设置或为空 |
| setup 生成配置 | `scripts/windows/setup.ps1` / `scripts/macos/setup.sh` 复制 `.env.example` | 首次运行安装脚本且 `.env` 不存在 |
| 推荐配置模板 | `config/deepseek-gemini.env.example`：DeepSeek Responses + OpenRouter Gemini TTS | 手动复制其中的条目到 `.env` |

`config/deepseek-gemini.env.example` **不会**被安装脚本自动复制。它只是推荐起点，不是程序内置默认。

优先级从高到低：

```text
操作系统环境变量 > .env > 程序内置默认
```

语音生成并发量还有一个持久化覆盖：

```text
网页保存到 SQLite 的值 > TTS_CONCURRENCY > 默认 10
```

修改 `.env` 后需要重启服务；网页保存的并发设置立即生效并在重启后保留。

## 对照翻译

通用 OpenAI 兼容配置：

```dotenv
LLM_API_STYLE=responses
RESPONSES_BASE_URL=https://api.openai.com/v1
RESPONSES_API_KEY=填写服务密钥
RESPONSES_MODEL=填写服务可用的模型名称
RESPONSES_STRUCTURED_OUTPUT=true
RESPONSES_TIMEOUT_SECONDS=300
```

服务需兼容 `POST /responses`。请求包含 `store=false`、目标段落 ID、上下文、术语表和 JSON Schema；服务不支持 Schema 时设 `RESPONSES_STRUCTURED_OUTPUT=false`，服务端仍进行 JSON 和覆盖校验。`RESPONSES_STRUCTURED_OUTPUT` 只有精确值 `true` 才启用结构化输出。模型必须返回 `status=completed`。

### 推荐配置：DeepSeek + Responses

使用 [DeepSeek + Gemini 配置模板](../config/deepseek-gemini.env.example)：

```dotenv
LLM_API_STYLE=responses
RESPONSES_BASE_URL=https://api.deepseek.com
RESPONSES_API_KEY=填写DeepSeek密钥
RESPONSES_MODEL=deepseek-flash
RESPONSES_STRUCTURED_OUTPUT=true
```

DeepSeek 官方支持 `POST /responses` 和 `text.format=json_schema`，本项目推荐使用该协议。请求通过 `instructions` 和 `input` 传入任务，通过 `reasoning.effort=none` 关闭思考模式；仅解析 `output_text`，不把 reasoning 内容混入译文。结果必须为 `status=completed`，随后进行 ID 覆盖校验。参考 [DeepSeek Responses 指南](https://api-docs.deepseek.com/guides/responses_api/) 和 [接口参数](https://api-docs.deepseek.com/api/create-response/)。

`LLM_API_STYLE=chat_completions` 仍可显式选择，使用 `/chat/completions`、JSON object 模式及 `finish_reason=stop` 校验。失败时不会自动切换协议重复调用。

翻译任务的行为：

- 每个任务只接受一个显式段落 ID，且只发起该段的生成调用；单任务接口拒绝空选区和多段选区。
- 「整章翻译」通过独立批次接口为缺失译文的段落建立多个单段任务，同章按顺序执行。
- 原段超过 900 Unicode 字符时导入阶段拆为阅读单元，保留原文位置和自然段编号。
- 返回上限为 12000 tokens；截断、重复 ID、空译文使本段失败，不阻止其他段落继续。
- 提供最多前两段及后一段原文作为参考，明确要求模型只翻译目标段落。
- 术语表修改会使旧翻译任务失效；已有译文保持可读。
- 模型拒绝、JSON 错误和语义问题不会自动反复请求；可手动重试或校对。

提示词版本、缓存键和任务状态见[架构与数据流](architecture.md)。旧版 `TRANSLATION_BATCH_CHARS` 已移除，说明见[升级与迁移](upgrading.md)。

## 日语朗读

### OpenAI 兼容 Speech 接口

```dotenv
TTS_PROVIDER=openai
TTS_BASE_URL=https://api.openai.com/v1
TTS_API_KEY=填写语音服务密钥
TTS_MODEL=gpt-4o-mini-tts
TTS_VOICE=marin
```

调用 `POST /audio/speech`，请求 WAV。除 `tts-1` / `tts-1-hd` 外附带独立朗读指令，不把说明文字拼入正文。实际模型、声音和指令字段支持情况以你配置的服务为准。

### OpenRouter / Gemini

```dotenv
TTS_PROVIDER=openrouter
TTS_BASE_URL=https://openrouter.ai/api/v1
TTS_API_KEY=填写服务密钥
TTS_MODEL=google/gemini-3.8-flash-tts
TTS_VOICE=Aoede
```

`google/gemini-` 前缀请求 PCM；从响应 Content-Type 的 `rate` 和 `channels` 读取元数据，缺失时拒绝猜测。其他 OpenRouter 模型请求 MP3，由 FFmpeg 解码为 PCM WAV。`google/gemini-3.8` 和 `openai/` 前缀分别附带对应 provider options。声音选择和原生 Gemini API 的区别见 [Gemini TTS 接入说明](gemini-tts.md)。

### Azure Speech

```dotenv
TTS_PROVIDER=azure
AZURE_SPEECH_KEY=填写密钥
AZURE_SPEECH_REGION=japaneast
AZURE_SPEECH_VOICE=ja-JP-NanamiNeural
```

区域端点使用 SSML，文本和声音名经过 XML 转义，请求 24kHz、16 位、单声道 WAV。Azure 模式不读取 `TTS_BASE_URL`、`TTS_API_KEY`、`TTS_MODEL` 和 `TTS_VOICE`。

### 默认声音规则

- `TTS_VOICE` 显式填写时始终优先。
- `TTS_VOICE` 留空且 `TTS_PROVIDER=openai` 时使用 `marin`。
- `TTS_VOICE` 留空、`TTS_PROVIDER=openrouter` 且模型以 `google/gemini-` 开头时使用 `Aoede`。
- 其他 OpenRouter 模型仍使用 `marin`，除非显式配置。

Gemini 声音的性别、风格和试听建议见 [Gemini TTS 接入说明](gemini-tts.md)。

## 运行参数

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `LISTEN_ADDR` | `127.0.0.1:8787` | Go HTTP 监听地址 |
| `DATA_DIR` | `./data` | SQLite、音频与备份目录 |
| `FRONTEND_DIR` | `./frontend/dist` | 构建后的前端 |
| `RESPONSES_TIMEOUT_SECONDS` | 300 | 5–1800 秒，包含本次传输重试等待 |
| `TTS_TIMEOUT_SECONDS` | 180 | 5–1800 秒 |
| `TTS_CONCURRENCY` | 10 | 1–32，未保存网页高级设置时使用的全局合成并发量 |
| `FFMPEG_PATH` | `ffmpeg` | 可执行文件名称或完整路径 |

数值超出范围或格式无效时使用默认值。

## 完整环境变量表

| 变量 | 默认 / 取值 | 用途 |
| --- | --- | --- |
| `LISTEN_ADDR` | `127.0.0.1:8787` | HTTP 监听地址 |
| `DATA_DIR` | `./data` | 数据目录；相对进程工作目录 |
| `FRONTEND_DIR` | `./frontend/dist` | 前端静态资源目录 |
| `LLM_API_STYLE` | `responses` | `responses` 或 `chat_completions` |
| `RESPONSES_BASE_URL` | `https://api.openai.com/v1` | 翻译服务根地址，末尾斜杠会被去掉 |
| `RESPONSES_API_KEY` | 空 | 翻译服务密钥；为空时自动翻译不可用 |
| `RESPONSES_MODEL` | 空 | 翻译模型；为空时自动翻译不可用 |
| `RESPONSES_STRUCTURED_OUTPUT` | `true` | 只有 `true` 启用 JSON Schema / JSON object |
| `RESPONSES_TIMEOUT_SECONDS` | `300` | 5–1800 秒 |
| `TTS_PROVIDER` | `openai` | `openai`、`openrouter` 或 `azure` |
| `TTS_BASE_URL` | `https://api.openai.com/v1` | OpenAI 兼容 / OpenRouter 根地址 |
| `TTS_API_KEY` | 空 | OpenAI 兼容 / OpenRouter 密钥 |
| `TTS_MODEL` | `gpt-4o-mini-tts` | 语音模型 |
| `TTS_VOICE` | 按规则自动选择 | 声音名称；显式值优先 |
| `AZURE_SPEECH_KEY` | 空 | Azure Speech 密钥 |
| `AZURE_SPEECH_REGION` | 空 | Azure 区域，例如 `japaneast` |
| `AZURE_SPEECH_VOICE` | `ja-JP-NanamiNeural` | Azure 声音 |
| `TTS_TIMEOUT_SECONDS` | `180` | 5–1800 秒 |
| `TTS_CONCURRENCY` | `10` | 1–32；网页保存的设置优先 |
| `FFMPEG_PATH` | `ffmpeg` | FFmpeg 可执行文件名称或完整路径 |

## 并发、重试与音频缓存

- 翻译使用 3 个工作协程，同章最多一个单段任务运行，其他章节可以并行。
- 语音有独立任务调度器，每个整章任务并行准备片段，所有章节共享同一个动态并发上限，默认能同时发起 10 个不同片段的合成请求。
- 提高并发量立即放行更多等待请求；调低时不取消在途合成，等其完成后再按新上限调度。
- 并发量不影响音频缓存键、声音或播放速度。
- 429 和 5xx 最多重试两次，尊重 1–30 秒的整数 `Retry-After`，否则等待 1 / 2 秒；认证、参数错误直接反馈。网络断连与超时不自动重试，用户可以选择重试任务。
- 服务未返回 token 用量时保存空值，不能认定没有计费。
- 音频先写 WAV，再由单独压缩协程生成 FLAC。逐样本核验无损、数据库发布成功后才删除 WAV。播放器请求 WAV 兼容表示；磁盘缓存为 FLAC 时由纯 Go 解码，不依赖 FFmpeg。
- 播放倍速不重新合成。OpenAI 兼容和 OpenRouter 合成请求显式使用 `speed=1.0`，Azure 使用默认语速；播放器初始速度也是 1.0。
- 当前 FLAC 解码依赖可能对 24kHz 文件输出其上游样本覆盖提示；该提示本身不是解码失败。应用仍会核对 PCM 是否完全一致，失败则保留 WAV。

## 相关文档

- [Gemini TTS 接入](gemini-tts.md)
- [架构与数据流](architecture.md)
- [真实验证记录](verification.md)
- [运维、限制与故障排查](operations.md)
