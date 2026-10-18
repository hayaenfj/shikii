[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · **Tiếng Việt** · [Bahasa Indonesia](README_id.md)

# shikii: Vận hành mô hình quyết định

### Một vòng đời cho mọi mô hình quyết định: define → decide → record → label → calibrate → check → drift.

**Mô hình đưa ra một xác suất. shikii cho bạn biết liệu bạn có được phép hành động dựa trên xác suất đó hay không.**

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-185%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

**[Ghi chú thiết kế](../decision-ops-framework.md) · [Vận hành](../operations.md) · [So sánh mô hình](../model-comparison.md) · [Hướng dẫn chi tiết (tiếng Nhật)](../guide.ja.md)**

[Bắt đầu nhanh](#bắt-đầu-nhanh) · [shikii hoạt động như thế nào](#shikii-hoạt-động-như-thế-nào) · [Có thể kết nối với gì](#có-thể-kết-nối-với-gì) · [Kết quả](#kết-quả) · [FAQ](#faq) · [Tài liệu](#tài-liệu)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

`scripts/demo.sh`

> [!IMPORTANT]
> shikii không bao giờ chạy mô hình và không bao giờ đọc keychain. Nó chỉ trao đổi với một endpoint mô hình quyết định mà bạn đã có, lưu lại các bản ghi, và từ chối **act** cho đến khi các xác suất của *câu hỏi này trên mô hình này* được kiểm chứng bằng nhãn. Các khối hiệu chuẩn được sinh tự động, không bao giờ viết tay.

---

Các mô hình quyết định (TypeSafe Jev, llama.cpp Kev-4B và OpenJev, OpenRouter Decisions, …) trả lời các câu hỏi có kiểu, `choice`, `score` và `noul` (có/không), bằng xác suất và không sinh ra văn bản. Nhà cung cấp nào cũng kèm cùng một lời khuyến cáo: *các xác suất chưa được hiệu chuẩn cho dữ liệu của bạn, và ngưỡng không dùng chung được giữa các mô hình hay các câu hỏi khác nhau.*

shikii nắm phần mà không ai cung cấp: **vòng đời** của một quyết định. Một file spec chứa câu hỏi, các lựa chọn, mẫu state và các ngưỡng. `decide` hỏi mô hình và trả về một band (`act` / `ask` / `discard` / `abstain`), không bao giờ trả về con số thô. `label` ghi lại điều gì là đúng. `calibrate` khớp mô hình và kiểm chứng chéo, rồi ghi kết quả ngược vào spec dưới một khóa gồm hash của câu hỏi và tên mô hình. `check` làm CI thất bại khi cách diễn đạt hoặc mô hình bị lệch. `gc` giữ cho store gọn nhẹ.

Nó còn đi kèm `shikii-readout`, một adapter 500 dòng biến **bất kỳ GGUF chat nào trên llama.cpp nguyên bản** thành mô hình quyết định bằng cách đọc logit của các chữ cái đại diện cho lựa chọn. Theo các phép đo của chúng tôi, một mô hình ternary 27B thông thường được đọc theo cách này đạt độ chính xác ngang các mô hình quyết định đã huấn luyện, với tốc độ 0.6 giây một câu hỏi trên laptop.

> Cái tên đến từ tiếng Nhật: *shikii* (閾) nghĩa là ngưỡng, ranh giới biến một xác suất thành hành động. CLI có tên là `shikii`.

## Bắt đầu nhanh

Go 1.27 trở lên. Lần chạy đầu tiên không cần gì khác.

### Chạy trọn vòng mà không cần key hay GPU

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
scripts/demo.sh                 # mô hình giả: decide → label → calibrate → check → drift → status → gc
scripts/demo.sh --overconfident # một mô hình giả có xác suất cần hiệu chuẩn; xem calibrate chọn platt
```

### Trỏ tới một mô hình thật

```sh
make build                                        # bin/shikii, bin/shikii-readout, bin/fake-systemone

# a) mô hình quyết định cục bộ trên llama.cpp (tải Kev-4B, khoảng 2.5 GB, ở lần chạy đầu)
make server                                       # llama-server trên :8080
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8080 -i message='fix the typo in README'

# b) TypeSafe Jev dạng hosted. Key chỉ lấy từ biến môi trường
export TYPESAFE_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml -i message='fix the typo in README'

# c) OpenRouter Decisions API
export DECISIONS_API_KEY=...
bin/shikii decide examples/prompt-intent.yaml --hosted https://openrouter.ai/api/alpha/decisions -i message='…'
```

Câu trả lời gồm một band, một trace id và các xác suất. Cho đến khi spec mang một bản hiệu chuẩn cho mô hình này, band tối đa chỉ là `ask`.

### Đóng vòng lặp

```sh
bin/shikii label <trace_id> change_request          # or:  label --last --spec-id prompt-intent change_request
bin/shikii calibrate examples/prompt-intent.yaml --model <model name from the response> --write
bin/shikii check examples/prompt-intent.yaml        # exit 1 khi cách diễn đạt thay đổi hoặc precision@act tụt xuống dưới act
```

### Dùng bất kỳ GGUF nào làm mô hình quyết định

```sh
make server-gguf GGUF=/path/to/model.gguf ALIAS=my-model   # llama-server nguyên bản với flash attention và KV 8-bit, :8086
make readout                                               # shikii-readout đặt phía trước nó, :8090
bin/shikii decide examples/prompt-intent.yaml --local http://127.0.0.1:8090 -i message='…'
```

### Cài đặt và cập nhật

```sh
make install          # sao chép bin/shikii vào ~/.local/bin/shikii
git pull && make install
```

## shikii hoạt động như thế nào

Mỗi quyết định là một file YAML. Gồm câu hỏi, các lựa chọn, mẫu state, nơi state được phép gửi tới, các ngưỡng, và các khối hiệu chuẩn do `calibrate --write` sinh ra.

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
  required: [message]            # thiếu đầu vào → abstain; mô hình không bao giờ được gọi
policy:
  send_to: [hosted, local]       # phiên bị tainted tự động bỏ qua hosted
  pii: possible                  # bản ghi giữ hash của đầu vào, không bao giờ giữ văn bản
thresholds:
  act: 0.75                      # ≥ act → tự động act, chỉ khi đã hiệu chuẩn
  ask: 0.60                      # ≥ ask → hỏi con người; thấp hơn → discard
```

<p align="center"><img src="../assets/shikii-lifecycle.svg" alt="define, decide, record, label, calibrate, check, drift" width="100%"></p>

**Khóa hiệu chuẩn là `(hash của câu hỏi + template + các lựa chọn, model id do endpoint trả về)`.** Sửa một từ trong câu hỏi, hoặc để `jev-latest` chuyển sang phiên bản mới, và bản hiệu chuẩn đơn giản là biến mất: band tụt xuống `ask` thay vì âm thầm hành động dựa trên các hằng số đã cũ. Bố cục prompt cũng nằm trong khóa: `shikii-readout` báo bố cục của nó ngay trong tên mô hình (`…+state-last`), vì chỉ việc chuyển state vào bên trong prompt đã làm độ chính xác của một mô hình 27B thay đổi 5 điểm với cùng trọng số.

`calibrate` so sánh ba phương pháp bằng kiểm chứng chéo 5-fold và ghi phương pháp thắng. `identity` nghĩa là các xác suất thô đã đạt precision@act và mô hình chỉ đơn giản được đánh dấu là đã kiểm chứng. `platt` (σ((Δ−bias)/T)) dành cho câu hỏi có/không. `vector` (softmax((log p + b)/T)) dành cho choice và score, và sửa các thói quen theo từng lựa chọn, chẳng hạn "lúng túng là lùi về *other*". Nhãn một phần (`!option`, "không phải *cái đó*") được tính vào precision nhưng không dùng để khớp mô hình. Nhãn lấy từ file được lưu lại bền vững, nên về sau `check` và `drift` chạy được offline.

Mọi nhánh liên quan đến an toàn (định tuyến taint, không act khi chưa hiệu chuẩn, vô hiệu hóa khi hash cũ, kiểm tra nhãn, phát hiện đảo ngược, chọn phương pháp theo CV, `gc` chạy thử, …) đều có một mutation được ghi lại đã làm test thất bại trước khi bản sửa được chấp nhận. Có 63 test Go trên 8 package; demo chạy trong CI.

## Có thể kết nối với gì

shikii nói chuyện bằng dạng request của System One (`POST /v1/systemone`), nên bất cứ thứ gì phục vụ dạng đó đều dùng được. Adapter lo phần còn lại.

| Đích | shikii nhận được gì | Yêu cầu và hướng dẫn |
|---|---|---|
| **TypeSafe Jev** (hosted) | `choice` / `score` / `noul` kèm xác suất, model id như `jev-1.13.0` | `TYPESAFE_API_KEY` trong môi trường. `--hosted` mặc định |
| **Mô hình quyết định llama.cpp** (Kev-4B, OpenJev, Clef, …) | Cùng dạng, chạy cục bộ, model id lấy từ `/v1/models` | `llama-server -hf ggml-org/Kev-4B-GGUF:Q4_K_M` (`make server`). llama.cpp 0.6.0 trở lên |
| **OpenRouter Decisions API** | Cùng dạng qua OpenRouter | `DECISIONS_API_KEY`, `--hosted https://openrouter.ai/api/alpha/decisions` |
| **GGUF chat bất kỳ** qua `shikii-readout` | Đọc logit của chữ cái theo dạng System One, tự nó không có hiệu chuẩn | `llama-server` nguyên bản 0.7.0 phía sau. [`integrations/readout.md`](../../integrations/readout.md) |
| **PrismML Bonsai** qua [BonJev](https://github.com/ChezRD/bonjev) | Tương tự, với prompt riêng và hiệu chuẩn tích hợp sẵn của BonJev | Bản build Rust và Prism fork. [`integrations/bonjev.md`](../../integrations/bonjev.md) |
| **Mô hình giả** (`bin/fake-systemone`) | Xác suất tất định cho demo và test | Không cần gì. `make fake` |
| **Claude Code hook** (claude-jev) | Mỗi prompt được phân loại, mỗi lượt được gán nhãn từ working tree | [`integrations/claude-code.md`](../../integrations/claude-code.md) |

Taint là một đầu vào hạng nhất: truyền `--tainted` (hoặc để hook của bạn làm việc đó) trong một phiên đã chạm vào dữ liệu production thì request không bao giờ rời khỏi máy. Nếu không cấu hình endpoint cục bộ nào, quyết định sẽ abstain.

## Kết quả

Cùng spec, cùng nhãn, bốn tập đánh giá (ý định của prompt bằng tiếng Anh và tiếng Nhật, 62 yêu cầu; liên kết cùng vụ việc trên 32 cặp tiếng Nhật). Chi tiết và bảng đầy đủ nằm trong [`docs/model-comparison.md`](../model-comparison.md).

| Mô hình | Giấy phép | top-1 (tất cả / ja) | Brier có/không (thô) | Hiệu chuẩn được chọn | Giây mỗi câu hỏi |
|---|---|---|---|---|---|
| Kev-4B (mô hình quyết định đã huấn luyện) | Apache 2.0 | 0.919 / 0.913 | 0.046 | identity | 0.25 |
| OpenJev 27B (đã huấn luyện) | CC BY-NC 4.0 | **0.984 / 1.000** | 0.020 | vector | 3.8 |
| Bonsai 2 27B qua BonJev | Apache 2.0 | 0.968 / 1.000 | 0.026 → **0.013 (platt)** | vector / platt | 2.5 |
| **Ternary-Bonsai-27B qua `shikii-readout`, llama.cpp nguyên bản** | Apache 2.0 | 0.968 / 1.000 | 0.015 | identity | **0.58** |

**Cách diễn đạt thắng hiệu chuẩn.** Với ý định của prompt, 19 trong 21 lỗi rơi vào *other*; viết lại các lựa chọn đã đưa top-1 từ 0.68 lên 0.90 trước khi có bất kỳ bước hiệu chuẩn nào. Hiệu chuẩn dành cho trường hợp "thứ tự đúng, nhưng độ tự tin chưa đủ". Ma trận nhầm lẫn mà `calibrate` in ra cho bạn biết bạn đang gặp trường hợp nào.

**Mô hình đã được hiệu chuẩn sẵn không cần Platt.** Câu hỏi có/không của Kev-4B có Brier 0.046 khi còn thô; Platt làm nó tệ hơn khi kiểm chứng chéo, nên `identity` được ghi. Lần đầu tiên `calibrate` chọn `platt` là với một LLM thông thường được đọc qua logit. Việc có cần hiệu chuẩn hay không là thuộc tính của *mô hình cộng cách đọc*, không phải của riêng mô hình.

**Một GGUF thông thường đứng ngang hàng với một mô hình quyết định đã huấn luyện.** Một mô hình chat ternary 27B, llama.cpp mainline 0.7.0, một adapter, không fork, không huấn luyện, không Rust: top-1 bằng mô hình đã huấn luyện tốt nhất dưới giấy phép Apache, tiếng Nhật 1.000, 0.58 giây một câu hỏi với flash attention và một prefix đã được làm nóng. Chuyển state từ trước các lựa chọn xuống ngay trước câu trả lời đã đưa nó từ 0.919 lên 0.968 với cùng trọng số, đó là lý do bố cục nằm trong khóa hiệu chuẩn.

## FAQ

**shikii có chạy mô hình không?** Không. Nó gửi các request System One tới một endpoint do bạn tự chạy hoặc thuê, và giữ mọi thứ xảy ra sau câu trả lời. `shikii-readout` là ngoại lệ duy nhất về tinh thần: nó chuyển logit của một mô hình chat sang cùng dạng đó, nhưng phần suy luận vẫn là của llama.cpp.

**Vì sao quyết định của tôi không bao giờ tới `act`?** Vì spec không có khối hiệu chuẩn cho `(hash của câu hỏi này, model id này)`. Hãy thu thập nhãn (`label`, hoặc `--labeled file`), chạy `calibrate --write`, rồi `status` sẽ hiện `calibrated: identity|platt|vector`. Một quyết định chưa có precision được kiểm chứng ở ngưỡng act thì không được phép act. Đó chính là mục đích.

**Tôi đổi một từ trong câu hỏi và bản hiệu chuẩn biến mất.** Đúng như thiết kế. Hash đã đổi, các hằng số được khớp cho một câu hỏi khác. `status` hiển thị các nhãn cũ là `stale hash`; hãy chạy lại `calibrate --write` khi bạn đã có nhãn cho cách diễn đạt mới.

**Tôi nên dùng mô hình nào?** Tùy vào giấy phép, tốc độ và ngôn ngữ, và shikii không quyết định thay bạn. Bảng ở trên và [`docs/model-comparison.md`](../model-comparison.md) chính là lập luận: với cùng một spec và cùng các nhãn, bạn có thể chọn bằng con số, kể cả việc một mô hình có cần hiệu chuẩn hay không. `scripts/eval-model.sh <url> <name>` tái tạo bảng cho một endpoint mới.

**Nó có chạy được với tiếng Nhật không?** Có, và đó là nơi sự khác biệt lộ rõ. Kev-4B đạt 0.913 trên các prompt tiếng Nhật; các mô hình 27B đạt 1.000. Liên kết cùng vụ việc trên các bản tóm tắt điều tra bằng tiếng Nhật tách hoàn toàn mẫu dương khỏi mẫu âm trên mọi mô hình 27B.

**Dữ liệu của tôi đi đâu?** Chỉ state đã được render mới được gửi tới endpoint nêu trong `send_to`. Với `pii: possible` (mặc định), store giữ hash của đầu vào, không bao giờ giữ văn bản. `--tainted` giữ mọi thứ ở máy cục bộ. Key chỉ được đọc từ `DECISIONS_API_KEY` hoặc `TYPESAFE_API_KEY`, không ở đâu khác.

**Store cứ phình to mãi.** `shikii gc` xóa các bản ghi cũ hơn 30 ngày chưa bao giờ được gán nhãn. Các bản ghi đã có nhãn và bản thân các nhãn được giữ mãi mãi, vì chúng là nguyên liệu để hiệu chuẩn. Lệnh này chỉ chạy thử trừ khi bạn truyền `--apply`.

**Nó có thiên về lựa chọn đầu tiên không?** Hãy đo trước khi sửa: `shikii bias <spec> --permutations 100` xáo trộn các lựa chọn và báo cáo số lần đảo kết quả cùng độ lệch vị trí. Kev-4B: 0 lần đảo trong 100, độ lệch ≤ 0.024. Các LLM thông thường được đọc qua logit có thể khác; lệnh này sinh ra cho chúng.

## Tài liệu

- [`docs/decision-ops-framework.md`](../decision-ops-framework.md): ghi chú thiết kế, framework nắm giữ gì và từ chối nắm giữ gì, kế hoạch kiểm chứng và những gì đã được xác nhận.
- [`docs/operations.md`](../operations.md): cách dừng hoặc rollback từng bộ phận đang chạy, cách đọc một lỗi, những gì còn thiếu.
- [`docs/model-comparison.md`](../model-comparison.md): bảng so sánh và cách tái tạo nó.
- [`docs/guide.ja.md`](../guide.ja.md): hướng dẫn chi tiết bằng tiếng Nhật, có mọi lệnh và một vòng chạy thực tế đầy đủ.
- [`integrations/readout.md`](../../integrations/readout.md), [`integrations/bonjev.md`](../../integrations/bonjev.md), [`integrations/claude-code.md`](../../integrations/claude-code.md): ba kết nối chúng tôi đang chạy.
- [`examples/`](../../examples/) và [`testdata/`](../../testdata/README.md): một spec chưa có hiệu chuẩn để bắt đầu, và các tập dữ liệu có nhãn tổng hợp dùng trong test.

Các lệnh:

```
shikii decide    <spec> -i k=v ... [--hosted URL] [--local URL] [--tainted] [--store DIR] [--json]
shikii label     <trace_id> <truth> | --last --spec-id ID <truth>   [--weight W] [--source ask|outcome|manual]
shikii calibrate <spec> [--labeled file] [--model M] [--store DIR] [--write] [--force]
shikii check     <spec>...  [--store DIR]                 # CI: stale hash, khối viết tay, precision@act dưới act → exit 1
shikii drift     <spec> --model M [--window N]            # các nhãn gần đây so với Brier và precision đã ghi lúc hiệu chuẩn
shikii bias      <spec> [--rows K] [--permutations N]     # câu trả lời có phụ thuộc vào thứ tự các lựa chọn không?
shikii status    [spec...] [--store DIR]                  # kiểm kê: bản ghi, nhãn, nguồn, trạng thái theo từng (model, hash)
shikii gc        [spec...] [--older-than 30d] [--apply]   # xóa các bản ghi cũ chưa có nhãn; mặc định là chạy thử
shikii hash      <spec>
```

## Đóng góp

Chào đón issue và pull request. Chỉ có một quy tắc: một test bảo vệ một hành vi phải được chứng minh là thất bại trước. Hãy cố ý phá hành vi đó, chạy test, ghi lại nó thất bại như thế nào, rồi sửa và ghi lại rằng nó đã qua. Tài liệu liệt kê các mutation ấy cho mọi nhánh an toàn; hãy giữ danh sách này trung thực.

## Giấy phép

[Apache 2.0](../../LICENSE). Giấy phép của các mô hình là riêng biệt: Kev-4B và họ Bonsai là Apache 2.0, OpenJev là CC BY-NC 4.0, Jev hosted là dịch vụ của TypeSafe. shikii lưu các xác suất và nhãn của bạn, không lưu bất cứ thứ gì thuộc về các mô hình.
