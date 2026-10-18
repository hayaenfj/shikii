# Docker で動かす

Go もモデルの準備も要らない入り口。2 つの容れ物（llama.cpp の公式サーバーと shikii）を `docker compose` で立てる。

```sh
git clone https://github.com/hayaenfj/shikii && cd shikii
docker compose up            # 初回は Kev-4B(約 2.5 GB)を取ってくる
# → http://127.0.0.1:8095 が答え合わせの画面
```

- プロジェクトルート（このフォルダ）が `/work` に mount される。`decisions/`（判定）、`wiki/`（知識）、`.shikii/`（記録と正解）はホストのファイルがそのまま使われ、容れ物を消しても残る
- Linux では bind mount の所有者がホストのままなので、compose は `user: "${UID:-1000}:${GID:-1000}"` で動かす（`export UID GID` してから `docker compose up`。合わなければ `.shikii/` に書けず答えが保存されない）
- 他の容れ物（podman、k8s）では、`/.dockerenv` か `/run/.containerenv` か `KUBERNETES_SERVICE_HOST` があれば全ての口で待つ。無い容れ物では 127.0.0.1 に縛られるので host network を使う
- 画面は `127.0.0.1:8095` にだけ出す（LAN には出さない）。容れ物の中では shikii は全ての口で待つ（`/.dockerenv` があれば。ホストで直接動かす時は今までどおり 127.0.0.1 だけ）
- 鍵は環境変数でだけ渡す。hosted も使うなら compose の `environment:` に `DECISIONS_API_KEY` を足す
- 普通の GGUF（素のチャット用モデル）を使うなら、`llama` の command を `-m <file>` にし、間に `shikii-readout` の容れ物を立てて shikii の `--local` をそこに向ける（compose の末尾のコメント）
- 判定を自分のプログラムから叩くなら `POST http://127.0.0.1:8095/v1/decide`、正解は `/v1/label`（[mcp.md](mcp.md) と同じ口）

画像は `Dockerfile`（多段。実行側は distroless、root では動かない）。CI が毎回 `docker build` して壊れていないことを確かめる。
