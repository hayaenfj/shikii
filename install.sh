#!/bin/sh
# shikii を 1 行で入れる: curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
# Go 1.27 以上があれば go install で ~/go/bin(または GOBIN)に shikii と shikii-readout が入る。
# 続けて `shikii hook install --yes` を流す: Claude Code と Codex の hook(AI 自身の考え方を測る)、エージェントの作法(skill)、
# 手元のモデル(llama-server があれば裏で起こす)。これで人が打つコマンドは終わり。業務の判定(shikii init / new / serve)はエージェントが skill に沿って進める。
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
echo "installed: $BIN/shikii $BIN/shikii-readout"
case ":$PATH:" in *":$BIN:"*) ;; *) echo "add to PATH: $BIN  (the hooks use the binary's own path and work without it; the agent needs it on PATH to run \`shikii\` itself)";; esac
"$BIN/shikii" hook install --yes
echo "done. Open a new terminal and use Claude Code or Codex as usual. To hand it a business decision, just tell the agent what you want decided; it sets shikii up in the project by itself."
