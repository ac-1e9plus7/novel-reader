# HTTP API

最后核对：2026-10-18。实现位于 `backend/internal/app/server.go`。本文描述浏览器与后端之间的公开契约；字段变化时必须同步 `frontend/src/types.ts` 和浏览器测试。

## 通用约定

- 基础路径为 `/api`；请求和响应均为 JSON，音频接口除外。
- 写请求（非 `GET` / `HEAD`）必须携带 `X-Reader-Request: 1`。
- 浏览器发送的 `Origin` 如果存在，必须与请求 `Host` 相同；Host 还需匹配监听地址或本机名称。
- JSON 请求体上限为 4 MiB；未知字段、空请求体之外的多个 JSON 对象都会被拒绝。
- 服务不提供用户认证，默认只监听 `127.0.0.1`。对外部署必须自行增加认证、TLS 和访问控制。
- 成功响应通常为 `200`；创建小说/章节为 `201`；创建任务、批次或重试为 `202`。
- 业务错误响应统一为 `{"error":"可读消息"}`，状态码为 `400`（参数/格式）、`404`、`409`（版本冲突）、`500`。Host、Origin 或请求标记校验失败由中间件返回纯文本 `403`。
- 版本冲突不会静默覆盖。客户端应刷新后重试，并保留用户尚未提交的草稿。

## 数据形状

### Book

```json
{
  "id": "6f1c...",
  "title": "夏日来信",
  "author": "作者名",
  "created": "2026-10-18T12:00:00Z",
  "chapter_count": 3,
  "glossary_revision": 1
}
```

### Chapter

`GET /api/chapters/{chapter}` 返回完整章节；目录接口返回同样的结构，但 `raw` 为空、`segments` 为 `null`。

```json
{
  "id": "a1b2...",
  "book_id": "6f1c...",
  "title": "第一章 窗边",
  "position": 0,
  "revision": 1,
  "raw": "原文……",
  "segments": [
    {
      "id": "s1...",
      "chapter_id": "a1b2...",
      "position": 0,
      "paragraph": 0,
      "start": 0,
      "end": 12,
      "text": "「おはよう」と彼女は言った。",
      "translation": "「早上好。」她说道。",
      "translation_revision": 1,
      "manual": true,
      "speech_text": "「おはよう」と彼女は言った。",
      "speech_revision": 1,
      "clips": [
        {
          "id": "c1...",
          "segment_id": "s1...",
          "position": 0,
          "text": "「おはよう」と彼女は言った。",
          "asset_id": "asset...",
          "duration": 2.4
        }
      ]
    }
  ],
  "translated": 1,
  "total": 1,
  "audio_ready": 1
}
```

`start` / `end` 是 Go rune 下标，前端不能直接当作 JavaScript UTF-16 下标截取。

### Job

```json
{
  "id": "job...",
  "chapter_id": "a1b2...",
  "kind": "translation",
  "status": "running",
  "revision": 1,
  "done": 0,
  "total": 1,
  "message": "",
  "created": "2026-10-18T12:00:00Z",
  "cancel": false,
  "items": [
    {
      "id": "item...",
      "job_id": "job...",
      "position": 0,
      "status": "running",
      "message": ""
    }
  ]
}
```

`GET /api/jobs` 不包含 `items`；`GET /api/jobs/{job}` 才包含。任务状态为 `queued`、`running`、`succeeded`、`failed`、`partial_failed`、`cancelled`、`interrupted`、`invalidated`。

### TranslationBatch

```json
{
  "id": "batch...",
  "chapter_id": "a1b2...",
  "revision": 1,
  "created": "2026-10-18T12:00:00Z",
  "status": "running",
  "total": 4,
  "done": 1,
  "failed": 0
}
```

批次状态与任务类似，但由成员任务汇总得出。批次只是一组独立单段任务，不会合并成一次多段模型请求。

### Glossary

```json
{
  "revision": 2,
  "terms": [
    {"source": "彼女", "target": "她", "reading": "かのじょ"}
  ]
}
```

### Progress

```json
{
  "chapter_id": "a1b2...",
  "segment_id": "s1...",
  "clip_id": "c1...",
  "seconds": 1.5,
  "revision": 3
}
```

从未保存过进度时，`GET` 返回所有字段为空/零的对象。

### Attempt

```json
{
  "created": "2026-10-18T12:00:00Z",
  "elapsed_ms": 1504,
  "usage": {"input_tokens": 100, "output_tokens": 50},
  "status": "succeeded"
}
```

服务未返回用量时 `usage` 为 `null`；不能据此认定没有计费。

## 配置与设置

| 方法与路径 | 请求 | 响应 |
| --- | --- | --- |
| `GET /api/health` | — | `{"status":"ok"}` |
| `GET /api/config` | — | `translation_ready`、`tts_ready`、`model`、`tts_provider`、`tts_model`、`voice`、`flac_enabled`、`tts_concurrency` |
| `PATCH /api/settings/audio` | `{"concurrency":10}` | `{"tts_concurrency":10}` |

`PATCH /api/settings/audio` 接受 1–32 的整数并写入 SQLite，立即影响后续调度；调低上限不会取消在途合成。

## 书架与章节

| 方法与路径 | 请求 | 响应 |
| --- | --- | --- |
| `GET /api/books` | — | `Book[]`，按创建时间倒序 |
| `POST /api/books` | `{"title":"书名","author":"可选"}` | `201` + `Book` |
| `GET /api/books/{book}/chapters` | — | `Chapter[]` 摘要，按 `position` 排序 |
| `POST /api/books/{book}/chapters` | `{"title":"章节名","raw":"日文原文"}` | `201` + `{"id":"章节ID"}` |
| `PUT /api/books/{book}/chapters/order` | `{"expected_order":["id..."],"chapter_ids":["id..."]}` | `{"ok":true}` |
| `DELETE /api/chapters/{chapter}` | `{"revision":1}` | `{"ok":true}` |
| `GET /api/chapters/{chapter}` | — | 完整 `Chapter` |
| `PATCH /api/chapters/{chapter}/source` | `{"title":"标题","raw":"新原文","revision":1}` | `{"ok":true}` |

排序请求必须提交小说当前全部章节；`expected_order` 用于检测目录快照冲突。删除同时移除本章原文、译文、历史、任务和阅读位置，共用音频资产保留。

## 翻译

| 方法与路径 | 请求 | 响应 |
| --- | --- | --- |
| `POST /api/chapters/{chapter}/translation-jobs` | `{"revision":1,"segments":["段落ID"],"force":false}` | `202` + `Job` |
| `GET /api/chapters/{chapter}/translation-batches` | — | `TranslationBatch[]` |
| `POST /api/chapters/{chapter}/translation-batches` | `{"revision":1}` | `202` + `TranslationBatch` |
| `POST /api/translation-batches/{batch}/cancel` | — | `{"ok":true}` |
| `POST /api/translation-batches/{batch}/retry` | — | `202` + `TranslationBatch` |
| `PATCH /api/chapters/{chapter}/translations` | `{"revision":1,"items":[{"segment_id":"id","text":"译文","revision":0}]}` | `{"ok":true}` |
| `PATCH /api/segments/{segment}/translation` | `{"text":"译文","revision":0}` | `{"ok":true}` |

单段任务接口只接受恰好一个段落 ID；空数组或多段会返回 `400`。同一章节同一段的活动任务会被复用。整章批次只为缺失译文的段落建立任务，跳过已有译文和人工译文；重复提交运行中的批次会复用原批次。

批量人工译文在一个事务内保存：任何章节或段落版本冲突都会整批回滚，不会出现一部分保存成功。

## 语音与朗读文本

| 方法与路径 | 请求 | 响应 |
| --- | --- | --- |
| `POST /api/chapters/{chapter}/audio-jobs` | `{"revision":1,"segments":[],"force":false}` | `202` + `Job` |
| `PATCH /api/segments/{segment}/speech-text` | `{"text":"朗读文本","revision":1}` | `{"ok":true}` |
| `GET` / `HEAD` `/api/audio/{asset}?format=wav` | — | 音频字节 |
| `POST /api/cache/cleanup` | — | `{"removed":2}` |

`segments` 为空表示整章缺失音频；非空时只处理列出的段落。`force=true` 会重新生成已有音频。音频响应带 `ETag` 和 `Cache-Control: private, no-cache`，支持 Range；`format=wav` 时 FLAC 缓存会即时解码为 WAV。有排队或运行中的任务时，缓存清理返回 `400`。

## 术语表与阅读进度

| 方法与路径 | 请求 | 响应 |
| --- | --- | --- |
| `GET /api/books/{book}/glossary` | — | `Glossary` |
| `PUT /api/books/{book}/glossary` | `{"revision":1,"terms":[{"source":"原词","target":"译名","reading":"可选读音"}]}` | 更新后的 `Glossary` |
| `GET /api/books/{book}/progress` | — | `Progress` |
| `PUT /api/books/{book}/progress` | `Progress` | 更新后的 `Progress`，`revision` 递增 |

术语变更会使旧翻译任务失效。阅读进度必须指向本书内的章节、段落和已存在的片段；非法目标返回 `400`，版本冲突返回 `409`。

## 任务

| 方法与路径 | 请求 | 响应 |
| --- | --- | --- |
| `GET /api/jobs?chapter={id}` | — | 最近 50 条 `Job[]` |
| `GET /api/jobs/{job}` | — | 含分项的 `Job` |
| `GET /api/jobs/{job}/attempts` | — | `Attempt[]` |
| `POST /api/jobs/{job}/cancel` | — | `{"ok":true}` |
| `POST /api/jobs/{job}/retry` | — | `202` + `Job` |

重试只重置未成功分项。模型配置、章节原文或术语表变化后，旧任务不能悄悄续跑；需要重新创建任务。`invalidated` 任务不能重试。

## SSE

`GET /api/events?chapter={id}` 返回 `text/event-stream`。连接期间每秒发送一次任务快照，并另发一个命名 `batches` 事件：

```text
data: [{"id":"job...","status":"running","done":1,"total":4,...}]

event: batches
data: [{"id":"batch...","status":"running","done":1,"total":4,...}]
```

浏览器断开连接即停止推送。SSE 只是通知通道，任务状态仍以 SQLite 为准。

## 输入限制

| 字段 | 限制 |
| --- | --- |
| 书名、章节标题 | 1–200 字 |
| 章节原文 | 1–200000 字 |
| 单段人工译文 | 1–20000 字 |
| 朗读文本 | 1–4000 字 |
| 批量人工译文 | 1–5000 段，段落 ID 不得重复 |
| 术语表 | 最多 500 项，原词唯一，原文/译名非空 |
| 阅读进度秒数 | 0–3600 |
| JSON 请求体 | 最多 4 MiB |

更多行为见[架构与数据流](architecture.md)和[实现与维护约束](constraints.md)。
