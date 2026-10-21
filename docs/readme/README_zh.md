[English](../../README.md) · [日本語](README_ja.md) · **简体中文** · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii：把 AI 的"大概是"变成业务上敢用的"可以放手"

### 让 AI 来决定"是/否""选哪个""到什么程度"，并用你的核对结果把它养好的工具。你要做的，只是在页面上回答"实际上是怎样"。

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-260%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../decision-ops-framework.md)

[问题在哪](#问题在哪) · [用法 1：测量 AI 自己的思考](#用法-1测量-ai-自己的思考) · [用法 2：把业务判断交给它](#用法-2把业务判断交给它) · [你在页面上做什么](#你在页面上做什么) · [安装](#安装) · [为什么可以换 AI](#为什么可以换-ai) · [继续阅读](#继续阅读)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## 问题在哪

问 AI"这条咨询和上一条是同一件事吗？"，它回答"大概是，0.9"。真正干活时，这还不够。这个 0.9 真的是 10 次里对 9 次吗？从换上新 AI 那天起，它是不是已经悄悄开始说错了？那些没人盯着、它自己做出的决定，现在还对吗？如果每次都要人来核对，用 AI 就没什么意义了。

shikii 从你的核对结果里画出一条**线**：线以上，AI 才可以自己动手。线画出来之前，什么都不信。线画出来之后，每一个自动决定都带着根据：这条线建立在多少条核对结果上、当时答对了多少。线留在问题和核对结果这一边，而不在 AI 里面，所以换 AI 不用人再花力气。

用法有两种：测量给你写代码的 AI 自己的思考（最轻），以及把业务里的一个判断交给它。

## 用法 1：测量 AI 自己的思考

面向用 Claude Code 或 Codex 的人。写代码的 agent 在收到请求时和做完时都在下判断；shikii 默默地测量它们。

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

就这一行。它会装上可执行文件，给 Claude Code 和 Codex 加两个 hook，放好 agent 的规则（skill），并在后台启动一个小的本地 AI（Kev-4B）（没有 llama.cpp 的话，它会打印出安装它的那一行命令）。打开一个新终端，像平时一样和 agent 干活就行（在 Codex 里先跑一次 `/hooks`，信任 shikii 的 hook）。测量的是四件事。

| 什么时候 | 问题 | 正确答案从哪来 |
|---|---|---|
| 收到请求时 | 这个请求最终会改动文件吗？ | 自动：到下一个请求之前，工作树有没有变化 |
| 收到请求时 | agent 动手之前，是否应该先和你商定做法？ | 你，在页面上 |
| 一轮结束时 | 改动是否在你要求的范围内？ | 你，在页面上 |
| 一轮结束时 | 你要求的事做完了吗？ | 你，在页面上 |

一轮结束时会出现一行 `shikii: 本次会话有 3 条决定等你回答: http://127.0.0.1:8096/`，有空的时候打开它（页面也由 hook 在后台启动）；每条答案只要几秒。积累 50 条答案后，线就画出来了，agent 的行为随之改变：需要先出计划的请求会变成一条给 agent 的提示，看起来没做完或超出范围的一轮会变成一行给你的提示，各自附上"线建立在 61 条正确答案上，人工核对过的精确率 0.96"这样的根据。在那之前 hook 只做记录。hook 从不拦下任何工具。

一切都放在一个地方 `~/.shikii/agent`，每个问题一条线，跨你所有的项目（这些问题问的是 agent 的思考方式，所以在每个仓库里正确答案都是一样的）。每条记录都标着它来自哪个仓库，页面可以按仓库筛选。详情：[hook](../../integrations/claude-code.md)（日语）。

## 用法 2：把业务判断交给它

面向你在工作里反复做的判断，比如"这条咨询是同一件事吗？""这条消息是变更请求吗？"：让 AI 来决定，再用你的核对结果把它养好。这里同样没有要敲的命令。你只要告诉 agent。

> 我想让 shikii 判断一条咨询和之前的是不是同一件事，并用我的核对结果把它养好

agent 按它的规则（skill）来做：在项目里创建 `.shikii/`（`shikii init`），定下问题、选项和输入，写出决策文件（`shikii new`；它会把问题和选项给你看一次，因为那是你的意图），调用判定（`shikii decide`），启动核对页面（http://127.0.0.1:8095）并告诉你。线画出来之前，没有任何决定会是 `act`（放手去做）。你在页面上回答，达到这个决定自己定下的数量（本例是 20 条）后，线会自动画出来，从那以后：

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (可以放手去做。这条线建立在 61 条正确答案上，其中 48 条由人回答；在人回答过、且这条线会让它放手去做的 25 条里，96% 是对的)
```

`band` 只有四个值：`act` = 放手去做，`ask` = 找人确认，`discard` = 什么都不做，`abstain` = 输入不足。你从来不会直接拿原始概率去行动。

- **手动操作**：`shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`。全部写在[指南](../guide.md)里
- **从 agent 调用**：`shikii init` 会写好 MCP 配置，所以 Claude Code、Cursor、OpenCode 和 Codex 可以直接调用工具 `shikii_decide` 和 `shikii_label`（[MCP](../../integrations/mcp.md)）
- **从你自己的程序调用**：启动 `shikii serve`，调用 `POST /v1/decide`（判定）和 `POST /v1/label`（核对）。请求体是 JSON，必须带 `-H 'Content-Type: application/json'`（[HTTP API](../guide.md#or-run-one-process-for-programs-and-people)）
- **现成的决策**：本仓库的 `decisions/` 里有六个（请求类型、重复的 bug 报告、同一咨询，……）。用 `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` 复制一个，再改写措辞（[列表](../../decisions/README.md)）
- **你已经有一张过去决定的表格**：`shikii import past.csv --spec .shikii/decisions/<id>.yaml` 把它变成核对结果，前 50 条就不必手动回答了
- **Docker**：还没有 Go，也没有模型？`docker compose up` 会启动一个本地 AI 和 shikii，页面在 http://127.0.0.1:8095（[详情](../../integrations/docker.md)）
- **别人的例子**：[咨询邮件分流](../examples/inquiry-route.md)（日语）。一位第一次使用的人只靠 README 走完了整个循环；决策文件和 22 条输入都在里面

## 你在页面上做什么

在核对页面（`shikii serve`）上，人只做三件事。其余的都是机器的工作。

1. **回答。** 页面上列出 AI 没把握的决定，再加上它自动执行的决定里每 20 条抽 1 条，你按下按钮告诉它实际是什么。按键 1 到 9 和 Enter（与 AI 的选择相同）可以快速过。"已回答"可以修正之前的答案（以最新的为准）
2. **写一个新问法。** 如果你觉得"这样问会更准"，就在"问法"面板里用自己的话输入。机器用同一批核对结果把它和现在的问法比一比（对 agent 自己的决定，每新增 50 条答案自动比一次），只在真正更好的时候才说"可以采用"。你也可以让 AI 来提议问法。你从不用打开文件
3. **按下采用或拒绝。** 你看看意思有没有变，然后按下去。数字由机器核对，意思由你核对

"详情"里还有每个模型的线的状态、"再问一次 AI"（用今天的问法和线对同一段文本重新判定，并把前后并排显示），以及知识面板（"这样问效果好"这类主张，连同它们的证据）。记录只追加，从不删除；新的决定会接替旧的位置。

## 安装

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # 需要 Go 1.27 或更新；没有的话用 Docker（见上）
```

install.sh 做的事：安装可执行文件（`shikii` 和 `shikii-readout`），给 Claude Code 和 Codex 加两个 hook，把 agent 的规则（skill）放到 `~/.claude/skills/shikii`，如果有 llama.cpp，就在后台启动本地 AI（Kev-4B，端口 :8080）。想改用托管版 Jev 或 OpenRouter，就把密钥放进环境变量（[可以接什么](../guide.md#what-you-can-connect)）。`shikii doctor` 会告诉你什么在跑、什么还缺。

每个项目的准备（`shikii init`）是 agent 的工作（你也可以自己跑）。它只建一个文件夹 `.shikii/`（里面 `decisions/` = 决策文件，`wiki/` = 知识，`records/` 和 `labels/` = 记录和核对结果）。项目根目录不会再多出别的东西。`decisions/`、`wiki/` 和 `labels/` 是换了 AI 也能留下来的资产，请提交到版本库；`records/` 会一直增长，提交与否随意（`shikii gc` 会清掉旧的）。它还会在你各个 agent 的配置里（Claude Code 的 `.mcp.json`、Cursor 的 `.cursor/mcp.json`、OpenCode 的 `opencode.json`、Codex 的 `~/.codex/config.toml`）加上 shikii 的条目，但不碰你自己写的条目。再跑一次也不会改动任何已有的东西。

命令输出和 hook 的消息是英文的（如果 Claude Code 的语言设置是日语，hook 的消息会在英文旁边附上日语）。页面有日语。更细的地方，比如只用托管 AI、`AGENTS.md` 和 `CLAUDE.md` 怎么取舍、修改页面本身要用的 Node，都在[指南](../guide.md)里。

## 为什么可以换 AI

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="shikii 的架构：人做的三件事，资产（问题、正确答案、根据），机器养大的循环，可验证的 wiki，可丢弃的模型" width="100%"></p>

- **知识积累在问题和核对结果里，而不是在 AI 里。** 核对结果比 AI 活得久，所以新的 AI 可以原样使用它们，线也会重新画。实测里，让机器选择问法和排版、再加上相似的例子，达到了和微调模型权重（LoRA）相同的准确率（[数字](../bench.md)）
- **自动决定附带根据。** 不是"95% 有信心"，而是"在人核对过的 48 条上精确率 0.96"
- **wiki 不是拿来读的，是拿来测的。** "这样问效果好"作为带数字的主张积累下来，可以在新的 AI 上重新测试。只有人才能判定一条主张已被证实

## 继续阅读

- [指南](../guide.md)：每条命令、HTTP API、常见问题 · [spec 参考](../../skills/shikii/references/spec.md)
- [hook](../../integrations/claude-code.md)（日语）：agent 自己的思考是怎么被测量的、区段 → 行为对照表、内部结构
- [架构](../architecture.md)（日语）：上面那张图背后的全貌
- [现成的决策](../../decisions/README.md) · [怎么问的 wiki](../../wiki/index.md) · [测出的数字](../bench.md)
- [接入](../../integrations/mcp.md)：MCP、给普通聊天模型用的适配器、Docker

**给贡献者**：先故意把一个测试弄坏，看着它失败，再修好。每个数字都来自脚本。只有人可以确认一条 wiki 主张。不加任何设置；用固定的位置和固定的规则来解决。

## 许可证

[Apache-2.0 OR MIT](../../LICENSE)。模型的许可证另算（Kev-4B 和 Bonsai 系列是 Apache 2.0，OpenJev 是 CC BY-NC 4.0，托管版 Jev 是 TypeSafe 的服务）。
