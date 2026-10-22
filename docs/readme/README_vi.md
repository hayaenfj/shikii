[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · **Tiếng Việt** · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: biến câu "có lẽ" của AI thành câu "làm được, an toàn" mà bạn có thể dựa vào để vận hành công việc

### Một công cụ để AI quyết định "có hay không", "cái nào" và "bao nhiêu", rồi lớn lên từ những lần sửa của bạn. Việc của bạn chỉ là trả lời, trên một trang, điều gì thật sự đã đúng.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-311%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0%20OR%20MIT-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../decision-ops-framework.md)

[Vấn đề](#vấn-đề) · [Cách dùng 1: đo chính cách nghĩ của AI](#cách-dùng-1-đo-chính-cách-nghĩ-của-ai) · [Cách dùng 2: giao một quyết định trong công việc](#cách-dùng-2-giao-một-quyết-định-trong-công-việc) · [Bạn làm gì trên trang](#bạn-làm-gì-trên-trang) · [Cài đặt](#cài-đặt) · [Vì sao bạn có thể đổi AI](#vì-sao-bạn-có-thể-đổi-ai) · [Đọc thêm](#đọc-thêm)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii" width="100%"></p>

## Vấn đề

Hỏi AI "yêu cầu này có cùng vụ việc với yêu cầu trước không?" và nó trả lời "có lẽ, 0,9". Với công việc thật, như vậy chưa đủ. Con số 0,9 đó có thật sự đúng 9 lần trong 10 không? Từ ngày bạn đổi sang AI mới hơn, nó có âm thầm trở thành nói sai không? Những quyết định nó tự đưa ra khi không ai nhìn còn đúng không? Nếu một người phải kiểm tra mỗi lần, thì dùng AI cũng chẳng còn mấy ý nghĩa.

shikii vạch một **đường ranh** từ những lần sửa của bạn: từ mức này trở lên, AI được tự hành động. Chưa có đường ranh thì không gì được tin. Khi đã có, mỗi quyết định tự động đi kèm căn cứ của nó: đường ranh dựa trên bao nhiêu lần sửa và đúng bao nhiêu phần. Đường ranh nằm cùng câu hỏi và các lần sửa, không nằm trong AI, nên đổi AI không tốn công người.

Có hai cách dùng: đo cách nghĩ của AI đang viết mã cho bạn (nhẹ nhất), và giao cho nó một quyết định trong công việc của bạn.

## Cách dùng 1: đo chính cách nghĩ của AI

Dành cho người dùng Claude Code hoặc Codex. Agent viết mã đưa ra các phán đoán khi nhận yêu cầu và khi làm xong; shikii đo chúng trong im lặng.

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
```

Chỉ vậy thôi. Lệnh này cài tệp chạy, thêm hai hook vào Claude Code và Codex, đặt các quy tắc của agent (một skill) vào chỗ, và khởi động một AI nhỏ trên máy bạn (Kev-4B) chạy nền (nếu thiếu llama.cpp, nó in ra một dòng để bạn cài). Mở một terminal mới và tiếp tục làm việc với agent như thường (trong Codex, chạy `/hooks` một lần để tin cậy các hook của shikii). Bốn điều được đo.

| Khi nào | Câu hỏi | Đáp án thật đến từ đâu |
|---|---|---|
| Một yêu cầu đến | Yêu cầu này rồi có làm thay đổi tệp không? | Tự động: cây làm việc có thay đổi trước yêu cầu kế tiếp không |
| Một yêu cầu đến | Agent có nên thống nhất cách làm với bạn trước khi đụng vào gì không? | Bạn, trên trang |
| Lượt kết thúc | Các thay đổi có nằm trong phạm vi bạn yêu cầu không? | Bạn, trên trang |
| Lượt kết thúc | Nó đã làm xong điều bạn yêu cầu chưa? | Bạn, trên trang |

Cuối mỗi lượt hiện ra một dòng, `shikii: 3 quyết định của phiên này đang chờ bạn trả lời: http://127.0.0.1:8096/`, và bạn mở nó khi rảnh một phút (hook cũng khởi động trang ở chế độ nền); mỗi câu trả lời chỉ mất vài giây. Sau 50 câu trả lời, đường ranh được vạch và cách hành xử của agent thay đổi: một yêu cầu cần lập kế hoạch sẽ thành một ghi chú gửi agent, còn một lượt trông như chưa xong hoặc vượt phạm vi sẽ thành một dòng gửi bạn, mỗi cái kèm căn cứ như "đường ranh trên 61 đáp án, độ chính xác do người kiểm tra 0,96". Trước đó, các hook chỉ ghi lại. Không hook nào bao giờ chặn một công cụ.

Mọi thứ nằm ở một chỗ, `~/.shikii/agent`, với một đường ranh cho mỗi câu hỏi xuyên suốt mọi dự án của bạn (các câu hỏi là về cách nghĩ của agent, nên đáp án thật giống nhau ở mọi kho mã). Mỗi bản ghi được đánh dấu kho mã nó đến từ đâu, và trang có thể lọc theo đó. Chi tiết: [hook](../../integrations/claude-code.md) (tiếng Nhật).

## Cách dùng 2: giao một quyết định trong công việc

Dành cho những quyết định bạn lặp lại trong công việc, như "yêu cầu này có cùng vụ việc không?" hay "tin nhắn này có phải là yêu cầu thay đổi không?": để AI quyết định và lớn lên từ những lần sửa của bạn. Ở đây cũng không có lệnh nào phải gõ. Bạn nói với agent.

> Tôi muốn shikii quyết định xem một yêu cầu có cùng vụ việc với yêu cầu trước không, và lớn lên từ những lần sửa của tôi

Agent làm theo quy tắc của nó (skill): tạo `.shikii/` trong dự án (`shikii init`), chốt câu hỏi, các lựa chọn và đầu vào rồi viết tệp quyết định (`shikii new`; nó cho bạn xem câu hỏi và các lựa chọn một lần, vì đó là ý định của bạn), gọi quyết định (`shikii decide`), khởi động trang trả lời (http://127.0.0.1:8095) và báo cho bạn. Chưa có đường ranh thì không quyết định nào là `act` (cứ làm). Khi bạn trả lời trên trang, đường ranh tự được vạch khi đạt số lượng riêng của quyết định đó (20 trong ví dụ này), và từ đó trở đi:

```sh
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (làm được, an toàn. Đường ranh dựa trên 61 đáp án, 48 trong đó do người trả lời; trong các câu do người trả lời mà đường ranh này cho phép hành động, 25 câu, 96% là đúng)
```

Chỉ có bốn giá trị `band`: `act` = cứ làm, `ask` = hỏi một người, `discard` = không làm gì, `abstain` = thiếu đầu vào. Bạn không bao giờ hành động dựa trên xác suất thô.

- **Bằng tay**: `shikii init` → `shikii new` → `shikii decide … -i key=value` → `shikii serve`. Tất cả có trong [hướng dẫn](../guide.md)
- **Từ một agent**: `shikii init` viết sẵn thiết lập MCP, nên Claude Code, Cursor, OpenCode và Codex có thể gọi trực tiếp các công cụ `shikii_decide` và `shikii_label` ([MCP](../../integrations/mcp.md))
- **Từ chương trình của bạn**: chạy `shikii serve` và gọi `POST /v1/decide` (quyết định) và `POST /v1/label` (sửa). Nội dung là JSON và bắt buộc có `-H 'Content-Type: application/json'` ([HTTP API](../guide.md#or-run-one-process-for-programs-and-people))
- **Quyết định làm sẵn**: sáu cái trong thư mục `decisions/` của repo này (loại yêu cầu, báo lỗi trùng, cùng vụ việc, …). Chép một cái với `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` rồi viết lại lời ([danh sách](../../decisions/README.md))
- **Bạn đã có bảng các quyết định cũ**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` biến nó thành các lần sửa, nên bạn không phải trả lời 50 câu đầu bằng tay
- **Docker**: chưa có Go và chưa có mô hình? `docker compose up` khởi động một AI trên máy và shikii, với trang tại http://127.0.0.1:8095 ([chi tiết](../../integrations/docker.md))
- **Ví dụ của người khác**: [phân luồng thư yêu cầu](../examples/inquiry-route.md) (tiếng Nhật). Một người dùng lần đầu đã đi trọn vòng chỉ với README, có kèm tệp quyết định và 22 đầu vào

## Bạn làm gì trên trang

Trên trang trả lời (`shikii serve`), một người làm ba việc. Mọi việc còn lại là của máy.

1. **Trả lời.** Trang hiện những quyết định AI không chắc, cộng thêm 1 trong 20 quyết định nó đã tự làm, và bạn nhấn nút cho điều thật sự đã đúng. Các phím 1 đến 9 và Enter (giống lựa chọn của AI) giúp đi nhanh. "Đã trả lời" cho phép sửa câu trả lời trước (câu mới nhất được tính)
2. **Viết một cách hỏi khác.** Nếu bạn nghĩ "hỏi theo cách này sẽ tốt hơn", hãy gõ nó vào ô "Cách hỏi" bằng lời của bạn. Máy chấm điểm nó so với cách hỏi hiện tại trên cùng những lần sửa (với các quyết định của chính agent, tự động mỗi 50 câu trả lời mới) và chỉ nói "có thể áp dụng" khi nó thật sự tốt hơn. Bạn cũng có thể để một AI đề xuất cách hỏi. Bạn không bao giờ phải mở tệp
3. **Nhấn chấp nhận hoặc từ chối.** Bạn xem ý nghĩa có đổi không rồi nhấn. Máy kiểm tra con số; bạn kiểm tra ý nghĩa

"Chi tiết" thêm trạng thái của đường ranh theo từng mô hình, mục "hỏi AI lại" (quyết định lại cùng văn bản đó với cách hỏi và đường ranh hôm nay, rồi xem trước và sau), và bảng kiến thức (các nhận định dạng "hỏi theo cách này thì đúng", kèm bằng chứng). Bản ghi chỉ được thêm vào, không bao giờ bị xóa; quyết định mới thay chỗ quyết định cũ.

## Cài đặt

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh   # Go 1.27 trở lên; nếu không có thì dùng Docker (ở trên)
```

install.sh làm gì: cài các tệp chạy (`shikii` và `shikii-readout`), thêm hai hook vào Claude Code và Codex, đặt các quy tắc của agent (skill) vào `~/.claude/skills/shikii`, và nếu có llama.cpp, khởi động AI trên máy (Kev-4B tại :8080) chạy nền. Muốn dùng Jev trên dịch vụ hoặc OpenRouter thay cho nó, hãy đặt khóa vào một biến môi trường ([kết nối](../guide.md#what-you-can-connect)). `shikii doctor` cho bạn biết cái gì đang chạy và cái gì còn thiếu.

Thiết lập cho từng dự án (`shikii init`) là việc của agent (bạn cũng có thể tự chạy). Nó tạo một thư mục duy nhất, `.shikii/` (bên trong: `decisions/` = các tệp quyết định, `wiki/` = kiến thức, `records/` và `labels/` = bản ghi và các lần sửa). Không gì khác xuất hiện ở thư mục gốc dự án. `decisions/`, `wiki/` và `labels/` là tài sản còn nguyên khi đổi AI, nên hãy commit chúng; `records/` lớn dần, nên commit hay không là tùy bạn (`shikii gc` bỏ các bản cũ). Nó cũng thêm một mục shikii vào thiết lập của các agent (Claude Code `.mcp.json`, Cursor `.cursor/mcp.json`, OpenCode `opencode.json`, Codex `~/.codex/config.toml`) mà không đụng đến các mục bạn tự viết. Chạy lại lần nữa không thay đổi gì đã có.

Đầu ra của lệnh và các thông báo của hook bằng tiếng Anh (nếu ngôn ngữ trong thiết lập của Claude Code là tiếng Nhật, thông báo của hook hiện thêm tiếng Nhật bên cạnh tiếng Anh). Trang có tiếng Nhật. Các điểm tinh hơn, như chỉ dùng AI trên dịch vụ, `AGENTS.md` so với `CLAUDE.md`, và Node để sửa chính trang đó, nằm trong [hướng dẫn](../guide.md).

## Vì sao bạn có thể đổi AI

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="kiến trúc shikii: ba việc con người làm, các tài sản (câu hỏi, đáp án, biên nhận), vòng lặp máy nuôi lớn, wiki kiểm chứng được, mô hình dùng xong bỏ" width="100%"></p>

- **Kiến thức tích lại cùng câu hỏi và các lần sửa, không phải trong AI.** Các lần sửa còn nguyên khi đổi AI, nên AI mới dùng lại chúng y như cũ và đường ranh được vạch lại. Trong các phép đo của chúng tôi, để máy chọn cách hỏi và bố cục rồi thêm các ví dụ tương tự đạt độ chính xác ngang với tinh chỉnh trọng số của mô hình (LoRA) ([con số](../bench.md))
- **Quyết định tự động đi kèm biên nhận.** Không phải "tự tin 95%" mà là "độ chính xác 0,96 trên 48 trường hợp đã được người kiểm tra"
- **Wiki không phải để đọc, mà để đo.** "Hỏi theo cách này thì đúng" tích lại thành các nhận định kèm con số, và có thể kiểm lại trên AI mới. Chỉ có người mới quyết định một nhận định là đã xác nhận

## Đọc thêm

- [Hướng dẫn](../guide.md): mọi lệnh, HTTP API, câu hỏi thường gặp · [tài liệu tham chiếu spec](../../skills/shikii/references/spec.md)
- [Hook](../../integrations/claude-code.md) (tiếng Nhật): cách đo chính cách nghĩ của agent, bảng dải → cách hành xử, cấu trúc bên trong
- [Kiến trúc](../architecture.md) (tiếng Nhật): toàn cảnh phía sau sơ đồ ở trên
- [Quyết định làm sẵn](../../decisions/README.md) · [wiki về cách hỏi](../../wiki/index.md) · [con số đã đo](../bench.md)
- [Kết nối](../../integrations/mcp.md): MCP, bộ chuyển đổi cho các mô hình chat thường, Docker

**Dành cho người đóng góp**: cố ý phá một bài kiểm thử và xem nó thất bại trước khi sửa. Mọi con số đều đến từ một script. Chỉ có người mới xác nhận một nhận định trong wiki. Không thêm thiết lập mới; thay vào đó là chỗ đặt cố định và quy tắc cố định.

## Giấy phép

[Apache-2.0 OR MIT](../../LICENSE). Giấy phép của mô hình là chuyện riêng (Kev-4B và họ Bonsai theo Apache 2.0, OpenJev theo CC BY-NC 4.0, Jev trên dịch vụ là dịch vụ của TypeSafe).
