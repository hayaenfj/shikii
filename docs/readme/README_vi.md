[English](../../README.md) · [日本語](README_ja.md) · [简体中文](README_zh.md) · [繁體中文](README_zh-TW.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Français](README_fr.md) · [Italiano](README_it.md) · [Português (Brasil)](README_pt-BR.md) · [Русский](README_ru.md) · [Українська](README_uk.md) · [Polski](README_pl.md) · [Türkçe](README_tr.md) · **Tiếng Việt** · [Bahasa Indonesia](README_id.md)

<p align="center"><img src="../assets/gen-mark.png" alt="shikii" width="112"></p>

# shikii: biến câu "có lẽ" của AI thành câu "làm được, an toàn" mà bạn có thể dựa vào để vận hành công việc

### Một công cụ để giao cho AI quyết định có/không, cái nào, hay bao nhiêu, và giữ cho quyết định đó đáng tin trong công việc hằng ngày. Bạn kiểm tra một số câu trả lời và thỉnh thoảng xác nhận "đây vẫn là điều tôi muốn chứ?". Phần còn lại máy lo.

[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?style=flat-square)](../../go.mod)
[![Tests](https://img.shields.io/badge/tests-192%20go%20tests%20%2B%20CI-00ADD8?style=flat-square)](../../.github/workflows/test.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue?style=flat-square)](../../LICENSE)
[![Status](https://img.shields.io/badge/status-prototype-orange?style=flat-square)](../operations.md)

[Vấn đề](#vấn-đề) · [shikii làm gì](#shikii-làm-gì) · [Bạn làm gì](#bạn-làm-gì) · [Cài đặt](#cài-đặt) · [Đường vào ngắn nhất](#đường-vào-ngắn-nhất) · [Một vòng trọn vẹn](#một-vòng-trọn-vẹn) · [Điểm mới](#điểm-mới) · [Đọc thêm](#đọc-thêm)

<p align="center"><img src="../assets/shikii-hero.svg" alt="shikii: mô hình cho bạn một xác suất, shikii cho bạn biết có được hành động theo nó hay không" width="100%"></p>

## Vấn đề

Hỏi AI "yêu cầu này có cùng vụ việc với yêu cầu trước không?" và nó trả lời "có lẽ, 0,9". Với công việc thật, như vậy chưa đủ.

- Con số 0,9 đó có thật sự đúng 9 lần trong 10 không?
- Từ ngày bạn đổi sang mô hình mới, nó có âm thầm bắt đầu nói sai không?
- Nếu đổi cách hỏi một chút, độ chính xác thay đổi thế nào?
- Những quyết định nó tự đưa ra khi không ai nhìn: chúng còn đúng không?

Nếu một người phải kiểm tra từng câu trả lời, thì dùng AI cũng chẳng còn mấy ý nghĩa.

## shikii làm gì

1. **Gom các lần sửa của bạn và vạch ra đường ranh mà từ đó trở lên AI được tự hành động.** Từ những câu bạn đã trả lời ("thực ra đúng là thế này"), nó đo quan hệ giữa độ tự tin của AI và tần suất AI đúng, rồi quyết định từ mức nào trở lên có thể tin được. Chưa có đường ranh thì không gì tự hành động. Khi đã có, mỗi quyết định tự động đi kèm một biên nhận cho biết đường ranh dựa trên bao nhiêu câu trả lời đã kiểm tra và chúng đúng bao nhiêu phần
2. **Thử các cách hỏi khác và giữ lại cách tốt hơn.** Trên cùng những lần sửa đó, nó chấm điểm các câu hỏi thay thế, mô tả lựa chọn và bố cục lời nhắc, và chỉ áp dụng khi thật sự tốt hơn. Bạn chỉ xác nhận là ý nghĩa không thay đổi
3. **Cho bạn đổi mô hình mà không phải bắt đầu lại.** Các lần sửa được lưu tách khỏi mô hình, nên mô hình mới dùng lại chúng và máy tự vạch lại đường ranh. Không cần người làm gì
4. **Giữ những gì đã học trong một wiki.** "Hỏi theo cách này thì đúng" được tích lại thành các nhận định kèm con số đã đo, có thể kiểm lại trên mô hình khác. Những gì người khác đo trên mô hình của họ được thêm vào cùng một trang

## Bạn làm gì

- **Viết ra điều bạn muốn quyết định.** Trả lời vài câu hỏi là có một tệp quyết định (`shikii new`, theo kiểu hỏi đáp hoặc với `--type --question --option --input --pii`; định dạng đầy đủ ở [tài liệu tham chiếu spec](../../skills/shikii/references/spec.md))
- **Kiểm tra câu trả lời.** Trang web hiện những quyết định AI không chắc, cộng thêm 1 trong 20 quyết định nó đã tự làm, và bạn nhấn nút cho đáp án đúng thật. Các phím 1 đến 9 và Enter (giống lựa chọn của AI) giúp đi nhanh. "Đã trả lời" cho phép sửa câu trả lời trước (câu mới nhất được tính). "Chi tiết" thêm mục "hỏi AI lại", quyết định lại cùng văn bản đó với cách hỏi và đường ranh hôm nay, rồi hiện trước và sau; bản ghi chỉ được thêm vào, không bao giờ bị xóa
- **Thỉnh thoảng xác nhận "đây vẫn là điều tôi muốn chứ?".** Khi máy tìm được cách hỏi đạt điểm cao hơn, bạn xem ý nghĩa có đổi không rồi nhấn chấp nhận hoặc từ chối

Mọi việc còn lại là của máy.

## Cài đặt

```sh
curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh     # cần Go 1.27 trở lên
cd <thư mục gốc dự án> && shikii init
```

`shikii init` dựng một thư mục duy nhất, `.shikii/` (bên trong có `decisions/` cho các spec của bạn, `wiki/` cho kiến thức, `records/` và `labels/` cho bản ghi và các lần sửa của bạn; không thêm gì khác vào thư mục gốc dự án), đồng thời thiết lập các kết nối với agent trong một lần: máy chủ MCP của shikii cho Claude Code (`.mcp.json`), Cursor (`.cursor/mcp.json`), OpenCode (`opencode.json`) và Codex (`~/.codex/config.toml`), một mục trong `AGENTS.md`, và các quy tắc chi tiết (skill) trong `~/.claude/skills/shikii`. Các mục do bạn tự viết không bao giờ bị đụng đến (mục shikii đã có sẵn thì để nguyên). Chỉ thư mục skill được thay mới mỗi lần, nên đừng sửa nó bằng tay. `wiki/` nhận phần kiến thức chung (cách hỏi từng loại câu hỏi, các mẫu câu đã hiệu quả, thói quen của từng mô hình); bộ cải thiện cách hỏi (`tune --propose`) đọc nó, và các trang bạn đã sửa không bao giờ bị `init` lần sau ghi đè. Không có Go? Dùng Docker bên dưới.

Các mục cấu hình trỏ đến một mô hình chạy trên máy bạn (`shikii up`, cổng 8080). Chỉ dùng mô hình trên dịch vụ ngoài? Hãy tự xóa `--local` khỏi từng tệp trong bốn tệp. Tệp cấu hình cho những công cụ bạn không dùng cũng được tạo ra; hãy xóa hoặc thêm vào `.gitignore` (chạy `shikii init` lần hai không bao giờ đụng vào thứ đã có). Hãy commit `.shikii/decisions/`, `.shikii/wiki/` và `.shikii/labels/` (spec của bạn, kiến thức và các lần sửa: tài sản còn nguyên khi đổi mô hình). `.shikii/records/` (mọi quyết định) lớn dần, nên commit hay không là tùy bạn (`shikii gc` bỏ các bản cũ). Nếu Codex đã có `mcp_servers`, không gì được thêm và các dòng cần dán được in ra. Dự án chỉ có `CLAUDE.md` sẽ không nhận `AGENTS.md` (vì OpenCode sẽ ngừng đọc `CLAUDE.md`).

Sửa chính trang trả lời (`web/`, Svelte) cần Node 24 (`make web`). Dùng shikii thì không cần: trang đã dựng sẵn được commit và nhúng trong tệp chạy.

## Đường vào ngắn nhất

- **Docker**: không có Go, không có mô hình? `docker compose up` khởi động một mô hình trên máy (Kev-4B) và shikii, với trang trả lời tại http://127.0.0.1:8095 ([chi tiết](../../integrations/docker.md))
- **Quyết định làm sẵn**: sáu cái trong thư mục `decisions/` của repo này (loại yêu cầu, báo lỗi trùng, cùng vụ việc, …). Chép một cái vào `.shikii/decisions/` của bạn, hoặc bắt đầu từ nó với `shikii new --from <repo>/decisions/prompt-intent-v2.yaml` ([danh sách](../../decisions/README.md))
- **Bạn đã có bảng các quyết định cũ**: `shikii import past.csv --spec .shikii/decisions/<id>.yaml` biến nó thành các lần sửa, nên bạn không phải trả lời 50 câu đầu bằng tay
- **Từ chương trình của bạn**: chạy `shikii serve` và gọi `POST /v1/decide` (quyết định) và `POST /v1/label` (sửa). Nội dung là JSON và bắt buộc có `-H 'Content-Type: application/json'` (mọi điểm cuối: [hướng dẫn dài](../guide.md#or-run-one-process-for-programs-and-people))
- **Ví dụ của người khác**: [phân luồng thư yêu cầu](../examples/inquiry-route.md) (tiếng Nhật). Một người dùng lần đầu đã đi trọn vòng chỉ với README; có kèm spec và 22 đầu vào
- **Đo chính cách nghĩ của AI** (Claude Code và Codex): `shikii hook install` thêm hai hook chạy shikii trước và sau mỗi lượt. Trước: yêu cầu này rồi có làm thay đổi tệp không, và agent có nên thống nhất cách làm trước không? Sau: các thay đổi có nằm trong phạm vi bạn yêu cầu không, và nó đã làm xong điều bạn yêu cầu chưa? Đáp án đầu tiên có sẵn miễn phí từ việc cây làm việc có thay đổi hay không; phần còn lại bạn trả lời trên một trang web. Chưa có đường ranh thì không gì thay đổi. Sau đó, một yêu cầu cần lập kế hoạch sẽ thành một ghi chú gửi agent, còn một lượt trông như chưa xong hoặc vượt phạm vi sẽ thành một dòng gửi bạn, kèm số đáp án mà đường ranh dựa trên và độ chính xác do người kiểm tra. Không hook nào bao giờ chặn một công cụ ([chi tiết](../../integrations/claude-code.md), tiếng Nhật)
- **Trong CI**: `uses: hayaenfj/shikii@main` chạy `shikii check` trên mỗi pull request, nên một cách hỏi bị đổi hoặc một đường ranh bị yếu đi sẽ làm bản dựng thất bại

## Một vòng trọn vẹn

```sh
shikii up                                                             # khởi động mô hình trên máy (lần đầu sẽ tải về)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=ask  (chưa có đường ranh, nên: hỏi người)
shikii serve --local http://127.0.0.1:8080                                   # spec và kiến thức được đọc từ .shikii/
#   → trả lời tại http://127.0.0.1:8095. Khi một quyết định đủ số nó yêu cầu (eval.min_n, với cái này là 20) đường ranh tự được vạch
#     Để thử với ít câu trả lời hơn, hạ eval.min_n trong spec xuống số bạn có (đường ranh viết bằng --force sẽ bị check đánh dấu)
shikii decide .shikii/decisions/same-issue.yaml -i report1="…" -i report2="…" --local http://127.0.0.1:8080
#   → band=act
#     receipt: line platt fitted 2026-10-18 on 61 truths (48 human); human-checked act decisions 25, precision 0.960
#     (làm được, an toàn, kèm bằng chứng: đường ranh dựa trên 61 lần sửa, 48 trong đó từ người. Trong các câu trả lời của người mà đường ranh này xếp vào phía act, 25 câu, 96% là đúng)
```

Chỉ có bốn dải. `act` = cứ làm, `ask` = hỏi người, `discard` = không làm gì, `abstain` = thiếu đầu vào. Bạn không bao giờ phải đọc một xác suất thô.

## Điểm mới

<p align="center"><img src="../assets/shikii-architecture-en.svg" alt="kiến trúc shikii: ba việc con người làm, các tài sản (câu hỏi, đáp án, biên nhận), vòng lặp máy nuôi lớn, wiki kiểm chứng được, mô hình dùng xong bỏ" width="100%"></p>

- **Kiến thức tích lại trong câu hỏi và các lần sửa, không phải trong mô hình.** Nên mô hình là thứ dùng xong bỏ, và đổi nó không cần người làm gì. Đã đo: khi máy chọn cách hỏi và bố cục rồi thêm các ví dụ tương tự, một mô hình nền thông thường đạt độ chính xác ngang với tinh chỉnh (LoRA) ([con số](../bench.md))
- **Quyết định tự động đi kèm bằng chứng.** Không phải "tự tin 95%" mà là "độ chính xác 0,96 trên 48 câu trả lời đã được người kiểm tra"
- **Wiki không phải để đọc, mà để đo.** "Hỏi theo cách này thì đúng" tích lại thành nhận định kèm con số và được kiểm lại trên mô hình mới. Chỉ có người mới quyết định một nhận định là đã xác nhận

## Đọc thêm

- [Hướng dẫn dài](../guide.md): mọi lệnh, phần bắt đầu nhanh đầy đủ, HTTP API, những gì bạn có thể kết nối, kết quả đã đo và câu hỏi thường gặp · [tài liệu tham chiếu spec](../../skills/shikii/references/spec.md)
- [Ghi chú thiết kế](../decision-ops-framework.md): khung này nhận trách nhiệm gì và từ chối nhận gì, và những gì đã được kiểm chứng · [Kiến trúc](../architecture.md) (tiếng Nhật)
- [Quyết định làm sẵn](../../decisions/README.md) · [wiki về cách hỏi](../../wiki/index.md) · [đo hiệu năng](../bench.md) · [so sánh mô hình](../model-comparison.md) · [nhật ký vận hành](../operations.md)
- [Kết nối](../../integrations/mcp.md): MCP, bộ chuyển đổi biến một mô hình chat thường thành mô hình quyết định, Docker, Claude Code
- [Hướng dẫn tiếng Nhật](../guide.ja.md) với một vòng trọn vẹn từ thực tế

## Đóng góp

Hoan nghênh issue và pull request. Một bài kiểm thử bảo vệ một hành vi phải được cho thấy thất bại trước: cố ý phá hành vi đó, chạy kiểm thử, ghi lại nó thất bại thế nào, rồi sửa và ghi lại rằng nó đạt. Mọi con số trong tài liệu đều đến từ một script. Chỉ có người mới đổi trạng thái của một nhận định trong wiki (`confirmed` / `tentative` / `rejected`), kèm lần chạy làm bằng chứng. Không thêm tùy chọn mới: bố cục (`decisions/` `wiki/` `.shikii/` ở thư mục gốc dự án) và các quy tắc cố định làm việc đó.

## Giấy phép

[Apache 2.0](../../LICENSE). Giấy phép của mô hình là chuyện riêng: Kev-4B và họ Bonsai theo Apache 2.0, OpenJev theo CC BY-NC 4.0, Jev trên dịch vụ là của TypeSafe. shikii lưu xác suất và nhãn của bạn, không lưu gì của mô hình.
