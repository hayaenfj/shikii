#!/bin/sh
# shikii を 1 行で入れる: curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
# Go 1.27 以上があれば go install で ~/go/bin(または GOBIN)に shikii と shikii-readout が入る。
# 続けて `shikii hook install --yes` を流す: Claude Code と Codex の hook(AI 自身の考え方を測る)、エージェントの作法(skill)、
# 手元のモデル(llama-server があれば裏で起こす)。利用するプロジェクトで shikii --init を一度実行する。
# 「このプロジェクトでshikiiを使いたい」と頼めばエージェントが初期化する。未初期化の場所では記録しない。
set -eu
if ! command -v go >/dev/null 2>&1; then
  echo "install.sh: Go 1.27以上が必要です（https://go.dev/dl/）。Dockerを使う場合は docker compose up" >&2
  exit 1
fi
GO_MINOR=$(go version | sed -E 's/.*go1\.([0-9]+).*/\1/')
if [ "$GO_MINOR" -lt 27 ]; then
  echo "install.sh: Go 1.27以上が必要です（現在: $(go version)）" >&2
  exit 1
fi
if [ -f go.mod ] && grep -q 'module github.com/hayaenfj/shikii' go.mod; then
  go install ./cmd/shikii ./cmd/shikii-readout      # repo の中: 今のソースから
else
  go install github.com/hayaenfj/shikii/cmd/shikii@latest github.com/hayaenfj/shikii/cmd/shikii-readout@latest
fi
BIN="$(go env GOBIN)"; [ -n "$BIN" ] || BIN="$(go env GOPATH)/bin"
echo "インストール済み: $BIN/shikii $BIN/shikii-readout"
case ":$PATH:" in *":$BIN:"*) ;; *) echo "PATHに追加してください: $BIN（hookは実行ファイルの絶対パスを使います）";; esac
"$BIN/shikii" hook install --yes
echo "インストール完了。新しいターミナルを開き、利用するプロジェクトで shikii --init を一度実行してください。"
echo "エージェントに「このプロジェクトでshikiiを使いたい」と頼んでも初期化できます。未初期化の場所では記録しません。"
