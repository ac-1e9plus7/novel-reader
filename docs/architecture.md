# 架构与数据流

最后核对：2026-10-18。本文说明系统如何组织；不变量和修改约束以[实现与维护约束](constraints.md)为准。

## 组件

| 组件 | 主要位置 | 职责 |
| --- | --- | --- |
| 前端 | `frontend/src/` | 书架、阅读、校对、播放器和设置；只保存界面偏好，不保存模型密钥 |
| HTTP 服务 | `backend/internal/app/server.go` | REST、SSE、音频 Range、静态资源和请求来源校验 |
| 存储层 | `backend/internal/app/store.go` | SQLite 事务、版本检查、快照、阅读进度和实例锁 |
| 任务引擎 | `backend/internal/app/jobs.go` | 持久化队列、工作协程、取消、重试和崩溃恢复 |
| 音频层 | `backend/internal/app/audio.go` | 合成去重、WAV / FLAC、原子发布、缓存清理 |
| 模型适配 | `backend/internal/app/providers.go` | Responses / Chat Completions、OpenAI / OpenRouter / Azure Speech |
| 启动脚本 | `scripts/windows/`、`scripts/macos/` | 安装、后台启停、冒烟测试和启动验证 |

```text
浏览器
  │  REST / SSE / Range
  ▼
Go HTTP Server ───────────────► 静态文件 (frontend/dist)
  │
  ├── Store (SQLite, WAL, 单连接) ◄──► data/reader.sqlite3
  ├── Engine
  │     ├── translation workers (3)
  │     └── audio workers (2) + 每任务最多 32 个分项协程
  ├── Audio
  │     ├── data/audio/*.wav / *.flac
  │     └── 后台 FLAC 压缩协程
  └── Provider ──► LLM / TTS 服务
```

## 一次请求的路径

1. 浏览器通过 `/api/*` 调用 Go 服务。写请求必须带 `X-Reader-Request: 1`；浏览器同时发送的 `Origin` 必须与 `Host` 一致。
2. 服务端解析 JSON，拒绝未知字段、多个 JSON 对象和超过 4 MiB 的请求体。
3. 同步操作在短事务内完成；翻译和语音创建任务后立即返回 `202` 和任务 ID。
4. 前端通过 SSE `/api/events` 每秒接收任务快照和命名 `batches` 事件；任务真相来源始终是 SQLite。
5. 音频通过 `/api/audio/{asset}` 读取，支持 Range 和 ETag；缓存为 FLAC 时可在请求时转为 WAV。

## 章节、分段与片段

- 用户粘贴的 `raw` 原样保存，不做模型改写。原文实际变化会递增章节 `revision`、保存 `chapter_revisions` 快照并重建分段。
- 自然段以换行为基础。空行保留在原始文本中，但不生成阅读单元。
- 单个阅读单元超过 900 个 Unicode 字符时继续拆分；`start` / `end` 是 Go rune 下标，不是 JavaScript UTF-16 下标。
- 每个阅读单元再按约 280 字拆成音频片段（`audio_clips`）。播放、缓存和读音修改以片段为单位，正文高亮回到所属段落。
- 译文、朗读文本和章节原文各有独立版本；写入时使用乐观锁，冲突返回 `409`。

## 翻译任务

1. 单段接口必须且只能提交一个段落 ID，一次模型请求只包含一个翻译目标。
2. 相邻原文只作为上下文：最多前两段和后一段；模型被要求不得翻译或拼入目标段落。
3. “整章翻译”不是一次多段模型调用，而是在同一事务中为缺失译文建立多个独立单段任务，并记录到一个批次。
4. 翻译队列有 3 个工作协程；同一章节同时最多运行一个翻译任务，不同章节可以并行。
5. 模型结果先校验协议完成状态（Responses `status=completed`、Chat Completions `finish_reason=stop`）、`segment_id` 覆盖、重复和空译文，再在发布事务中重新检查章节版本、术语表版本、目标译文版本和人工标记。
6. 人工译文优先；晚到的模型结果不能覆盖。原文或术语表变化会使旧任务失效，重试时要求重新创建任务。

## 语音任务

1. 整章语音任务为每个缺失音频的片段建立分项；已缓存且缓存键一致的片段会跳过。
2. 音频队列有 2 个工作协程；单个整章任务内部最多并发处理 32 个分项。所有章节共享一个可动态调整的全局合成上限，默认 10，范围 1–32。
3. 合成请求完成前会按缓存键合并同文本、同声音、同服务配置的重复请求。
4. 新音频先写 WAV 临时文件，原子改名并写入 `audio_assets` 后才对外可见。FFmpeg 可用时由后台协程压缩为 FLAC，逐样本校验无损后更新资产表示并删除 WAV。
5. 播放器请求 `?format=wav`。磁盘上是 FLAC 时由纯 Go 解码，因此移除 FFmpeg 后已有缓存仍可播放；新 MP3 响应仍需要 FFmpeg 解码。
6. 清理只删除没有被任何 `audio_clips` 引用的资产，且有任务运行时拒绝执行。

## 数据模型

当前 schema 版本为 3。完整迁移说明见[升级与迁移](upgrading.md)。

| 表组 | 表 | 作用 |
| --- | --- | --- |
| 书籍与目录 | `books`、`chapters`、`chapter_revisions` | 小说、章节顺序、原文版本快照 |
| 阅读内容 | `segments`、`translations`、`translation_history`、`glossary_entries` | 阅读单元、译文、人工与模型历史、术语 |
| 音频 | `audio_assets`、`audio_clips` | 音频资源与片段引用；资源可按缓存键复用 |
| 任务 | `jobs`、`job_items`、`generation_attempts`、`job_summaries` | 任务、分项、每次尝试的耗时/用量、翻译摘要 |
| 批次 | `translation_batches`、`translation_batch_jobs` | 整章翻译分组；每个成员仍是独立单段任务 |
| 设置与进度 | `app_settings`、`reading_progress` | 持久化语音并发和每本小说的阅读/播放位置 |

数据库启用外键和 WAL，连接池固定为一个连接。`Store.mu` 串行协调需要一致性的多步读写；HTTP 请求和 FFmpeg 不在数据库事务内等待。

## 提示词与缓存键

提示词版本定义在 `backend/internal/app/providers.go`：

- `translationPromptVersion = "translation-v2-single-paragraph"`
- `speechPromptVersion = "speech-v1"`

缓存键由配置和提示词版本共同决定：

- 翻译键包含服务地址、模型、结构化输出开关和翻译提示词版本；Chat Completions 显式包含协议风格，DeepSeek Responses 还包含 `reasoning.effort=none` 的固定标记。
- 语音键包含 provider、服务地址、模型、声音、Azure 区域/声音和语音提示词版本；实际文本再单独参与摘要。
- 修改提示词语义时必须递增对应版本常量，否则旧音频或旧任务可能被误认为仍然有效。翻译提示词版本与语音提示词版本相互独立，避免翻译调整导致全部音频重新计费。

## 任务状态

- 任务和分项：`queued`、`running`、`succeeded`、`failed`、`cancelled`、`interrupted`、`invalidated`；任务还可能出现 `partial_failed`。
- 服务重启时，启动流程把 `queued` / `running` 任务标记为 `interrupted`，不会自动发起新的付费调用；用户可手动继续未成功部分。
- 取消会保留成功分项并传播 context，但不能承诺远端请求已经停止计费。
- 外部生成不是 exactly-once：远端成功、本地提交前崩溃时，重试可能再次收费。

## 延伸阅读

- [HTTP API](api.md)：请求、响应、状态码和 SSE 示例。
- [实现与维护约束](constraints.md)：修改代码前必须保持的不变量。
- [升级与迁移](upgrading.md)：schema 版本、备份和回滚。
- [运维与限制](operations.md)：部署边界、隐私、费用和故障排查。
- [测试策略](testing.md)：这些行为如何被回归测试覆盖。
