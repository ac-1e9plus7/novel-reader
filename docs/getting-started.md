# 安装与启动

## 环境

- Go 1.24 或更高版本。
- Node.js 24 或更高版本及 npm。
- 可选 FFmpeg：用于后台 FLAC 压缩和 OpenRouter MP3 响应解码。缺少时 OpenAI WAV、Azure WAV 和带元数据的 PCM 仍可使用，MP3 响应会明确报错。

SQLite 通过纯 Go 驱动内嵌，不需要安装数据库服务或 C 编译器。Go race 检测是例外，需要平台支持的 C 工具链。

## Windows

在项目根目录运行：

```powershell
.\scripts\windows\setup.ps1
```

脚本通过 npm 锁文件安装依赖、构建前端、下载 Go 模块并生成 `bin/novel-reader.exe`。仅在 `.env` 不存在时从模板复制，不覆盖已有配置。

编辑 `.env`，参见 [服务配置](configuration.md)。注意 setup 复制的是 `.env.example`；`config/deepseek-gemini.env.example` 是推荐模板，需要手动复制，不会自动生效。然后：

```powershell
.\scripts\windows\start.ps1 start
.\scripts\windows\start.ps1 stop
```

默认访问 [http://127.0.0.1:8787](http://127.0.0.1:8787)。脚本运行的服务保持后台、窗口隐藏，日志位于 `logs/`。关闭终端不停止服务。停止时核验记录的 PID、可执行文件路径及启动时间，拒绝终止不匹配的进程。

Windows 后台停止会终止进程。数据库事务和原子文件发布保护已完成结果；下次启动把未完成任务标为中断。未完成的远端调用仍可能计费。

所有脚本保留调用者工作目录。需要更新构建时先停止当前服务，再运行 setup；不要覆盖正在被服务提供的前端文件。

## macOS

脚本位于 `scripts/macos/`，兼容系统自带的 Bash 3.2；从 Terminal 执行，不要 `source`。无需更换默认的 zsh，也不需要 GNU `readlink`、`flock` 或 `timeout`。

```bash
./scripts/macos/setup.sh
# 编辑 .env
./scripts/macos/start.sh start
./scripts/macos/start.sh status
./scripts/macos/start.sh stop
```

安装脚本验证 Go 1.24+、Node.js 24+，通过锁文件安装依赖，生成 `frontend/dist/` 和 `bin/novel-reader`。仅在 `.env` 不存在时创建，不覆盖已有设置。启动和停止还需要系统的 `lsof`、`ps`，启动验证需要 `curl` 和 Node.js。

服务通过 `nohup` 在后台运行，工作目录固定为项目根目录，日志写入 `logs/`，进程身份保存在 `logs/server-macos.pid`。脚本不执行 `.env` 中的 Shell 代码，由后端读取配置。环境变量可覆盖配置，例如：

```bash
LISTEN_ADDR=127.0.0.1:9000 ./scripts/macos/start.sh start
```

停止前核验 PID、启动时间和可执行文件路径，发送 SIGTERM，最多等待 10 秒；超时保留状态供排查，不强制杀进程。重复 start 返回当前状态，重复 stop 不影响其他进程。更新构建前先停止服务。

脚本命令通过 `logs/.macos-control.lock/` 互斥。如果脚本被强制终止后遗留此目录，确认没有脚本仍在运行，再使用 `rmdir logs/.macos-control.lock` 清理空锁目录；不要删除用户数据目录。

```bash
# 后端测试、vet 与前端构建
./scripts/macos/test.sh
# 加上 race 与桌面/手机浏览器测试
(cd frontend && npx playwright install chromium)
./scripts/macos/test.sh --race --browser
# 隔离数据和随机本机端口，不调用模型
./scripts/macos/smoke-test.sh
```

所有入口支持 `--help`，可从其他目录用绝对路径执行，保留调用者工作目录。Git Bash 可用于逐文件语法检查，但不能代替 macOS 的实际进程管理验证：

```bash
for script in scripts/macos/*.sh; do bash -n "$script" || exit; done
```

## 手动构建（Windows / macOS / Linux）

以下从项目根目录开始：

```sh
npm --prefix frontend ci
npm --prefix frontend run build
cd backend
go mod download
go build -o ../bin/novel-reader ./cmd/server
cd ..
test -e .env || cp .env.example .env
# 编辑 .env 后，在项目根目录运行
./bin/novel-reader
```

Windows 手动构建建议使用 `-o ../bin/novel-reader.exe`，复制配置用 `Copy-Item`。只有配置不存在时才复制模板。

后端按**进程工作目录**读取 `.env`，`DATA_DIR` 和 `FRONTEND_DIR` 相对工作目录解析，因此生产启动必须位于项目根目录。终端前台模式使用 Ctrl+C 停止。

## 数据和备份

```text
data/
  reader.sqlite3       小说、译文、任务、术语和阅读位置
  reader.sqlite3-wal   SQLite 运行时文件（可能存在）
  reader.sqlite3-shm   SQLite 运行时文件（可能存在）
  .instance.lock       数据目录进程锁
  audio/               WAV / FLAC 音频资源
  backups/             迁移前数据库快照
```

备份时先停止服务，复制完整 `DATA_DIR`（包括 WAL/SHM 和 `audio/`），并单独安全保存 `.env`。只复制主数据库不能构成完整备份。数据路径、恢复步骤、单实例锁和安全边界的完整说明见[运维、限制与故障排查](operations.md)。

升级前还要阅读[升级与迁移](upgrading.md)，确认 schema 版本、自动备份和回滚方式。

## 常见问题

启动失败、端口占用、配置未生效、翻译失败、音频未压缩、任务中断和 403/409 等问题的排查表见[运维、限制与故障排查](operations.md#故障排查)。
