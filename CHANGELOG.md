# 变更记录

本项目从 0.1.0 开始记录。日期均为本地开发日期，发布版本以 Git 标签为准。

## Unreleased

### Documentation

- 统一“程序内置默认 / setup 生成配置 / 推荐配置模板”的表述，消除默认配置歧义。
- 新增[架构与数据流](docs/architecture.md)、[HTTP API](docs/api.md)、[升级与迁移](docs/upgrading.md)、[运维与限制](docs/operations.md)和[真实验证记录](docs/verification.md)。
- 将历史验证记录、迁移说明和已移除配置从当前参考文档中拆出。
- 增加数据与隐私说明、完整环境变量表、术语表和已知限制。
- 增加 `scripts/check-docs.mjs`，在 CI 中检查文档链接、索引覆盖和环境变量引用。
- 添加 Apache-2.0 许可证、`NOTICE` 和指向 `AGENTS.md` 的 `CLAUDE.md`。

## 0.1.0 - 2026-10-18

### Added

- 书架、章节导入、日中对照阅读、人工译文和术语表。
- 单段翻译、整章翻译批次、持久化任务、取消、重试和重启恢复。
- OpenAI 兼容、OpenRouter / Gemini 和 Azure Speech 朗读，WAV / FLAC 缓存与纯 Go FLAC 解码。
- 章节排序与删除、动态语音生成并发、Windows / macOS 启动脚本和 Linux CI。
