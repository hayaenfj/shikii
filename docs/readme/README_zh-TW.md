[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · **繁體中文** · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii：把 AI 的「大概是」變成業務上敢用的「可以放手」

### 讓 AI 來決定「是/否」「選哪個」「到什麼程度」，並從你的核對結果裡把它養大的工具。你要做的，只是在頁面上回答「實際上是怎樣」。

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-211%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[問題在哪](#問題在哪) · [用法 1：量測 AI 自己的思考](#用法-1量測-ai-自己的思考) · [用法 2：把業務決定交給它](#用法-2把業務決定交給它) · [你在頁面上做什麼](#你在頁面上做什麼) · [安裝](#安裝) · [為什麼可以換 AI](#為什麼可以換-ai) · [繼續閱讀](#繼續閱讀)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## 問題在哪

問 AI「這則諮詢和上一則是同一件事嗎？」，它回答「大概是，0.9」。真正做事時，這還不夠。這個 0.9 真的是 10 次裡對 9 次嗎？從你換上新 AI 的那天起，它是不是已經悄悄變成了謊話？那些沒人盯著、它自己做出的決定，現在還對嗎？如果每次都要人來核對，用 AI 就沒什麼意義了。

shikii 從你的核對結果裡畫出一條**線**：線以上，AI 才可以自己動手。線畫出來之前，什麼都不信任。線畫出來之後，每一個自動決定都帶著根據：這條線建立在多少條核對結果上、答對過多少次。線留在問題和核對結果那一邊，不在 AI 裡面，所以換 AI 不花人手。

用法有兩種：量測替你寫程式的 AI 自己的思考（最輕的一種），以及把你業務裡的一項決定交給它。

## 用法 1：量測 AI 自己的思考

給用 Claude Code 或 Codex 的人。寫程式的 agent 在收到請求時和完成工作時都會做判斷；shikii 默默地量測它們。

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

就這樣。它會安裝執行檔，替 Claude Code 和 Codex 加上兩個 hook，放好 agent 的規則（skill），並在背景啟動一個小的本機 AI（Kev-4B）（沒有 llama.cpp 的話，會印出安裝它的那一行）。開一個新的終端機，照常和 agent 一起工作就好（在 Codex 裡，先跑一次 `/hooks` 來信任 shikii 的 hook）。量測的有四件事。

| 時機 | 問題 | 正確答案從哪裡來 |
|---|---|---|
| 收到請求時 | 這個請求最終會改動檔案嗎？ | 自動：下一個請求之前，工作樹有沒有變化 |
| 收到請求時 | agent 動手之前，應該先和你商定做法嗎？ | 你，在頁面上 |
| 一輪結束時 | 改動是否在你要求的範圍內？ | 你，在頁面上 |
| 一輪結束時 | 你要求的事做完了嗎？ | 你，在頁面上 |

一輪結束時會出現一行 `shikii: 這個工作階段有 3 條決定等你回答: http://127.0.0.1:8096/`，有空的時候打開就好（hook 也會在背景把頁面啟動起來）；每個答案只要幾秒。累積 50 個答案之後線就畫出來，agent 的行為隨之改變：需要先出計畫的請求會變成一條給 agent 的提示，看起來沒做完或超出範圍的一輪會變成一行給你的提示，兩者都附帶根據，例如「線建立在 61 條正確答案上，人工核對的精確率 0.96」。在那之前，hook 只做紀錄。hook 從不攔下任何工具。

所有東西都放在同一個地方 `~/.shikii/agent`，每個問題一條線，跨你所有的專案（這些問題問的是 agent 的思考方式，所以正確答案在每個儲存庫裡都一樣）。每條紀錄都標記了它來自哪個儲存庫，頁面可以按儲存庫篩選。詳情：[hook](../../integrations/claude-code.md)（日文）。

## 用法 2：把業務決定交給它

像「這則諮詢是同一件事嗎？」「這則訊息是變更請求嗎？」這類你在工作裡反覆做的決定：讓 AI 來決定，再從你的核對結果裡把它養大。這裡同樣沒有要你打的命令。你跟 agent 說就行。

> 我想讓 shikii 來判定一則諮詢是不是和之前的某一則同一件事，並從我的核對結果裡把它養大

agent 會照它的規則（skill）進行：在專案裡建立 `.shikii/`（`shikii init`），定下問題、選項和輸入，寫出決策檔案（`shikii new`；它會把問題和選項給你看一次，因為那是你的意圖），呼叫判定（`shikii decide`），啟動核對頁面（http://127.0.0.1:8095），然後告訴你。線畫出來之前，沒有任何決定會是 `act`（放手去做）。隨著你在頁面上回答，一到這個決定自己定下的數量（這個例子是 20 條），線就自動畫出來，從此以後：

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (可以放手去做。這條線建立在 61 條正確答案上，其中 48 條是人回答的；在人回答的答案裡，這條線會放行的那 25 條，有 96% 是對的)
```

`band` 只有四種值：`act` = 放手去做，`ask` = 找人確認，`discard` = 什麼都不做，`abstain` = 輸入不足。你照著做的從來不是原始機率。

- **手動**：`shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`。全部都在[指南](../guide.md)裡
- **從 agent**：`shikii init` 會寫好 MCP 設定，所以 Claude Code、Cursor、OpenCode 和 Codex 可以直接呼叫 `shikii_decide` 和 `shikii_label` 這兩個工具（[MCP](../../integrations/mcp.md)）
- **從你自己的程式**：啟動 `shikii serve`，呼叫 `POST /v1/decide`（判定）和 `POST /v1/label`（核對）。請求內容是 JSON，必須帶 `-H 'Content-Type: application/json'`（[HTTP API](../guide.md#or-run-one-process-for-programs-and-people)）
- **現成的決策**：本儲存庫的 `decisions/` 裡有六個（請求類型、重複的 bug 回報、同一諮詢，……）。用 `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` 複製一個再改寫（[清單](../../decisions/README.md)）
- **你已經有一張過去決定的表格**：`shikii import past.csv --spec .shikii/decisions/<id>.yaml` 把它變成核對結果，前 50 條就不必手動回答了
- **Docker**：還沒有 Go，也沒有模型？`docker compose up` 會啟動一個本機 AI 和 shikii，頁面在 http://127.0.0.1:8095（[詳情](../../integrations/docker.md)）
- **別人的例子**：[諮詢郵件分流](../examples/inquiry-route.md)（日文）。一位第一次使用的人只靠 README 走完了整個循環；決策檔案和 22 筆輸入都在裡面

## 你在頁面上做什麼

在核對頁面（`shikii serve`）上，人做三件事。其餘的都是機器的工作。

1. **回答。** 頁面上會列出 AI 沒把握的決定，再加上它自動執行的決定裡每 20 條抽 1 條，你按下按鈕告訴它實際是什麼。按鍵 1 到 9 和 Enter（與 AI 的選擇相同）可以快速過。「已回答」可以修正之前的答案（以最新的為準）
2. **寫一個新問法。** 要是你覺得「這樣問應該更準」，就在「問法」面板裡用自己的話輸入。機器會用同一批核對結果替它和目前的問法打分（agent 自己的決定，每新增 50 個答案就自動量一次），只在真正更好的時候才說「可以採用」。你也可以讓 AI 來提新問法。你從不用打開檔案
3. **按下採用或拒絕。** 你看看意思有沒有變，然後按下去。機器核對數字；你核對意思

「詳情」裡會多出每個模型的線的狀態、「再問一次 AI」（用今天的問法和線對同一段文字重新判定，並把前後並排顯示），以及知識面板（「這樣問效果好」這一類的主張，連同它們的證據）。紀錄只追加，從不刪除；新的決定會接替舊的位置。

## 安裝

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 或更新；沒有的話用 Docker（見上）
```

install.sh 做的事：安裝執行檔（`shikii` 和 `shikii-readout`），替 Claude Code 和 Codex 加上兩個 hook，把 agent 的規則（skill）放到 `~/.claude/skills/shikii`，而且如果有 llama.cpp，就在背景啟動本機 AI（Kev-4B，連接埠 :8080）。想改用託管版 Jev 或 OpenRouter，把金鑰放進環境變數（[可以接什麼](../guide.md#what-you-can-connect)）。`shikii doctor` 會告訴你什麼在跑、什麼還缺。

每個專案的準備（`shikii init`）是 agent 的工作（你也可以自己跑）。它只建立一個資料夾 `.shikii/`（裡面：`decisions/` = 決策檔案，`wiki/` = 知識，`records/` 和 `labels/` = 紀錄和核對結果）。專案根目錄不會再多出別的東西。`decisions/`、`wiki/` 和 `labels/` 是換 AI 也能留下來的資產，所以請提交到版本庫；`records/` 會一直增長，提交與否隨意（`shikii gc` 會清掉舊的）。它也會在你各個 agent 的設定裡（Claude Code 的 `.mcp.json`、Cursor 的 `.cursor/mcp.json`、OpenCode 的 `opencode.json`、Codex 的 `~/.codex/config.toml`）加上一條 shikii 的條目，而不碰你自己寫的條目。再跑一次，已有的東西不會改變。

命令的輸出和 hook 的訊息是英文（如果 Claude Code 的語言設定是日文，hook 的訊息會在英文旁邊附上日文）。頁面有日文。更細的地方，例如只用託管 AI、`AGENTS.md` 和 `CLAUDE.md` 的取捨，以及修改頁面本身要用的 Node，都在[指南](../guide.md)裡。

## 為什麼可以換 AI

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="shikii 的架構：人做的三件事，資產（問題、正確答案、根據），機器養大的循環，可驗證的 wiki，可丟棄的模型" width="100%"></p>

- **知識累積在問題和核對結果裡，而不是在 AI 裡。** 核對結果比 AI 活得久，所以新的 AI 可以原樣沿用，線由機器重新畫。在我們的實測裡，讓機器選擇問法和排版、再加上相似的例子，達到了和微調模型權重（LoRA）相同的準確率（[數字](../bench.md)）
- **自動決定附帶根據。** 不是「95% 有信心」，而是「在人核對過的 48 筆上精確率 0.96」
- **wiki 不是拿來讀的，是拿來量的。** 「這樣問效果好」作為帶數字的主張累積下來，並可以在新的 AI 上重新測試。只有人才能判定一條主張已被證實

## 繼續閱讀

- [指南](../guide.md)：每條命令、HTTP API、常見問題 · [spec 參考](../../skills/shikii/references/spec.md)
- [hook](../../integrations/claude-code.md)（日文）：agent 自己的思考是怎麼量的、區段 → 行為的對照表、內部結構
- [架構](../architecture.md)（日文）：上面那張圖背後的全貌
- [現成的決策](../../decisions/README.md) · [怎麼問的 wiki](../../wiki/index.md) · [量到的數字](../bench.md) · [營運筆記](../operations.md)
- [接入](../../integrations/mcp.md)：MCP、把普通聊天模型接進來的轉接器、Docker

**給參與貢獻的人**：先故意把測試弄壞、看它失敗，再修好。每個數字都來自腳本。只有人可以確認一條 wiki 主張。不加新設定；靠固定的位置和固定的規則來解決。

## 授權

[Apache 2.0](../../LICENSE)。模型的授權另計（Kev-4B 和 Bonsai 系列是 Apache 2.0，OpenJev 是 CC BY-NC 4.0，託管版 Jev 是 TypeSafe 的服務）。
