# 测试策略

测试保护的是用户可观察的行为和数据边界，不依赖真实账户或模型密钥。

## 后端

在 `backend/` 执行：

```sh
go test ./...
go vet ./...
go test -race ./...
```

最后一条需要可用的 race/C 工具链；生产构建的纯 Go SQLite 不要求 C。测试每次创建临时数据目录，不读取项目 `.env` 或用户 `data/`。

| 覆盖 | 保护的行为 |
| --- | --- |
| Unicode 与原文偏移 | 换行、引号、长文、emoji 不丢失，片段能还原来源 |
| 翻译覆盖 | 缺失、重复、未知 ID 和空译文拒绝发布 |
| 部分失败重试 | 成功批次保持不变，重试只补剩余内容 |
| 在途人工校对 | 用户译文优先，晚到模型结果不能覆盖 |
| 手工译文与离线翻译服务 | 缺少 LLM 密钥、外部服务失败后仍能朗读、改读音、保存进度和手工部分译文；不额外调用翻译服务 |
| 批量手工译文 | 跨章节、重复 ID、过期原文/译文均拒绝；冲突时整批回滚，已有音频不受影响 |
| 原文修改冲突 | 老任务不关联新版本，旧原文/译文保留历史 |
| 相同原文保存 | 不破坏段落 ID、翻译和缓存 |
| 术语和阅读进度 | 乐观锁冲突可见，跨小说位置拒绝写入 |
| TTS 并发去重 | 同文本同设置的并发请求只合成一次 |
| 朗读文本修改 | 只使目标段音频失效，保留其他引用 |
| 取消与进程恢复 | 已完成保留，重启不自动计费，同目录互斥 |
| 音频格式 | 拒绝截断/非 16 位 WAV，修复流式 WAV 头 |
| FLAC | 有 FFmpeg 时真实转码、纯 Go 解码、逐样本对比 |
| 模型协议 | Schema、Authorization、PCM 元数据与错误脱敏 |
| DeepSeek | 默认 Responses 的 JSON Schema、reasoning.effort=none、只解析 output_text；保留显式 Chat Completions；截断/拒绝/缺段不切换协议、保留用量 |
| Gemini 女性声音 | 默认 Aoede、显式声音覆盖、声音变更使缓存键改变、PCM 参数和风格元数据与朗读正文分离 |
| HTTP | Range / ETag、写请求来源校验、409 版本冲突 |
| 缓存清理 | 未引用资产删除、旧压缩结果不复活资源 |

## 浏览器

在 `frontend/` 执行：

```sh
npm ci
npm run build
npx playwright install chromium
npm run test:e2e
```

Playwright 启动 `backend/cmd/testserver`，服务固定监听 `127.0.0.1:8788`，使用系统临时目录和假翻译/合成器。假 TTS 返回一秒测试音调，便于验证浏览器真的加载和播放音频。测试入口从不加载 `.env`，不会产生模型费用。

浏览器用例在桌面 Chromium 与 Pixel 7 尺寸分别运行，覆盖创建小说、章节预览、翻译、手工校对、语音、改读音、刷新持久化、术语表、原文修改、主题和显示模式，并检查页面没有横向溢出。失败保留 trace 与 HTML 报告，阅读页截图保存在 `frontend/test-results/`。

另有断点续播用例：从 SQLite 中保存的第二片段 0.3 秒位置恢复，不自动播放；点击播放后连续进入第三片段，并确认最后的播放位置写回数据库。

手工译文浏览器用例模拟翻译请求失败，验证无译文朗读、仅日文模式下打开整章录入、部分填写、刷新恢复和单段补译；并通过另一页面写入制造真实版本冲突，检查草稿可见且没有部分保存。

端口已占用时测试拒绝复用服务；不要终止未知进程。浏览器测试依赖构建结果，改源码后先重新构建。

## CI

Windows 构建后可运行 `scripts/windows/smoke-test.ps1`，验证后台启停、首页、隔离 SQLite 和调用者工作目录保持。默认端口 8789，数据写入 `tmp/smoke-*`；有托管服务运行或端口被占用时拒绝执行，不停止已有服务。该测试不发送模型请求。

macOS 使用 `scripts/macos/test.sh --browser --race` 和 `scripts/macos/smoke-test.sh`；后者使用随机空闲端口、`tmp/smoke.*` 隔离目录，验证启动、状态、首页、数据库和停止。Windows 本机可使用 Git Bash 逐文件执行 `bash -n`；这只验证 Bash 语法，不代表已在 macOS 验证进程管理。

`.github/workflows/ci.yml` 在推送 `main`、Pull Request 和手动触发时执行，分为两层：

1. **Repository hygiene**：检查 Git 索引中是否包含密钥、私有配置、数据库、音频或构建产物，并验证检查器自身、提交空白格式和 GitHub Actions 工作流语法（actionlint）；对 macOS 脚本执行 `bash -n` 和 ShellCheck。
2. **Verify (Linux)**：仅在 Ubuntu 运行，检查 Go 格式、模块完整性、vet、race、覆盖率和构建；通过 npm 锁文件安装、TypeScript 检查、前端构建及桌面/手机浏览器测试。安装 FFmpeg 与 lsof 后验证音频编解码和 Shell 服务启停。

CI 不再包含 Windows runner 或 Windows 专用测试。Windows PowerShell 脚本保留供本机使用。Linux 上的 Shell 启停验证不能替代 macOS 真机验证。

流水线只授予 `contents: read`，Action 固定到 commit SHA，不注入模型密钥；同一分支的新运行取消旧运行。测试报告和 Linux 覆盖率保留 7 天。CI 不执行 `cmd/probe`，不会调用收费模型。

提交前在根目录运行 `node scripts/check-repository.mjs`，它检查的是 **Git 暂存区**；先执行 `git add`，并用 `git diff --cached --stat` 确认范围。检查器发现问题只输出文件名和规则名，不回显密钥。它是针对本项目的常见泄漏防线，不等同于完整安全审计。

完整运行记录见 [GitHub Actions](https://github.com/ac-1e9plus7/novel-reader/actions/workflows/ci.yml)。本地测试通过不代表远端 CI 已完成。

## 真实服务验收

在明确授权真实调用后，用单章短文本分别验证：服务协议、翻译覆盖与质量、人名读音、真实音频时长、取消以及模型计费记录。不能把模拟测试通过描述为实际模型验证。供应商、模型和声音变更时重新做小样本验收。
