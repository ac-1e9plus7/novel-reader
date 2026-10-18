# 栞 Shiori · 日中对照小说阅读器

把粘贴的日文小说整理成可校对、可朗读的中文对照阅读内容。React / TypeScript 提供书架与阅读界面，Go 管理模型请求、任务和音频，SQLite 保存本机数据。

## 功能

- **书架与章节**：创建小说、粘贴日文、预览段落、保存原文版本，支持章节上移、下移和删除。
- **对照翻译**：支持单段和整章翻译；整章按段批量建立任务，每个任务只调用一次单段翻译，结合邻接原文和术语表生成译文。
- **阅读与校对**：日中对照、仅日文、仅中文；人工译文保护、深浅色、字号和手机布局。
- **手工译文与原文阅读**：逐段或整章手动填写，可只保存部分段落；没有译文或翻译服务不可用时仍可朗读原文、调整读音和保存进度。
- **日语朗读**：OpenAI 兼容 / OpenRouter / Azure Speech；分段播放、连播、循环、倍速、读音修正和整章准备。
- **高级语音设置**：生成并发量默认 10，可在网页调整为 1–32，实时生效并持久保存；播放默认原速 1.0。
- **音频缓存**：WAV 即时可用，后台 FLAC 无损压缩；独立的纯 Go FLAC 解码保证移除 FFmpeg 后已有缓存仍可播放。
- **持久化任务**：处理记录、取消、部分失败重试、重启后继续；阅读和播放位置存入 SQLite。
- **维护支持**：用量记录、缓存清理、离线模拟测试、Windows / macOS 脚本和 Linux CI。

## 快速开始

需要 **Go 1.24+、Node.js 24+**。FFmpeg 可选，用于 FLAC 压缩与 MP3 解码。

**Windows（PowerShell）**，在项目根目录执行：

```powershell
.\scripts\windows\setup.ps1
# 编辑生成的 .env，填写 Responses 与 TTS 配置
.\scripts\windows\start.ps1 start
```

停止：

```powershell
.\scripts\windows\start.ps1 stop
```

**macOS（Terminal）**：

```bash
./scripts/macos/setup.sh
# 编辑生成的 .env，填写模型配置
./scripts/macos/start.sh start
# 查看状态 / 停止
./scripts/macos/start.sh status
./scripts/macos/start.sh stop
```

启动后打开 [http://127.0.0.1:8787](http://127.0.0.1:8787)。脚本按平台分别位于 `scripts/windows/` 和 `scripts/macos/`。

没有密钥时仍可创建小说、保存原文、填写译文和阅读；自动翻译、语音需要对应服务配置。模型调用产生的费用由配置的服务收取。

先创建小说，再添加章节，保存后即可阅读和朗读日文。需要中文时，可点击「手动输入译文」填写、在段落旁使用「翻译此段」，或点击「整章翻译」批量安排单段任务。点击「朗读」按需生成音频，或点击「准备整章语音」；这些功能不要求先有译文。播放器按片段连播，正文高亮对应段落。

## 文档

| 文档 | 内容 |
| --- | --- |
| [安装与启动](docs/getting-started.md) | Windows / macOS 脚本、手动启动、数据备份 |
| [服务配置](docs/configuration.md) | Responses、TTS、超时、音频格式 |
| [使用指南](docs/reading.md) | 导入、校对、术语、播放与恢复 |
| [开发指南](docs/development.md) | 文件职责、接口、构建、测试 |
| [实现与维护约束](docs/constraints.md) | 版本、任务、缓存、密钥与数据保留 |
| [测试策略](docs/testing.md) | 回归用例覆盖与验收方法 |
| [文档索引](docs/README.md) | 文档分工 |

运行时文件位于 `data/`，不要提交密钥、个人文本和生成音频。默认仅监听本机；对外提供服务前需自行增加身份认证、TLS 和访问控制。
