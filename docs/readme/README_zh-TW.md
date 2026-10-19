[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · **繁體中文** · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii：把 AI 的「大概是」變成業務上敢用的「可以放手」

### 讓 AI 來決定「是/否」「選哪個」「到什麼程度」，並讓這個決定在日常工作中一直靠得住的工具。你只需要核對一部分答案，偶爾確認一句「這還是我想要的嗎？」。剩下的交給機器。

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-193%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[問題在哪](#問題在哪) · [shikii 做什麼](#shikii-做什麼) · [你做什麼](#你做什麼) · [安裝](#安裝) · [最快的入口](#最快的入口) · [走完一圈](#走完一圈) · [新在哪裡](#新在哪裡) · [繼續閱讀](#繼續閱讀)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii：模型給你一個機率，shikii 告訴你能不能照著做" width="100%"></p>

## 問題在哪

問 AI「這則諮詢和上一則是同一件事嗎？」，它回答「大概是，0.9」。真正做事時，這還不夠。

- 這個 0.9 真的是 10 次裡對 9 次嗎？
- 從換上新模型那天起，它是不是已經悄悄開始說錯了？
- 把問法稍微改一下，準確率會怎麼變？
- 那些沒人盯著、它自己做出的決定，現在還對嗎？

如果每個答案都要人來核對，用 AI 就沒什麼意義了。

## shikii 做什麼

1. **收集你的核對結果，畫出一條「線」：線以上 AI 才可以自己動手。** 從你給出的答案（「實際上是這樣」）裡，它量出 AI 的自信程度與實際答對頻率之間的關係，再決定從哪個位置往上可以信任。線畫出來之前，什麼都不會自動執行。線畫出來之後，每一個自動決定都帶著一份根據，寫明這條線建立在多少條核對過的答案上、這些答案當時答對了多少
2. **嘗試別的問法，留下更好的那個。** 用同一批核對結果替其他候選的問題、選項說明和提示詞排版打分，只在真正更好的時候才採用。候選問法可以來自你（在頁面上用自己的話輸入），也可以來自 AI；每當新的核對結果累積到足夠多，機器就再量一次。你只需確認意思沒變
3. **換模型不必從頭再來。** 核對結果與模型分開保存，新模型可以直接沿用，線由機器重新畫。不需要人手
4. **把學到的東西留在 wiki 裡。** 「這樣問效果好」會連同量出的數字一起作為主張累積下來，可以在別的模型上重新驗證。其他人在他們的模型上量到的結果也加到同一頁

## 你做什麼

- **寫下你想決定什麼。** 回答幾個問題，一個決策檔案就出來了（`shikii new`，可以互動式，也可以用 `--type --question --option --input --pii`；完整格式見 [spec 參考](../../skills/shikii/references/spec.md)）
- **核對答案。** 頁面上會列出 AI 沒把握的決定，再加上它自動執行的決定裡每 20 條抽 1 條，你按下按鈕告訴它實際是什麼。按鍵 1 到 9 和 Enter（與 AI 的選擇相同）可以快速過。「已回答」可以修正之前的答案（以最新的為準）。「詳情」裡多出一個「再問一次 AI」，用今天的問法和線對同一段文字重新判定，並把前後並排顯示；紀錄只追加，從不刪除
- **偶爾確認一句「這還是我想要的嗎？」。** 當機器找到一種得分更高的問法時，你看看意思有沒有變，然後按下採用或拒絕。要是你想到了更好的問法，就在同一個頁面上輸入；機器來量測，你從不用改檔案

其餘的都是機器的工作。

## 安裝

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # needs Go 1.27 or newer
cd <project root> && shikii init
```

`shikii init` 一次建好一個資料夾 `.shikii/`（裡面 `decisions/` 放你的 spec，`wiki/` 放知識，`records/` 和 `labels/` 放紀錄和你的核對結果；專案根目錄不會再多出別的東西），以及各個 agent 的接入：替 Claude Code（`.mcp.json`）、Cursor（`.cursor/mcp.json`）、OpenCode（`opencode.json`）和 Codex（`~/.codex/config.toml`）設好 shikii 的 MCP server，在 `AGENTS.md` 裡加一節，並把詳細規則（skill）放到 `~/.claude/skills/shikii`。你自己寫的條目絕不會被動（已有的 shikii 條目也原樣保留）。只有 skill 資料夾每次都會被換掉，所以不要手動改它。`wiki/` 會收到共享知識（每種問題類型該怎麼問、有效的措辭模式、每個模型的習性）；改進問法的功能（`tune --propose`）會讀取它，而你編輯過的頁面不會被之後的 `init` 覆寫。沒有 Go？用下面的 Docker。

這些條目指向本機模型（`shikii up`，連接埠 8080）。只用託管模型？在四個檔案裡各自手動刪掉 `--local`。你不用的工具也會產生設定檔；刪掉它們或加進 `.gitignore`（再跑一次 `shikii init` 不會碰已有的東西）。請把 `.shikii/decisions/`、`.shikii/wiki/` 和 `.shikii/labels/` 提交到版本庫（你的 spec、知識和核對結果：這些是換模型也能留下來的資產）。`.shikii/records/`（每一條決定）會一直增長，提交與否隨意（`shikii gc` 會清掉舊的）。如果 Codex 已經有 `mcp_servers`，就不會添加，而是印出需要貼上的幾行。只有 `CLAUDE.md` 的專案不會產生 `AGENTS.md`（否則 OpenCode 會不再讀 `CLAUDE.md`）。

要修改核對頁面本身（`web/`，Svelte）需要 Node 24（`make web`）。只是使用 shikii 則不需要：建好的頁面已提交，並嵌在執行檔裡。

## 最快的入口

- **Docker**：沒有 Go，也沒有模型？`docker compose up` 會啟動一個本機模型（Kev-4B）和 shikii，核對頁面在 http://127.0.0.1:8095（[詳情](../../integrations/docker.md)）
- **現成的決策**：本儲存庫的 `decisions/` 裡有六個（請求類型、重複的 bug 回報、同一諮詢，……）。複製一個到你的 `.shikii/decisions/`，或用 `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` 以它為起點（[清單](../../decisions/README.md)）
- **你已經有一張過去決定的表格**：`shikii import past.csv --spec .shikii/decisions/<id>.yaml` 把它變成核對結果，前 50 條就不必手動回答了
- **從你自己的程式呼叫**：啟動 `shikii serve`，呼叫 `POST /v1/decide`（判定）和 `POST /v1/label`（核對）。請求內容是 JSON，必須帶 `-H 'Content-Type: application/json'`（所有端點見[完整指南](../guide.md#or-run-one-process-for-programs-and-people)）
- **別人的例子**：[諮詢郵件分流](../examples/inquiry-route.md)（日文）。一位第一次使用的人只靠 README 走完了整個循環；spec 和 22 筆輸入都在裡面
- **量測 AI 自己的思考**（Claude Code 和 Codex）：`shikii hook install` 會裝上兩個 hook，在 agent 每一輪工作的前後各跑一次 shikii。之前問：這個請求最終會改動檔案嗎？agent 是否應該先和你商定做法？之後問：改動是否在你要求的範圍內？你要求的事做完了嗎？第一個正確答案是免費得到的，看工作樹有沒有變化就行；其餘的由你在頁面上回答。線畫出來之前什麼都不會變。之後，需要先出計畫的請求會變成一條給 agent 的提示，看起來沒做完或超出範圍的一輪會變成一行給你的提示，並附上這條線建立在多少條正確答案上以及人工核對過的精確率。hook 從不攔下任何工具（[詳情](../../integrations/claude-code.md)，日文）
- **在 CI 裡**：`uses: hayaenfj/shikii@main` 會在每個 pull request 上跑 `shikii check`，問法被改動或線被削弱時建置就會失敗

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

區段（band）只有四種。`act` = 放手去做，`ask` = 找人確認，`discard` = 什麼都不做，`abstain` = 輸入不足。你永遠不需要去讀一個原始機率。

## 新在哪裡

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="shikii 的架構：人做的三件事，資產（問題、正確答案、根據），機器養大的循環，可驗證的 wiki，可丟棄的模型" width="100%"></p>

- **知識累積在問題和核對結果裡，而不是在模型裡。** 所以模型是可丟棄的，換模型不需要人手。實測：讓機器選擇問法和排版、再加上相似的例子，一個普通的基礎模型在準確率上追平了微調（LoRA）（[數字](../bench.md)）
- **自動決定附帶證據。** 不是「95% 有信心」，而是「在人核對過的 48 筆答案上精確率 0.96」
- **wiki 不是拿來讀的，是拿來量的。** 「這樣問效果好」作為帶數字的主張累積下來，並在新模型上重新測試。只有人才能判定一條主張已被證實

## 繼續閱讀

- [完整指南](../guide.md)：每條命令、完整的快速上手、HTTP API、可以接什麼、量到的結果和常見問題 · [spec 參考](../../skills/shikii/references/spec.md)
- [設計筆記](../decision-ops-framework.md)：這個框架負責什麼、拒絕負責什麼，以及驗證過什麼 · [架構](../architecture.md)（日文）
- [現成的決策](../../decisions/README.md) · [怎麼問的 wiki](../../wiki/index.md) · [基準測試](../bench.md) · [模型比較](../model-comparison.md) · [營運日誌](../operations.md)
- [接入](../../integrations/mcp.md)：MCP、把普通聊天模型變成決策模型的轉接器、Docker、Claude Code
- [日文指南](../guide.ja.md)，含一個完整的真實循環

## 參與貢獻

歡迎提 issue 和 pull request。守護某個行為的測試要先展示它失敗：故意把行為破壞掉，跑測試，記下它怎麼失敗的，再修好並記下它通過。文件裡的每個數字都來自腳本。只有人可以改變 wiki 主張的狀態（`confirmed` / `tentative` / `rejected`），並附上作為證據的那次執行。不加新選項：靠佈局（專案根目錄下的 `decisions/` `wiki/` `.shikii/`）和固定規則來解決。

## 授權

[Apache 2.0](../../LICENSE)。模型的授權另計：Kev-4B 和 Bonsai 系列是 Apache 2.0，OpenJev 是 CC BY-NC 4.0，託管版 Jev 是 TypeSafe 的服務。shikii 只保存你的機率和標籤，不保存模型的任何東西。
