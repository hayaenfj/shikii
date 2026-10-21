# Docker・Podmanで業務のAPIと画面を動かす

コンテナの入口は業務の判定APIと答え合わせの画面向け。ホストのClaude Code・Codexにhookを入れる操作は、[通常の導入](../docs/readme/README_ja.md#始め方)で行う。

## 用意するもの

| 構成 | 必要なもの |
|---|---|
| Docker | 稼働中のDocker EngineとDocker Compose、リポジトリのclone |
| Podman | 稼働中のPodmanエンジンと外部のCompose provider、リポジトリのclone |
| 画像をビルドする時 | ネットワーク。Node.js 24とGo 1.27はビルド用のコンテナが用意する |
| 初めて判定モデルを動かす時 | モデルを取得する通信と保存領域。既定はKev-4B Q4_K_M（約2.5GB） |

[Docker Composeの説明](https://docs.docker.com/compose/)。Podmanの `compose` は、docker-composeまたはpodman-composeなど外部のproviderを呼ぶため、Podman本体とは別にproviderが必要。[Podman公式の説明](https://docs.podman.io/en/latest/markdown/podman-compose.1.html)。

## 初回の準備と起動

リポジトリをcloneし、shikiiのフォルダで、最初に画像を組み、業務の問いを1つ用意する。

```sh
docker compose build shikii
docker compose run --rm --no-deps shikii new same-issue --from decisions/same-issue.yaml
docker compose up
# → http://127.0.0.1:8095
```

すでに `.shikii/decisions/` へ問いを準備したプロジェクトでは、起動から進める。`new` は既存の同名の問いを上書きしない。サンプルをコピーする時に、サンプルの古い線は持ち込まない。

Linuxでbind mountの所有者を合わせる場合は、各composeコマンドに `env UID="$(id -u)" GID="$(id -g)"` を前置する。Compose構成の `user` がその値を使う。シェルのreadonlyなUID変数に代入せず、子プロセスへ値を渡す。

Podmanの場合は、providerを用意して同じ `docker-compose.yml` を `podman compose` から使う。Composeとモデルを組み合わせた一式の起動は、この更新では実測していない。providerやホスト側のvolumeの権限も含めて確認する。

## 保存先とモデル

- プロジェクトを `/work` にmountし、`.shikii/` の問い・知識・記録・正解をホストへ残す。コンテナを作り直しても同じ資料を使う。
- Composeはllama.cppの公式サーバーとshikiiを立てる。llama.cppは `0.0.0.0:8080`、shikiiは `http://llama:8080` の準備ができるのを待つ。ホストの画面は `127.0.0.1:8095` に出す。
- shikiiはDockerの `/.dockerenv`、Podmanの `/run/.containerenv` などからコンテナを判別する。モデルやruntimeを替える場合は、その構成の待ち受けと接続先を確認する。
- 外部APIの鍵は環境変数で渡す。利用する判定が外部送信を許可していることも確かめる。
- 通常のチャットGGUFを使う場合は、llama.cppとの間に `shikii-readout` を置く。[変換役の条件と実行例](readout.md)。任意のチャットAPIへそのままつながる変換役ではない。

HTTPの判定は `POST /v1/decide`、正解は `POST /v1/label`。JSONのContent-Typeが必要。[APIの一覧](../docs/guide.ja.md#serve-の-http-api)。

## 画像の中身とライセンス

`Dockerfile` はNode.jsで画面を組み、Goで2つの実行ファイルを組み、実行用のdistroless画像へ入れる。ホストで使うだけならGo・Node.jsは不要。実行時はrootで動かさない。

shikii本体は `Apache-2.0 OR MIT`。OCIの `org.opencontainers.image.licenses` に同じ式を付け、`LICENSE`・`LICENSE-APACHE`・`LICENSE-MIT`・`THIRD-PARTY-NOTICES` を `/usr/share/licenses/shikii/` へ入れる。依存やベース画像・モデルの条件はそれぞれに従う。
