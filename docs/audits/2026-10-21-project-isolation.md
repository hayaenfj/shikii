# プロジェクト分離の監査（2026-10-21）

対象は `caee172..01d2634`。独立した2つの監査役が、プロジェクト別の保存先、共通画面、CLI/MCP、採点・較正を確認した。**指摘は5件（medium 2件、low 3件）、全て未修正。** 重複は1件に統合した。今回は監査と安全な再現だけを行い、修正は加えていない。

| ID | 深刻度 | 主分類 | 問題 |
|---|---|---|---|
| SHK-20261021-001 | medium | data-protection | 別プロジェクトの同時較正で報告と出力先が混ざる |
| SHK-20261021-002 | medium | input-validation | サブディレクトリで init・new・tune の保存先が分かれる |
| SHK-20261021-003 | low | data-protection | 保存先の検査エラーを無視し、既定のCLIがリンク先へ保存する |
| SHK-20261021-004 | low | data-protection | JSONLの末端ファイルのリンク経由で別の記録を読み書きする |
| SHK-20261021-005 | low | input-validation | 末尾 / のないURLへの転送でspec・traceの指定が消える |

## 前提と確認範囲

利用者1人のローカル運用で、HTTPはループバックに待ち受け、UIとAPIに利用者認証はない。プロジェクトの選択は登録した実パスのハッシュで行う。プロジェクトごとの別ユーザー権限やtenant認証は備えていない。POSTにはOrigin・JSONの種類・本文サイズの検査がある。

未認証の共通HTTPから個別HTTP、CLI/MCPの保存先解決、会話・教訓・仮採点・正解・較正の保存までを追った。同名のプロジェクト、サブディレクトリ、URLの相対参照、リンク、初期化、同時実行、出力先の復元も確認した。実データ・設定・実モデル・稼働中のサーバーは変更せず、合成データ・偽モデル・隔離ディレクトリ・Go overlayで再現した。

通常の221 Goテスト、画面8検査、本番ビルドは成功している。以下は通常の検査で捉えられていなかった境界の不具合である。再現テストが成功するのは「問題が起きた」ことを確認するためで、実装が正常という意味ではない。

## SHK-20261021-001：同時較正の報告が別プロジェクトへ入る

- **条件**：共通画面から異なるプロジェクトの自動較正やtuneが同時に走る。通常の操作だけで発生し、ファイル操作の権限や特殊な設定は不要。
- **影響**：Aの較正報告がBのHTTP応答へ入り、Aの報告は欠ける。終了後の出力先も終了済みのバッファへ変わったままになる。較正ファイル自体が別の仕事へ書かれることは、この再現では確認していない。
- **根拠**：`cmd/shikii/project.go:262` は仕事ごとに別のserverを保持する。`cmd/shikii/serve.go:537` と `cmd/shikii/serve_tune.go:40` は個別serverのcalibMuで、プロセス共通のstdoutを差し替える。
- **再現結果**：合成A/Bの実際のhubへ `/v1/label` を送り、偽モデルの待機で較正を重ねた。Bの応答へのAの較正先混入、Aの報告欠落、stdout復元不一致を確認。別の再現ではrace detectorも競合を検出した。
- **原因**：守る状態はプロセス共通なのに、排他はプロジェクトごとになっている。
- **推奨修正**：報告先を明示的なio.Writerで渡す。暫定的な修正なら、stdoutを捕捉する全経路でプロセス共通の排他を使う。
- **受け入れ条件**：A/Bの較正とtuneを重ねても各応答に自分の報告だけが入り、出力先が復元され、race検査も通る。
- **既存項目との差分**：operations.md §5fの「tuneがstdout捕捉のため較正を待たせる」は既知。今回のhubでは別serverの排他が効かず、待機に加えて報告が交差する。2人の監査役の同じ指摘は本項へ統合した。

## SHK-20261021-002：CLIの既定保存先が統一されていない

- **条件**：Gitリポジトリのサブディレクトリから、オプションで保存先を指定せずinit・new・tuneを実行する。
- **影響**：newで作ったspecが既定の画面/MCPに現れない。正解があるのにtuneは「0 truths」で失敗する。initはサブディレクトリに二つ目の保存先とMCP設定を作る。
- **根拠**：`cmd/shikii/new.go:29`、`cmd/shikii/init.go:28`・`:35`・`:45`、`cmd/shikii/tune.go:70` はcwdの `.shikii` を使う。一方、bindStore、MCP、serveはプロジェクトルートを使う。
- **再現結果**：合成repoのdocs/でnewを実行するとdocs/.shikii/decisionsへ保存された。initはdocs/.shikii/wikiとdocs/.mcp.jsonを作った。プロジェクトルートに合成ラベル2件を保存してdocs/からtuneすると「0 truths < eval.min_n=2」で失敗し、同じcwd・spec・偽モデルで正規のstoreを明示すると2件を採点して成功した。
- **原因**：プロジェクトルートを解決する処理への移行漏れ。
- **推奨修正**：明示した保存先は保持し、既定の作成・初期化・採点・MCP設定は同じプロジェクト解決へ揃える。
- **受け入れ条件**：プロジェクトルートとサブディレクトリから作成・初期化・tuneを実行しても、同じspec・wiki・正解・MCP参照先を使い、二つ目の保存先を作らない。
- **他項との差分**：リンクや検査エラーがなくても起こる。SHK-20261021-003とは別の修正単位。

## SHK-20261021-003：保存先の拒否を既定値で取り消す

- **条件**：同一UIDのファイル操作で、Aの `.shikii` をBの保存先へのリンクにする。遠隔や別UIDへの権限拡大は確認していないためlowと評価する。
- **影響**：AのCLI入力がBの記録へ保存される。
- **根拠**：`cmd/shikii/project.go:79` のdefaultProjectStoreは、currentProjectStoreの検査エラーを `.shikii` へ置き換える。`cmd/shikii/main.go:176` などの呼び出し側は検査をやり直さない。
- **再現結果**：currentProjectStoreはリンクを拒否した。同じcwdで実際のcmdDecideを--storeなしで実行すると、Aの合成入力がBの記録へ保存された。MCP/serveも同じ既定値解決を使うことは静的に確認した。
- **原因**：保存先を拒否したエラーと、単なる既定値の選択を区別していない。
- **推奨修正**：保存先解決をエラー付きで返し、拒否した保存先では読み書きと起動を止める。
- **受け入れ条件**：`.shikii` や主要サブディレクトリをリンクへ差し替えても、既定CLI/MCP/serveが拒否し、リンク先のファイルを増やさない。
- **他項との差分**：主要フォルダの検査は拒否できている。次項は、その検査を通る通常フォルダ内の最終ファイルが原因。

## SHK-20261021-004：JSONLの末端リンクをたどる

- **条件**：同一UIDの操作で、Aのrecords/intent.jsonlをBの記録へのリンクにできる。Aの主要フォルダ自体は通常のフォルダ。遠隔や別UIDへの権限拡大は確認していないためlowと評価する。
- **影響**：Aの画面APIがBの入力を返し、Aからの記録追記もBへ届く。
- **根拠**：`cmd/shikii/project.go:91` の検査は主要フォルダだけ。`internal/store/store.go:644`・`:653` のJSONL読み書きは最終ファイルのリンクをたどる。
- **再現結果**：Aの保存先・hub登録の検査を通過した。AのGET queueにBだけに保存した合成markerが現れ、通常のAppendRecordによるAの追記もBに到達した。
- **原因**：フォルダの検査だけでは、下のファイルの参照先を保証できない。
- **推奨修正**：JSONLの読み書きも通常ファイルを確認し、リンクを拒否する。生成領域の中間フォルダも同じ方針に揃える。
- **受け入れ条件**：records・labels・feedback・reviewsの最終ファイルと中間フォルダをリンクへ差し替えても、別の保存先を読み書きしない。
- **他項との差分**：SHK-20261021-003を直しても本経路は残る。長文用の原文ファイルにはO_NOFOLLOWがあり、今回のJSONLとは違う。

## SHK-20261021-005：末尾 / の補完でqueryを失う

- **条件**：`/p/<id>?spec=done-as-asked&trace=<合成ID>` のように末尾 / を省いたURLを開く。生成される通常のリンクには末尾 / が付く。
- **影響**：307の転送先が `/p/<id>/` だけになり、開く判定と記録の指定が失われる。記録は消えない。
- **根拠**：`cmd/shikii/project.go` のnewProjectHubは、末尾 / がない場合にqueryを含まないp.URLへ転送する。
- **再現結果**：隔離hubで307を受け、Locationに元のspec・traceが含まれないことを確認した。
- **原因**：正規のパスを組み立てる時にRawQueryを引き継いでいない。
- **推奨修正**：正規化したパスへ元のqueryを保持して転送する。
- **受け入れ条件**：末尾 / の有無にかかわらず、同じspec・traceが開く。未知のプロジェクトは引き続き拒否する。

## 再現資料

この機械の隔離ディレクトリに、再現コード・overlay・ログを保持している。合成markerだけを使い、実会話や秘密は含めていない。

```sh
go test -overlay /tmp/shikii-independent-project-audit/overlay.json ./cmd/shikii \
  -run '^TestAuditReproduce(ProjectDefaultSymlink|HubCalibrationOutputCrossesProjects|HubFollowsLeafRecordSymlink)$' \
  -count=1 -v

go test -overlay /tmp/shikii-audit-ui-dvr_ndb2/overlay.json ./cmd/shikii \
  -run '^TestAuditUI(SubdirCommands|SubdirTuneReadsDifferentStore|HubPath|HostRead)$' \
  -count=1 -v
```

- `/tmp/shikii-independent-project-audit/`：project_audit_test.go、reproduction.log、leaf-record-link.log。
- `/tmp/shikii-audit-ui-dvr_ndb2/`：audit_ui_runtime_test.go、reproductions.log、tune.log。
- race検出：後者のoverlayで `-race -run '^TestAuditUIProjectConcurrentCapture$'` を使うと競合を検出する。競合を実証するための検査であり、失敗が再現結果となる。

## 未解決の運用事項と未確認範囲

- **OPS-SEED-001（既存動作、low / availability）**：seedAgentSpecsが最終ファイルを作った直後に一度だけ合成I/O中断を入れると、空のspecが再試行後も既存ファイルとして残ることを確認した。以前の既存ファイル保持方式にもある回復性の課題で、今回の差分の5件には数えていない。空の最終名を公開しない原子的な作成と、正常な既存ファイルの保持を両立するのが修正条件。証跡は `/tmp/shikii-independent-project-audit/interrupted-seed.log`。
- **CHK-HOST-001（未確認境界）**：合成の不正HostによるGETで、hub一覧と個別queueの合成markerへHTTP 200で到達することを確認した。実ブラウザとDNSを組み合わせた到達は未確認で、DNS rebindingによる攻撃成立は断定しない。既存S3のPOST Origin対策とは別の確認事項。
- 依存パッケージやCIの権限設定の再監査、別UID間の認証・認可、実利用の負荷、各OSの耐障害性までは検証していない。修正後は上記の受け入れ条件を回帰テストにし、RED→GREENを確認する必要がある。

この監査は脆弱性がないことを保証するものではない。修正は未実施、push・公開はしていない。
