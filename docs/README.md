# 文档索引

## 使用者

- [安装与启动](getting-started.md)：依赖、构建、启动、停止、备份入口和常见问题。
- [服务配置](configuration.md)：模型服务、运行参数、完整环境变量和配置优先级。
- [Gemini TTS 接入](gemini-tts.md)：OpenRouter 配置、女性声音选择和原生接口区别。
- [使用指南](reading.md)：导入、校对、术语、播放、进度和任务状态。

## 运维

- [运维、限制与故障排查](operations.md)：日志、备份恢复、安全边界、隐私、费用和已知限制。
- [升级与迁移](upgrading.md)：schema 版本、迁移备份、离线迁移和回滚。

## 开发者

- [架构与数据流](architecture.md)：组件、请求路径、任务生命周期、数据库和缓存键。
- [HTTP API](api.md)：请求/响应形状、状态码、SSE 和输入限制。
- [开发指南](development.md)：代码结构、开发启动、检查命令和格式化。
- [实现与维护约束](constraints.md)：数据一致性、任务、缓存和密钥不变量。
- [测试策略](testing.md)：回归覆盖、模拟服务、CI 和真实服务验收边界。
- [真实验证记录](verification.md)：显式付费验证工具、原则和历史结果。

## 协作入口

- [项目 README](../README.md)
- [协作要求](../AGENTS.md)
- [Claude Code 入口](../CLAUDE.md)：只引用 `AGENTS.md`，不重复规则。
- [变更记录](../CHANGELOG.md)

## 术语约定

| 术语 | 含义 |
| --- | --- |
| 原文 / `raw` | 用户粘贴的日文文本，原样保存，不用模型结果覆盖 |
| 阅读单元 / 段落 / `segment` | 阅读和翻译的基本单位；按行和 900 Unicode 字符拆分 |
| 片段 / `clip` | 朗读的基本单位；一个阅读单元按约 280 字拆分 |
| 朗读文本 / `speech_text` | 实际送去合成的文本；可与原文不同，用于调整读音 |
| 译文 / `translation` | 中文翻译；分自动生成和人工译文，人工译文优先 |
| 任务 / `job` | 一次持久化的生成请求，包含一个或多个分项 |
| 批次 / `batch` | 整章翻译的分组；成员仍是独立单段任务 |
| 声音 / 音色 / `voice` | TTS 服务的声音名称，例如 `Aoede`、`marin` |
| 语音生成并发量 | 同时发起的 TTS 合成请求上限，1–32，默认 10 |
| 内置默认 / setup 生成 / 推荐模板 | 三种不同来源的配置，定义见[服务配置](configuration.md) |

## 维护约定

- 详细规则只维护一处：配置语义放 `configuration.md`，数据不变量放 `constraints.md`，用户操作放 `reading.md`。
- 新增文档必须更新本索引和[项目 README](../README.md)的文档表。
- 修改文档后运行 `node scripts/check-docs.mjs`，检查相对链接、索引覆盖、环境变量引用和示例路径。
- 历史验证记录放入 `verification.md`，版本变化放入 `CHANGELOG.md`，不要混入当前参考文档。
