#!/bin/sh
# shikii を 1 行で入れる: curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
# Go 1.27 以上があれば go install で ~/go/bin(または GOBIN)に shikii と shikii-readout が入る。
# その後 `shikii init` をプロジェクトルート(実行する場所)で 1 回打てば、エージェントの準備は終わり。
set -eu
if ! command -v go >/dev/null 2>&1; then
  echo "install.sh: Go 1.27 or newer is needed (https://go.dev/dl/). Or use Docker: docker compose up" >&2
  exit 1
fi
GO_MINOR=$(go version | sed -E 's/.*go1\.([0-9]+).*/\1/')
if [ "$GO_MINOR" -lt 27 ]; then
  echo "install.sh: Go 1.27 or newer is needed (found $(go version))" >&2
  exit 1
fi
if [ -f go.mod ] && grep -q 'module github.com/hayaenfj/shikii' go.mod; then
  go install ./cmd/shikii ./cmd/shikii-readout      # repo の中: 今のソースから
else
  go install github.com/hayaenfj/shikii/cmd/shikii@latest github.com/hayaenfj/shikii/cmd/shikii-readout@latest
fi
BIN="$(go env GOBIN)"; [ -n "$BIN" ] || BIN="$(go env GOPATH)/bin"
# cmd/shikii-readout の実行ファイル名は readout になるので、shikii-readout の名前にも置く
[ -f "$BIN/readout" ] && cp "$BIN/readout" "$BIN/shikii-readout"
echo "installed: $BIN/shikii $BIN/shikii-readout"
case ":$PATH:" in *":$BIN:"*) ;; *) echo "add to PATH: $BIN";; esac
echo "next: cd <your project> && shikii init"
