[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · **繁體中文** · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · [Tiếng Việt](README_vi.md) · [Bahasa Indonesia](README_id.md)

# shikii: 營運決策模型

### 為每一個決策模型提供同一套生命週期: define → decide → record → label → calibrate → check → drift。

**模型給你一個機率。shikii 告訴你能不能據此行動。**

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-179%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[設計筆記](../decision-ops-framework.md) · [維運手冊](../operations.md) · [模型比較](../model-comparison.md) · [詳細用法(日文)](../guide.ja.md)**

[快速開始](#快速開始) · [運作原理](#運作原理) · [可連接的對象](#可連接的對象) · [結果](#結果) · [FAQ](#faq) · [文件](#文件)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii 從不執行模型,也從不讀取鑰匙圈。它只與你既有的決策模型端點通訊、保存紀錄,並且在*這個問題在這個模型上*的機率經標籤驗證之前,拒絕 **act**。校準區塊一律由工具產生,絕不手寫。

---

決策模型(TypeSafe Jev、llama.cpp 上的 Kev-4B 和 OpenJev、OpenRouter Decisions 等)回答具型別的問題 `choice`、`score` 和 `noul`(是/否),只回傳機率,不產生文字。每家廠商都附有同樣的免責聲明:*這些機率並未針對你的資料校準,閾值也無法跨模型、跨問題通用。*

shikii 負責的是沒人提供的那一塊:決策的**生命週期**。一個 spec 檔記錄問題、選項、state 範本與閾值。`decide` 向模型提問,回傳一個檔位(`act` / `ask` / `discard` / `abstain`),而不是原始數字。`label` 記錄實際的真相。`calibrate` 進行擬合並做交叉驗證,再以「問題雜湊 + 模型名稱」為鍵,把結果寫回 spec。`check` 在措辭或模型發生漂移時讓 CI 失敗。`gc` 讓 store 保持精簡。

它還附帶 `shikii-readout`,一個 500 行的轉接器,透過讀取選項字母的 logits,把**原版 llama.cpp 上的任意聊天 GGUF** 變成決策模型。在我們的量測中,以這種方式讀取的一般 27B ternary 模型,準確率與專門訓練的決策模型相當,在筆記型電腦上每題只需 0.6 秒。

> 這個名字來自日文:*shikii* (閾) 在日文中意指閾值,也就是把機率變成行動的那條界線。CLI 名為 `shikii`。

## 快速開始

需要 Go 1.27 或更新版本。第一次執行不需要其他任何東西。

### 不需要金鑰和 GPU,跑完整個迴圈

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # 假模型:decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # 機率過度自信的假模型,觀察 calibrate 如何選出 platt
```

### 接上真實模型

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) 以 llama.cpp 執行的本機決策模型(第一次執行會下載 Kev-4B,約 2.5 GB)
make server                                       # llama-server 監聽 :8080
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) 託管的 TypeSafe Jev。金鑰只從環境變數讀取
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

回傳結果包含檔位、trace id 與各機率。在 spec 中寫入該模型的校準之前,檔位最高只到 `ask`。

### 閉合迴圈

```sh
bin/shikii label <trace_id> change_request          # or:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <model name from the response> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1 when the wording changed or precision@act fell below act
```

### 把任意 GGUF 當作決策模型

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # stock llama-server with flash attention and 8-bit KV, :8086
make readout                                               # shikii-readout in front of it, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### 安裝與更新

```sh
make install          # copies bin/shikii to ~/.local/bin/shikii
git pull && make install
```

## 運作原理

每個決策就是一個 YAML 檔:問題、選項、state 範本、state 可以送往何處、閾值,以及由 `calibrate --write` 產生的校準區塊。

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
  required: [message]            # 缺少輸入 → abstain;絕不呼叫模型
policy:
  send_to: [hosted, local]       # 被汙染(tainted)的工作階段會自動略過 hosted
  pii: possible                  # 紀錄只保存輸入的雜湊,不保存原文
thresholds:
  act: 0.75                      # ≥ act → 自動 act,且僅限已校準時
  ask: 0.60                      # ≥ ask → 詢問人;低於 → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**校準的鍵是 `(問題 + 範本 + 選項的雜湊, 端點回傳的模型 id)`。** 改動問題中的一個字,或是讓 `jev-latest` 滾動到新版本,校準就會直接失效:檔位會降為 `ask`,而不是悄悄沿用過期的常數繼續行動。prompt 版面配置也是鍵的一部分:`shikii-readout` 會在模型名稱中帶上它的版面配置(`…+state-last`),因為光是把 state 在 prompt 中挪個位置,就讓同一份權重的 27B 模型準確率變動了 5 個百分點。

`calibrate` 在 5 折交叉驗證下比較三種方法,並寫入勝出者。`identity` 表示原始機率已經滿足 precision@act,模型只需標記為已驗證。`platt`(σ((Δ−bias)/T))用於是/否問題。`vector`(softmax((log p + b)/T))用於 choice 和 score,可修正每個選項上的固有習慣,例如「拿不準時就退到 *other*」。部分標籤(`!option`,即「不是*那個*」)計入 precision,但不參與擬合。從檔案取得的標籤會被持久化,因此之後的 `check` 和 `drift` 可以離線執行。

每一條與安全相關的分支(taint 路由、未校準不得 act、雜湊過期失效、標籤驗證、反轉偵測、以 CV 決定方法、`gc` 的 dry-run 等),都有一筆留有紀錄的變異:修正前先讓測試失敗,才接受修正。共有 63 個 Go 測試,分布在 8 個套件中;示範會在 CI 中執行。

## 可連接的對象

shikii 使用 System One 請求格式(`POST /v1/systemone`),因此任何提供該介面的服務都能接入。其餘的由轉接器涵蓋。

| 對象 | shikii 得到什麼 | 需求與指南 |
|---|---|---|
| **TypeSafe Jev**(託管) | 附機率的 `choice` / `score` / `noul`,模型 id 形如 `jev-1.13.0` | 環境變數中設定 `TYPESAFE_API_KEY`。`--hosted` 的預設值 |
| **llama.cpp 決策模型**(Kev-4B、OpenJev、Clef 等) | 相同的格式,在本機執行,模型 id 來自 `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M`(`make server`)。llama.cpp 0.6.0 或更新版本 |
| **OpenRouter Decisions API** | 相同的格式,經由 OpenRouter | `DECISIONS_API_KEY`,`--hosted https://openrouter.ai/api/alpha/decisions` |
| **任意聊天 GGUF**,經 `shikii-readout` | 以 System One 格式提供字母 logit 讀出,本身不帶校準 | 其後接原版 `llama-server` 0.7.0。[`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai**,經 [BonJev](https://github.com/ChezRD/bonjev) | 同上,使用 BonJev 自己的 prompt 與內建校準 | Rust 建置與 Prism fork。[`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **假模型**(`bin/fake-systemone`) | 供示範與測試用的確定性機率 | 不需要任何東西。`make fake` |
| **Claude Code hook**(claude-jev) | 對每則提示分類,並於每一輪結束後依工作樹貼上標籤 | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

汙染(taint)是一等輸入:在碰過正式環境資料的工作階段中傳入 `--tainted`(或讓你的 hook 來傳),請求就絕不會離開本機。若未設定本機端點,該決策會 abstain。

## 結果

相同的 spec、相同的標籤、四個評測集(英文與日文的提示意圖共 62 筆請求;32 組日文資料的同案關聯)。詳細資訊與完整表格請見 [`docs/model-comparison.md`](../model-comparison.md)。

| 模型 | 授權 | top-1(全部 / ja) | 是/否 Brier(原始) | 選出的校準 | 每題耗時(秒) |
|---|---|---|---|---|---|
| Kev-4B(專門訓練的決策模型) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B(專門訓練) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B,經 BonJev | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B,經 `shikii-readout`,原版 llama.cpp** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**措辭勝過校準。** 在提示意圖任務上,21 個錯誤中有 19 個落在 *other*;只改寫選項措辭,在做任何校準之前,top-1 就從 0.68 提升到 0.90。校準針對的是「排序正確,但信心不足」的情況。`calibrate` 印出的混淆矩陣會告訴你屬於哪一種。

**已經校準好的模型不需要 Platt。** Kev-4B 的是/否原始 Brier 為 0.046;Platt 在交叉驗證下反而更差,所以寫入的是 `identity`。`calibrate` 第一次選中 `platt`,是在透過 logits 讀取的一般 LLM 上。是否需要校準,取決於*模型加讀出方式*,而不是模型本身。

**一般 GGUF 能與專門訓練的決策模型並駕齊驅。** 一個 27B ternary 聊天模型、主線 llama.cpp 0.7.0、一個轉接器,沒有 fork,沒有訓練,沒有 Rust:top-1 與最好的 Apache 授權訓練模型相同,日文為 1.000,開啟 flash attention 並預熱前綴後每題 0.58 秒。把 state 從選項之前移到緊貼答案之前,同一份權重的準確率就從 0.919 升到 0.968,這就是版面配置被納入校準鍵的原因。

## FAQ

**shikii 會執行模型嗎?** 不會。它向你自己架設或租用的端點送出 System One 請求,並負責答案之後發生的一切。`shikii-readout` 在精神上算是唯一的例外:它把聊天模型的 logits 轉換成相同的格式,但推論仍是 llama.cpp 完成的。

**為什麼我的決策始終到不了 `act`?** 因為 spec 中沒有 `(這個問題的雜湊, 這個模型 id)` 對應的校準區塊。收集標籤(`label`,或 `--labeled file`),執行 `calibrate --write`,`status` 就會顯示 `calibrated: identity|platt|vector`。在 act 閾值上沒有經過驗證的 precision 的決策,不允許 act。這正是設計用意。

**我改了問題裡的一個字,校準就不見了。** 沒錯。雜湊變了,那些常數是針對另一個問題擬合的。`status` 會把舊標籤顯示為 `stale hash`;等你有了新措辭對應的標籤後,再執行一次 `calibrate --write`。

**我該用哪個模型?** 這取決於授權、速度與語言,shikii 不替你決定。上面的表格與 [`docs/model-comparison.md`](../model-comparison.md) 就是論據:用相同的 spec 與相同的標籤,你可以依數字來選,包括某個模型到底需不需要校準。`scripts/eval-model.sh <url> <name>` 可以為新端點重現這張表。

**支援日文嗎?** 支援,而且差異正是體現在這裡。Kev-4B 在日文提示上得分 0.913;27B 模型則是 1.000。在日文調查摘要的同案關聯任務上,每個 27B 模型都能把正例與負例完全分開。

**我的資料會去哪裡?** 只有算繪後的 state 會送往 `send_to` 中指定的端點。使用 `pii: possible`(預設)時,store 只保存輸入的雜湊,絕不保存原文。`--tainted` 會讓一切留在本機。金鑰只從 `DECISIONS_API_KEY` 或 `TYPESAFE_API_KEY` 讀取,不會從別處讀取。

**store 越來越大。** `shikii gc` 會刪除超過 30 天且從未取得標籤的紀錄。帶標籤的紀錄與標籤會永久保留,因為它們是校準的素材。除非傳入 `--apply`,否則只是 dry run。

**它會偏向第一個選項嗎?** 先量測再修正:`shikii bias <spec> --permutations 100` 會打亂選項順序,並回報翻轉次數與位置偏移。Kev-4B:100 次中 0 次翻轉,偏移 ≤ 0.024。透過 logits 讀取的一般 LLM 可能不同;這個指令就是為它們準備的。

## 文件

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): 設計筆記,框架負責什麼、拒絕負責什麼,驗證計畫以及已確認的內容。
- [`docs/operations.md`](../operations.md): 如何停止或回復每一個運作中的元件,如何解讀故障,還缺什麼。
- [`docs/model-comparison.md`](../model-comparison.md): 比較表以及重現方法。
- [`docs/guide.ja.md`](../guide.ja.md): 日文長篇指南,包含每一個指令與一次完整的實際迴圈。
- [`integrations/readout.md`](../../integrations/readout.md)、[`integrations/bonjev.md`](../../integrations/bonjev.md)、[`integrations/claude-code.md`](../../integrations/claude-code.md): 我們實際運作的三種連接。
- [`examples/`](../../examples/) 與 [`testdata/`](../../testdata/README.md): 一份沒有校準、可作為起點的 spec,以及測試中使用的合成標籤資料集。

指令:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: 雜湊過期、手寫區塊、precision@act 低於 act → exit 1
shikii drift     <spec> --model M [--window N]            # 近期標籤與校準時記錄的 Brier 和 precision 比較
shikii bias      <spec> [--rows K] [--permutations N]     # 答案是否取決於選項順序?
shikii status    [spec...] [--store DIR]                  # 盤點:紀錄、標籤、來源、每個 (model, hash) 的狀態
shikii gc        [spec...] [--older-than 30d] [--apply]   # 刪除過期且無標籤的紀錄;預設為 dry run
shikii hash      <spec>
```

## 貢獻

歡迎提交 issue 與 pull request。只有一條規矩:守護某個行為的測試,必須先展示它失敗。刻意破壞該行為,執行測試,記錄它是如何失敗的,然後修正,再記錄它通過。文件為每條安全分支都列出了這些變異;請保持這份清單真實。

## 授權

[Apache 2.0](../../LICENSE)。模型的授權另行適用:Kev-4B 與 Bonsai 系列為 Apache 2.0,OpenJev 為 CC BY-NC 4.0,託管的 Jev 是 TypeSafe 的服務。shikii 保存的是你的機率與標籤,不保存模型的任何內容。
