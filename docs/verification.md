# 真实服务验证记录

最后核对：2026-10-18。本文保存带日期的真实服务验证结果。当前配置以[服务配置](configuration.md)和[Gemini TTS 接入](gemini-tts.md)为准；历史记录不能替代当前配置说明。

## 验证原则

- 只有明确授权真实调用时才访问外部服务；机器上存在密钥不代表可以自动调用。
- 浏览器和 `go test` 使用临时数据与本地假模型，不能把它们的通过描述为真实模型验证。
- 验证使用固定中性短样本，不读取用户书架内容。
- 结果只证明该次服务调用、协议处理和音频格式成功，不代表翻译准确度、朗读自然度或所有模型/声音的普遍结论。
- 供应商、模型或声音变更后重新做小样本验收。

## 验证工具

`backend/cmd/probe` 创建独立输出目录，分别执行一次翻译和一次 TTS 任务：

```powershell
Push-Location backend
try { go build -o ../bin/novel-probe.exe ./cmd/probe } finally { Pop-Location }
.\bin\novel-probe.exe -confirm-paid -out data/probes/my-probe
```

macOS / Linux：

```sh
(cd backend && go build -o ../bin/novel-probe ./cmd/probe)
./bin/novel-probe -confirm-paid -out data/probes/my-probe
```

- 必须显式加 `-confirm-paid`，否则不会调用收费服务。
- 使用约 40 字的中性日文，创建一次翻译和一次 TTS 任务。
- 输出目录必须不存在或为空；重复使用已有目录会被拒绝。
- 输出包含独立 SQLite、音频缓存、可试听的 `sample.wav` 和不含密钥的 `report.json`。
- 数据写入独立目录，不占用用户书架。

## 2026-10-18：首次验证（旧默认条件）

使用当时 `.env` 中的模型配置执行一次中性样本验证。密钥文件未提交。

| 项目 | 结果 |
| --- | --- |
| 翻译 | `deepseek-flash`，Chat Completions，成功，任务耗时 1753 ms |
| 日语朗读 | `google/gemini-3.8-flash-tts`，Kore，成功，任务耗时 15534 ms |
| 音频 | 8.2 秒，24kHz / 16 位 / 单声道，WAV 播放副本可用 |
| 存储 | 独立 SQLite 与 FLAC 缓存，无损校验通过 |

本地结果位于 `data/probes/deepseek-gemini-20261018/`。耗时包含本地任务轮询，不是纯模型首字延迟。

## 2026-10-18：推荐配置复核（Responses + Aoede）

根据 DeepSeek Responses 官方文档调整推荐调用方式后，以同一段中性日文重新验证。

| 项目 | 结果 |
| --- | --- |
| 翻译 | `deepseek-flash`，`POST /responses`，JSON Schema，成功，任务耗时 1504 ms |
| 日语朗读 | `google/gemini-3.8-flash-tts`，Aoede，成功，任务耗时 6513 ms |
| 音频 | 8.8 秒，WAV 播放副本与 FLAC 无损缓存均可用 |

结果位于 `data/probes/deepseek-responses-aoede-20261018/`。此次验证没有调用 Chat Completions；该适配保留为显式选项。试听结果用于用户判断声线，不能仅凭官方分类宣称实际听感一定更女性化。

## 当前未验证事项

- 其他供应商、模型、区域或声音的兼容性。
- 长时间、整章和大并发的真实计费表现。
- 翻译准确度、术语一致性和朗读自然度的系统评估。
- macOS 真机的进程管理行为；CI 只运行 Linux，Git Bash 的 `bash -n` 只验证语法。
