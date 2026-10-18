# 开发指南

本文聚焦代码位置和开发命令。请求流、任务生命周期和数据库关系见[架构与数据流](architecture.md)；HTTP 契约见[HTTP API](api.md)。

## 结构

| 文件 | 职责 |
| --- | --- |
| `backend/cmd/server/main.go` | 生产入口、HTTP 生命周期、信号退出 |
| `backend/cmd/testserver/main.go` | 独立临时目录和离线假模型，只供测试 |
| `backend/cmd/probe/main.go` | 需显式确认付费的中性短文真实接口验证，使用独立数据库 |
| `backend/internal/app/config.go` | 环境配置、公开状态和模型配置指纹 |
| `backend/internal/app/model.go` | API 类型、Unicode 分段 |
| `backend/internal/app/schema.sql` / `migration_v*.sql` | SQLite 初始结构、旧任务作废和批次／设置迁移 |
| `backend/internal/app/chapters.go` | 章节排序、删除、在途任务取消 |
| `backend/cmd/migrate/main.go` | 离线应用迁移，不启动工作线程或调用模型 |
| `backend/internal/app/store.go` | 原文、译文、术语、进度、快照和事务 |
| `backend/internal/app/providers.go` | Responses / TTS HTTP 协议、结构校验 |
| `backend/internal/app/jobs.go` | 持久化队列、重试、取消和版本检查 |
| `backend/internal/app/translation_batches.go` | 整章翻译分组、创建单段任务、汇总进度、停止和补齐 |
| `backend/internal/app/audio_settings.go` | 持久化语音并发设置和可动态调整的全局限流器 |
| `backend/internal/app/audio.go` | PCM/WAV/FLAC、原子发布、去重和缓存清理 |
| `backend/internal/app/server.go` | REST、SSE、音频 Range 和前端静态资源 |
| `frontend/src/App.tsx` | 书架、阅读、编辑、任务与播放器编排 |
| `frontend/src/ManualTranslationEditor.tsx` | 章节手工译文、部分填写、冲突后的草稿保留 |
| `frontend/src/AudioSettings.tsx` | 高级语音生成并发设置 |
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

所有写请求携带 `X-Reader-Request: 1`；浏览器发送的 `Origin` 必须与 `Host` 一致。JSON 最大 4 MiB，未知字段会被拒绝。长任务返回 202 和任务 ID。

完整契约见 [HTTP API](api.md)，包括请求/响应形状、状态码、SSE 事件和输入限制。路由注册在 `backend/internal/app/server.go`，上游协议在 `backend/internal/app/providers.go`。

修改 API 字段时必须同步：

- `backend/internal/app/model.go` 和 `server.go`
- `frontend/src/types.ts` 和 `api.ts`
- `frontend/e2e/` 中的浏览器流程
- `docs/api.md`

单段翻译重复提交同一段会返回活动任务，不同段可以排队，同章串行执行。整章通过批次接口创建独立单段任务，译文标记人工后，即使 `force=true` 也不会覆盖。SSE 保留默认任务消息，并增加命名 `batches` 事件，避免只显示最近 50 个任务时丢失整章进度。

## 数据库与提示词

当前 schema 版本为 3。服务启动会自动迁移；也可在停服后构建 `backend/cmd/migrate`，从项目根目录运行该工具，只更新数据库，不调用模型。迁移备份、版本 2/3 差异和回滚见[升级与迁移](upgrading.md)；表结构和数据流见[架构与数据流](architecture.md#数据模型)。

schema 内保留来源与翻译历史；音频是独立资源，按 clip 引用。分段偏移是 Go Unicode rune 下标，前端不能直接当 UTF-16 下标截取。

提示词版本定义在 `backend/internal/app/providers.go`：

- `translationPromptVersion` 影响翻译缓存键和旧任务有效性。
- `speechPromptVersion` 影响语音缓存键；它与翻译提示词版本相互独立。
- 修改提示词语义时必须递增对应版本常量，并在提交前运行相关后端和浏览器测试。

## 检查

```powershell
.\scripts\windows\test.ps1 -Browser
```

macOS：

```bash
./scripts/macos/test.sh --browser --race
```

文档变更后运行：

```sh
node scripts/check-docs.mjs
```

分项命令及实际测试边界见 [测试策略](testing.md)。前端依赖锁定于 `package-lock.json`，Go 锁定于 `go.mod` / `go.sum`。格式化使用 `gofmt` 和 `npx prettier --write src e2e *.ts`。
