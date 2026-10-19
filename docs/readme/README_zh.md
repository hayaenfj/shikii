[English](../../README.md) · [日本語](README_ja.md) · **简体中文** · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii：把 AI 的"大概是"变成业务上敢用的"可以放手"

### 让 AI 来决定"是/否""选哪个""到什么程度"，并让这个决定在日常工作里一直靠得住的工具。你只需要核对一部分答案，偶尔确认一句"这还是我想要的吗？"。剩下的交给机器。

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-196%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[问题在哪](#问题在哪) · [shikii 做什么](#shikii-做什么) · [你做什么](#你做什么) · [安装](#安装) · [最快的入口](#最快的入口) · [走完一圈](#走完一圈) · [新在哪里](#新在哪里) · [继续阅读](#继续阅读)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii：模型给你一个概率，shikii 告诉你能不能照着做" width="100%"></p>

## 问题在哪

问 AI"这条咨询和上一条是同一件事吗？"，它回答"大概是，0.9"。真正干活时，这还不够。

- 这个 0.9 真的是 10 次里对 9 次吗？
- 从换上新模型那天起，它是不是已经悄悄开始说错了？
- 把问法稍微改一下，准确率会怎么变？
- 那些没人盯着、它自己做出的决定，现在还对吗？

如果每个答案都要人来核对，用 AI 就没什么意义了。

## shikii 做什么

1. **收集你的核对结果，画出一条"线"：线以上 AI 才可以自己动手。** 从你给出的答案（"实际上是这样"）里，它测出 AI 的自信程度与实际答对频率之间的关系，再决定从哪个位置往上可以信任。线画出来之前，什么都不会自动执行。线画出来之后，每一个自动决定都带着一份根据，写明这条线建立在多少条核对过的答案上、这些答案当时答对了多少
2. **尝试别的问法，留下更好的那个。** 用同一批核对结果给其他候选的问题、选项说明和提示词排版打分，只在真正更好的时候才采用。候选问法可以来自你（在页面上用自己的话输入），也可以来自 AI；每当新的核对结果积累到足够多，机器就再量一次。你只需确认意思没变
3. **换模型不必从头再来。** 核对结果与模型分开保存，新模型可以直接复用，线由机器重新画。不需要人手
4. **把学到的东西留在 wiki 里。** "这样问效果好"会连同测出的数字一起作为主张积累下来，可以在别的模型上重新验证。其他人在他们的模型上测到的结果也加到同一页

## 你做什么

- **写下你想决定什么。** 回答几个问题，一个决策文件就出来了（`shikii new`，可以交互式，也可以用 `--type --question --option --input --pii`；完整格式见 [spec 参考](../../skills/shikii/references/spec.md)）
- **核对答案。** 页面上会列出 AI 没把握的决定，再加上它自动执行的决定里每 20 条抽 1 条，你按下按钮告诉它实际是什么。按键 1 到 9 和 Enter（与 AI 的选择相同）可以快速过。"已回答"可以修正之前的答案（以最新的为准）。"详情"里多出一个"再问一次 AI"，用今天的问法和线对同一段文本重新判定，并把前后并排显示；记录只追加，从不删除
- **偶尔确认一句"这还是我想要的吗？"。** 当机器找到一种得分更高的问法时，你看看意思有没有变，然后按下采用或拒绝。要是你想到了更好的问法，就在同一个页面上输入；机器来测量，你从不用改文件

其余的都是机器的工作。

## 安装

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # needs Go 1.27 or newer
cd <project root> && shikii init
```

`shikii init` 一次性建好一个文件夹 `.shikii/`（里面 `decisions/` 放你的 spec，`wiki/` 放知识，`records/` 和 `labels/` 放记录和你的核对结果；项目根目录不会再多出别的东西），以及各个 agent 的接入：给 Claude Code（`.mcp.json`）、Cursor（`.cursor/mcp.json`）、OpenCode（`opencode.json`）和 Codex（`~/.codex/config.toml`）配好 shikii 的 MCP server，在 `AGENTS.md` 里加一节，并把详细规则（skill）放到 `~/.claude/skills/shikii`。你自己写的条目绝不会被动（已有的 shikii 条目也原样保留）。只有 skill 文件夹每次都会被替换，所以不要手改它。`wiki/` 会收到共享知识（每种问题类型该怎么问、有效的措辞模式、每个模型的习性）；改进问法的功能（`tune --propose`）会读取它，而你编辑过的页面不会被之后的 `init` 覆盖。没有 Go？用下面的 Docker。

这些条目指向本地模型（`shikii up`，端口 8080）。只用托管模型？在四个文件里各自手动删掉 `--local`。你不用的工具也会生成配置文件；删掉它们或加进 `.gitignore`（再跑一次 `shikii init` 不会碰已有的东西）。请把 `.shikii/decisions/`、`.shikii/wiki/` 和 `.shikii/labels/` 提交到版本库（你的 spec、知识和核对结果：这些是换模型也能留下来的资产）。`.shikii/records/`（每一条决定）会一直增长，提交与否随意（`shikii gc` 会清掉旧的）。如果 Codex 已经有 `mcp_servers`，就不会添加，而是打印出需要粘贴的几行。只有 `CLAUDE.md` 的项目不会生成 `AGENTS.md`（否则 OpenCode 会不再读 `CLAUDE.md`）。

要修改核对页面本身（`web/`，Svelte）需要 Node 24（`make web`）。只是使用 shikii 则不需要：构建好的页面已提交，并嵌入在可执行文件里。

## 最快的入口

- **Docker**：没有 Go，也没有模型？`docker compose up` 会启动一个本地模型（Kev-4B）和 shikii，核对页面在 http://127.0.0.1:8095（[详情](../../integrations/docker.md)）
- **现成的决策**：本仓库的 `decisions/` 里有六个（请求类型、重复的 bug 报告、同一咨询，……）。复制一个到你的 `.shikii/decisions/`，或用 `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` 以它为起点（[列表](../../decisions/README.md)）
- **你已经有一张过去决定的表格**：`shikii import past.csv --spec .shikii/decisions/<id>.yaml` 把它变成核对结果，前 50 条就不必手动回答了
- **从你自己的程序调用**：启动 `shikii serve`，调用 `POST /v1/decide`（判定）和 `POST /v1/label`（核对）。请求体是 JSON，必须带 `-H 'Content-Type: application/json'`（所有接口见[完整指南](../guide.md#or-run-one-process-for-programs-and-people)）
- **别人的例子**：[咨询邮件分流](../examples/inquiry-route.md)（日语）。一位第一次使用的人只靠 README 走完了整个循环；spec 和 22 条输入都在里面
- **测量 AI 自己的思考**（Claude Code 和 Codex）：`shikii hook install` 会装上两个 hook，在 agent 每一轮工作的前后各跑一次 shikii。之前问：这个请求最终会改动文件吗？agent 是否应该先和你商定做法？之后问：改动是否在你要求的范围内？你要求的事做完了吗？第一个正确答案是免费得到的，看工作树有没有变化就行；其余的由你在页面上回答。线画出来之前什么都不会变。之后，需要先出计划的请求会变成一条给 agent 的提示，看起来没做完或超出范围的一轮会变成一行给你的提示，并附上这条线建立在多少条正确答案上以及人工核对过的精确率。hook 从不拦下任何工具（[详情](../../integrations/claude-code.md)，日语）
- **在 CI 里**：`uses: hayaenfj/shikii@main` 会在每个 pull request 上跑 `shikii check`，问法被改动或线被削弱时构建就会失败

## 走完一圈

```sh
shikii up                                                             # start the local model (downloaded on first run)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (no line yet, so: ask a person)
shikii serve --local http://127.0.0.1:8080                                   # specs and knowledge are read from .shikii/
#   → answer at http://127.0.0.1:8095. Once a decision has the number it asks for (eval.min_n, 20 for this one) the line is drawn by itself
#     To try with fewer answers, lower eval.min_n in the spec to what you have (a line written with --force is flagged by check)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (safe to act, with the evidence: the line rests on 61 corrections, 48 of them from people. Of the human answers that this line puts on the act side, 25, 96% were right)
```

区段（band）只有四种。`act` = 放手去做，`ask` = 找人确认，`discard` = 什么都不做，`abstain` = 输入不足。你永远不需要去读一个原始概率。

## 新在哪里

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="shikii 的架构：人做的三件事，资产（问题、正确答案、根据），机器养大的循环，可验证的 wiki，可丢弃的模型" width="100%"></p>

- **知识积累在问题和核对结果里，而不是在模型里。** 所以模型是可丢弃的，换模型不需要人手。实测：让机器选择问法和排版、再加上相似的例子，一个普通的基础模型在准确率上追平了微调（LoRA）（[数字](../bench.md)）
- **自动决定附带证据。** 不是"95% 有信心"，而是"在人核对过的 48 条答案上精确率 0.96"
- **wiki 不是拿来读的，是拿来测的。** "这样问效果好"作为带数字的主张积累下来，并在新模型上重新测试。只有人才能判定一条主张已被证实

## 继续阅读

- [完整指南](../guide.md)：每条命令、完整的快速上手、HTTP API、可以接什么、测出的结果和常见问题 · [spec 参考](../../skills/shikii/references/spec.md)
- [设计笔记](../decision-ops-framework.md)：这个框架负责什么、拒绝负责什么，以及验证过什么 · [架构](../architecture.md)（日语）
- [现成的决策](../../decisions/README.md) · [怎么问的 wiki](../../wiki/index.md) · [基准测试](../bench.md) · [模型对比](../model-comparison.md) · [运维日志](../operations.md)
- [接入](../../integrations/mcp.md)：MCP、把普通聊天模型变成决策模型的适配器、Docker、Claude Code
- [日语指南](../guide.ja.md)，含一个完整的真实循环

## 参与贡献

欢迎提 issue 和 pull request。守护某个行为的测试要先展示它失败：故意把行为破坏掉，跑测试，记下它怎么失败的，再修好并记下它通过。文档里的每个数字都来自脚本。只有人可以改变 wiki 主张的状态（`confirmed` / `tentative` / `rejected`），并附上作为证据的那次运行。不加新选项：靠布局（项目根目录下的 `decisions/` `wiki/` `.shikii/`）和固定规则来解决。

## 许可证

[Apache 2.0](../../LICENSE)。模型的许可证另算：Kev-4B 和 Bonsai 系列是 Apache 2.0，OpenJev 是 CC BY-NC 4.0，托管版 Jev 是 TypeSafe 的服务。shikii 只保存你的概率和标签，不保存模型的任何东西。
