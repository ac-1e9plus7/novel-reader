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

编辑 `.env`，参见 [配置说明](configuration.md)，然后：

```powershell
.\scripts\windows\start.ps1 start
.\scripts\windows\start.ps1 stop
```

默认访问 [http://127.0.0.1:8787](http://127.0.0.1:8787)。脚本运行的服务保持后台、窗口隐藏，日志位于 `logs/`。关闭终端不停止服务。停止时核验记录的 PID、可执行文件路径及启动时间，拒绝终止不匹配的进程。

Windows 后台停止会终止进程。数据库事务和原子文件发布保护已完成结果；下次启动把未完成任务标为中断。未完成的远端调用仍可能计费。

所有脚本保留调用者工作目录。需要更新构建时先停止当前服务，再运行 setup；不要覆盖正在被服务提供的前端文件。

## 手动构建（Windows / macOS / Linux）

以下从项目根目录开始：

```sh
npm --prefix frontend ci
npm --prefix frontend run build
cd backend
go mod download
go build -o ../bin/novel-reader ./cmd/server
cd ..
cp .env.example .env
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
```

备份时先停止服务，复制完整 `DATA_DIR`。只复制主数据库而忽略仍在使用的 WAL 或音频目录，不能构成完整备份。恢复时停止服务，把完整数据目录放回原位，再启动。`.env` 单独安全保存。

更改 `DATA_DIR` 会切换到另一套独立数据。同一目录不能同时运行两个进程。锁文件无需手动删除，操作系统会在进程退出后释放锁。

## 常见问题

- **网页打不开**：检查日志中的监听地址、端口占用和前端是否构建。脚本默认端口为 `.env` 中的 `LISTEN_ADDR`。
- **翻译失败**：打开处理记录检查配置、权限、超时或格式错误，成功段落仍保留。
- **音频未压缩**：设置 FFmpeg 到 PATH 或 `FFMPEG_PATH`，重启后启动探测成功才启用压缩。
- **刷新后任务中断**：只有服务重启才标记中断；关闭网页不停止任务，重新打开即可查看进度。
- **修改配置未生效**：后端配置在启动时读取，必须重启服务。
