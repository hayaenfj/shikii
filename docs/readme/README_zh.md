[English](../../README.md) · [日本語](README_ja.md) · **简体中文** · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

# shikii: 运维决策模型

### 为每一个决策模型提供同一套生命周期: define → decide → record → label → calibrate → check → drift。

**模型给你一个概率。shikii 告诉你能不能据此行动。**

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-188%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[设计笔记](../decision-ops-framework.md) · [运维手册](../operations.md) · [模型对比](../model-comparison.md) · [详细用法(日语)](../guide.ja.md)**

[快速开始](#快速开始) · [工作原理](#工作原理) · [可连接的对象](#可连接的对象) · [结果](#结果) · [FAQ](#faq) · [文档](#文档)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii 从不运行模型,也从不读取钥匙串。它只与你已有的决策模型端点通信、保存记录,并且在*这个问题在这个模型上*的概率经标签验证之前,拒绝 **act**。校准块一律由工具生成,绝不手写。

---

决策模型(TypeSafe Jev、llama.cpp 上的 Kev-4B 和 OpenJev、OpenRouter Decisions 等)回答带类型的问题 `choice`、`score` 和 `noul`(是/否),只返回概率,不生成文本。每家厂商都附有同样的免责声明:*这些概率没有针对你的数据校准,阈值也不能跨模型、跨问题通用。*

shikii 负责的是没人提供的那一部分:决策的**生命周期**。一个 spec 文件记录问题、选项、state 模板和阈值。`decide` 向模型提问,返回一个档位(`act` / `ask` / `discard` / `abstain`),而不是原始数字。`label` 记录实际的真相。`calibrate` 拟合并做交叉验证,然后以"问题哈希 + 模型名称"为键,把结果写回 spec。`check` 在措辞或模型发生漂移时让 CI 失败。`gc` 让 store 保持精简。

它还附带 `shikii-readout`,一个 500 行的适配器,通过读取选项字母的 logits,把**原版 llama.cpp 上的任意聊天 GGUF** 变成决策模型。在我们的测量中,以这种方式读取的普通 27B ternary 模型,准确率与专门训练的决策模型相当,在笔记本电脑上每题只需 0.6 秒。

> 这个名字来自日语:*shikii* (閾) 在日语中意为阈值,即把概率变成行动的那条界线。CLI 名为 `shikii`。

## 快速开始

需要 Go 1.27 或更高版本。首次运行不需要其他任何东西。

### 无需密钥和 GPU,跑完整个循环

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # 假模型:decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # 概率过于自信的假模型,观察 calibrate 如何选出 platt
```

### 对接真实模型

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) 基于 llama.cpp 的本地决策模型(首次运行会下载 Kev-4B,约 2.5 GB)
make server                                       # llama-server 监听 :8080
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) 托管的 TypeSafe Jev。密钥只从环境变量读取
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

返回结果包含档位、trace id 和各概率。在 spec 中写入该模型的校准之前,档位最高只到 `ask`。

### 闭合循环

```sh
bin/shikii label <trace_id> change_request          # or:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <model name from the response> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1 when the wording changed or precision@act fell below act
```

### 把任意 GGUF 当作决策模型

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # stock llama-server with flash attention and 8-bit KV, :8086
make readout                                               # shikii-readout in front of it, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### 安装与更新

```sh
make install          # copies bin/shikii to ~/.local/bin/shikii
git pull && make install
```

## 工作原理

每个决策就是一个 YAML 文件:问题、选项、state 模板、state 可以发往哪里、阈值,以及由 `calibrate --write` 生成的校准块。

```yaml
id: prompt-intent
type: choice
question: What is the user asking the assistant to do?
options:
  - id: change_request
    description: Change something in the repository - edit, fix, add, remove, rename...
  - id: question
    description: Explain, investigate, compare or give an opinion, without changing anything.
state:
  template: "{{ message }}"
  required: [message]            # 缺少输入 → abstain;绝不调用模型
policy:
  send_to: [hosted, local]       # 被污染(tainted)的会话会自动跳过 hosted
  pii: possible                  # 记录只保存输入的哈希,不保存原文
thresholds:
  act: 0.75                      # ≥ act → 自动 act,且仅在已校准时
  ask: 0.60                      # ≥ ask → 询问人;低于 → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**校准的键是 `(问题 + 模板 + 选项的哈希, 端点返回的模型 id)`。** 改动问题中的一个词,或者让 `jev-latest` 滚动到新版本,校准就会直接失效:档位会降为 `ask`,而不是悄悄沿用过期的常数继续行动。prompt 布局也是键的一部分:`shikii-readout` 会在模型名中带上它的布局(`…+state-last`),因为仅仅把 state 在 prompt 中挪个位置,就让同一份权重的 27B 模型准确率变化了 5 个百分点。

`calibrate` 在 5 折交叉验证下比较三种方法,并写入胜出者。`identity` 表示原始概率已经满足 precision@act,模型只需标记为已验证。`platt`(σ((Δ−bias)/T))用于是/否问题。`vector`(softmax((log p + b)/T))用于 choice 和 score,可修正每个选项上的固有习惯,例如"拿不准时就退到 *other*"。部分标签(`!option`,即"不是*那个*")计入 precision,但不参与拟合。从文件获得的标签会被持久化,因此之后的 `check` 和 `drift` 可以离线运行。

每一条与安全相关的分支(taint 路由、未校准不得 act、哈希过期失效、标签校验、反转检测、基于 CV 的方法选择、`gc` 的 dry-run 等),都有一条记录在案的变异:修复前先让测试失败,才接受修复。共有 63 个 Go 测试,分布在 8 个包中;演示会在 CI 中运行。

## 可连接的对象

shikii 使用 System One 请求格式(`POST /v1/systemone`),因此任何提供该接口的服务都能接入。其余的由适配器覆盖。

| 对象 | shikii 得到什么 | 要求与指南 |
|---|---|---|
| **TypeSafe Jev**(托管) | 带概率的 `choice` / `score` / `noul`,模型 id 形如 `jev-1.13.0` | 环境变量中设置 `TYPESAFE_API_KEY`。`--hosted` 的默认值 |
| **llama.cpp 决策模型**(Kev-4B、OpenJev、Clef 等) | 同样的格式,本地运行,模型 id 来自 `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M`(`make server`)。llama.cpp 0.6.0 或更新版本 |
| **OpenRouter Decisions API** | 同样的格式,经由 OpenRouter | `DECISIONS_API_KEY`,`--hosted https://openrouter.ai/api/alpha/decisions` |
| **任意聊天 GGUF**,经 `shikii-readout` | 以 System One 格式提供字母 logit 读出,自身不带校准 | 其后接原版 `llama-server` 0.7.0。[`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai**,经 [BonJev](https://github.com/ChezRD/bonjev) | 同上,使用 BonJev 自己的 prompt 和内置校准 | Rust 构建和 Prism fork。[`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **假模型**(`bin/fake-systemone`) | 用于演示和测试的确定性概率 | 无需任何东西。`make fake` |
| **Claude Code hook**(claude-jev) | 对每条提示分类,并在每轮结束后根据工作树打标签 | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

污染(taint)是一等输入:在接触过生产数据的会话中传入 `--tainted`(或让你的 hook 来传),请求就永远不会离开本机。如果没有配置本地端点,该决策会 abstain。

## 结果

相同的 spec、相同的标签、四个评测集(英语和日语的提示意图共 62 条请求;32 对日语材料的同案关联)。详细信息和完整表格见 [`docs/model-comparison.md`](../model-comparison.md)。

| 模型 | 许可证 | top-1(全部 / ja) | 是/否 Brier(原始) | 选出的校准 | 每题耗时(秒) |
|---|---|---|---|---|---|
| Kev-4B(专门训练的决策模型) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B(专门训练) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B,经 BonJev | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B,经 `shikii-readout`,原版 llama.cpp** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**措辞胜过校准。** 在提示意图任务上,21 个错误中有 19 个落在了 *other*;只改写选项措辞,在做任何校准之前,top-1 就从 0.68 提高到了 0.90。校准针对的是"排序正确,但置信度不够"的情况。`calibrate` 打印的混淆矩阵会告诉你属于哪一种。

**已经校准好的模型不需要 Platt。** Kev-4B 的是/否原始 Brier 为 0.046;Platt 在交叉验证下反而更差,所以写入的是 `identity`。`calibrate` 第一次选中 `platt`,是在通过 logits 读取的普通 LLM 上。是否需要校准,取决于*模型加读出方式*,而不是模型本身。

**普通 GGUF 能与专门训练的决策模型比肩。** 一个 27B ternary 聊天模型、主线 llama.cpp 0.7.0、一个适配器,没有 fork,没有训练,没有 Rust:top-1 与最好的 Apache 许可训练模型相同,日语为 1.000,开启 flash attention 并预热前缀后每题 0.58 秒。把 state 从选项之前挪到紧贴答案之前,同一份权重的准确率就从 0.919 升到 0.968,这就是布局被纳入校准键的原因。

## FAQ

**shikii 会运行模型吗?** 不会。它向你自己运行或租用的端点发送 System One 请求,并负责答案之后发生的一切。`shikii-readout` 在精神上算是唯一的例外:它把聊天模型的 logits 转换成同样的格式,但推理依然是 llama.cpp 完成的。

**为什么我的决策始终到不了 `act`?** 因为 spec 中没有 `(这个问题的哈希, 这个模型 id)` 对应的校准块。收集标签(`label`,或 `--labeled file`),运行 `calibrate --write`,`status` 就会显示 `calibrated: identity|platt|vector`。在 act 阈值上没有经过验证的 precision 的决策,不允许 act。这正是设计意图。

**我改了问题里的一个词,校准就没了。** 没错。哈希变了,那些常数是针对另一个问题拟合的。`status` 会把旧标签显示为 `stale hash`;等你有了新措辞对应的标签后,再运行一次 `calibrate --write`。

**我该用哪个模型?** 这取决于许可证、速度和语言,shikii 不替你决定。上面的表格和 [`docs/model-comparison.md`](../model-comparison.md) 就是论据:用同样的 spec 和同样的标签,你可以按数字来选,包括某个模型到底需不需要校准。`scripts/eval-model.sh <url> <name>` 可以为新端点复现这张表。

**支持日语吗?** 支持,而且差异正是体现在这里。Kev-4B 在日语提示上得分 0.913;27B 模型则是 1.000。在日语调查摘要的同案关联任务上,每个 27B 模型都能把正例和负例完全分开。

**我的数据会去哪里?** 只有渲染后的 state 会发往 `send_to` 中指定的端点。使用 `pii: possible`(默认)时,store 只保存输入的哈希,绝不保存原文。`--tainted` 会让一切留在本地。密钥只从 `DECISIONS_API_KEY` 或 `TYPESAFE_API_KEY` 读取,不会从别处读取。

**store 越来越大。** `shikii gc` 会删除超过 30 天且从未获得标签的记录。带标签的记录和标签会永久保留,因为它们是校准的材料。除非传入 `--apply`,否则只是 dry run。

**它会偏向第一个选项吗?** 先测量再纠正:`shikii bias <spec> --permutations 100` 会打乱选项顺序,并报告翻转次数和位置偏移。Kev-4B:100 次中 0 次翻转,偏移 ≤ 0.024。通过 logits 读取的普通 LLM 可能不同;这个命令就是为它们准备的。

## 文档

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): 设计笔记,框架负责什么、拒绝负责什么,验证计划以及已确认的内容。
- [`docs/operations.md`](../operations.md): 如何停止或回滚每一个运行中的部件,如何解读故障,还缺什么。
- [`docs/model-comparison.md`](../model-comparison.md): 对比表以及复现方法。
- [`docs/guide.ja.md`](../guide.ja.md): 日语长篇指南,包含每一条命令和一次完整的真实循环。
- [`integrations/readout.md`](../../integrations/readout.md)、[`integrations/bonjev.md`](../../integrations/bonjev.md)、[`integrations/claude-code.md`](../../integrations/claude-code.md): 我们在运行的三种连接。
- [`examples/`](../../examples/) 和 [`testdata/`](../../testdata/README.md): 一份没有校准、可作为起点的 spec,以及测试中使用的合成带标签数据集。

命令:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: 哈希过期、手写块、precision@act 低于 act → exit 1
shikii drift     <spec> --model M [--window N]            # 近期标签与校准时记录的 Brier 和 precision 对比
shikii bias      <spec> [--rows K] [--permutations N]     # 答案是否取决于选项顺序?
shikii status    [spec...] [--store DIR]                  # 盘点:记录、标签、来源、每个 (model, hash) 的状态
shikii gc        [spec...] [--older-than 30d] [--apply]   # 删除过期且无标签的记录;默认 dry run
shikii hash      <spec>
```

## 贡献

欢迎提交 issue 和 pull request。只有一条规矩:守护某个行为的测试,必须先展示它失败。故意破坏该行为,运行测试,记录它是如何失败的,然后修复,再记录它通过。文档为每条安全分支都列出了这些变异;请保持这份清单真实。

## 许可证

[Apache 2.0](../../LICENSE)。模型的许可证另行适用:Kev-4B 和 Bonsai 系列为 Apache 2.0,OpenJev 为 CC BY-NC 4.0,托管的 Jev 是 TypeSafe 的服务。shikii 保存的是你的概率和标签,不保存模型的任何内容。
