# 开发指南

## 结构

| 文件 | 职责 |
| --- | --- |
| `backend/cmd/server/main.go` | 生产入口、HTTP 生命周期、信号退出 |
| `backend/cmd/testserver/main.go` | 独立临时目录和离线假模型，只供测试 |
| `backend/cmd/probe/main.go` | 需显式确认付费的中性短文真实接口验证，使用独立数据库 |
| `backend/internal/app/config.go` | 环境配置、公开状态和模型配置指纹 |
| `backend/internal/app/model.go` | API 类型、Unicode 分段 |
| `backend/internal/app/schema.sql` | SQLite 初始化 schema，版本 1 |
| `backend/internal/app/store.go` | 原文、译文、术语、进度、快照和事务 |
| `backend/internal/app/providers.go` | Responses / TTS HTTP 协议、结构校验 |
| `backend/internal/app/jobs.go` | 持久化队列、重试、取消和版本检查 |
| `backend/internal/app/audio.go` | PCM/WAV/FLAC、原子发布、去重和缓存清理 |
| `backend/internal/app/server.go` | REST、SSE、音频 Range 和前端静态资源 |
| `frontend/src/App.tsx` | 书架、阅读、编辑、任务与播放器编排 |
| `frontend/src/ManualTranslationEditor.tsx` | 章节手工译文、部分填写、冲突后的草稿保留 |
| `frontend/src/api.ts` / `types.ts` | 请求封装和接口类型 |
| `frontend/src/style.css` | 主题、阅读排版和移动布局 |
| `frontend/e2e/` | 真实 Go / SQLite + 假模型的浏览器流程 |
| `scripts/windows/` | PowerShell 安装、启停、测试和启动验证 |
| `scripts/macos/` | Bash 3.2 兼容的安装、启停、测试和启动验证；`common.sh` 为内部共享函数 |

目前代码按领域文件组织在一个 Go package 内，便于事务边界统一维护。拆包时不得绕过 Store 锁或版本检查。

## 开发启动

终端一，在项目根目录：

```powershell
Push-Location backend
try {
    # backend 是 go.mod 所在目录，运行时使用根目录配置需指定路径。
    go build -o ../bin/novel-reader.exe ./cmd/server
} finally { Pop-Location }
.\bin\novel-reader.exe
```

终端二，在项目根目录：

```powershell
npm --prefix frontend run dev
```

Vite 代理 `/api` 到 `127.0.0.1:8787`。变更后端端口时同步调整 `vite.config.ts`。Go 没有自动热重载，修改后重新构建并启动；服务重启会中断正在运行的模型任务。

macOS 可在根目录使用 `(cd backend && go build -o ../bin/novel-reader ./cmd/server)` 构建，再运行 `./bin/novel-reader`；也可使用 [平台脚本](getting-started.md#macos)。

## API

所有写请求携带 `X-Reader-Request: 1`；浏览器还需要同源 Origin。JSON 最大 4 MiB。长任务返回 202 和任务 ID。

| 方法与路径 | 用途 |
| --- | --- |
| GET / POST `/api/books` | 书架与创建小说 |
| GET / POST `/api/books/{id}/chapters` | 目录与导入章节 |
| GET `/api/chapters/{id}` | 完整当前章节及片段 |
| PATCH `/api/chapters/{id}/source` | `{title,raw,revision}` |
| POST `/api/chapters/{id}/translation-jobs` | `{revision,segments:[],force:false}` |
| POST `/api/chapters/{id}/audio-jobs` | 同上，空 segments 表示整章 |
| PATCH `/api/segments/{id}/translation` | `{text,revision}` |
| PATCH `/api/chapters/{id}/translations` | `{revision,items:[{segment_id,text,revision}]}`；只提交有变更的非空译文，事务内保存 |
| PATCH `/api/segments/{id}/speech-text` | `{text,revision}` |
| GET / PUT `/api/books/{id}/glossary` | `{revision,terms:[{source,target,reading}]}` |
| GET / PUT `/api/books/{id}/progress` | `{chapter_id,segment_id,clip_id,seconds,revision}` |
| GET `/api/jobs?chapter={id}` | 最近 50 条任务 |
| GET `/api/jobs/{id}` | 含分项状态的任务详情 |
| GET `/api/jobs/{id}/attempts` | 耗时、用量与结果状态 |
| POST `/api/jobs/{id}/cancel` / `retry` | 停止、继续未成功部分 |
| GET `/api/events?chapter={id}` | 每秒发送数据库任务快照的 SSE |
| GET / HEAD `/api/audio/{id}?format=wav` | WAV 兼容播放，支持 Range / ETag |
| POST `/api/cache/cleanup` | 清理未引用音频 |
| GET `/api/config` / `/api/health` | 无密钥状态、健康检查 |

同一章节、同类活动任务重复创建返回已有任务。客户端要等待其完成后再提交新的不同选区。译文标记人工后，即使 `force=true` 也不会覆盖。

## 数据库演进

当前 schema 版本为 1。后续已有字段或约束变更需要显式迁移、版本检查及旧库测试，不可仅修改 CREATE TABLE 后假定已有库会自动更新。迁移前建议备份完整 DATA_DIR。

schema 内保留来源与翻译历史；音频是独立资源，按 clip 引用。分段偏移是 Go Unicode rune 下标，前端不能直接当 UTF-16 下标截取。

## 检查

```powershell
.\scripts\windows\test.ps1 -Browser
```

macOS：

```bash
./scripts/macos/test.sh --browser --race
```

分项命令及实际测试边界见 [测试策略](testing.md)。前端依赖锁定于 `package-lock.json`，Go 锁定于 `go.mod` / `go.sum`。格式化使用 `gofmt` 和 `npx prettier --write src e2e *.ts`。
