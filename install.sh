#!/bin/sh
# Install shikii in one line: curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
# With Go 1.27 or later, go install puts shikii and shikii-readout in ~/go/bin (or GOBIN).
# Then it runs `shikii hook install --yes`: hooks for Claude Code and Codex (which measure the AI's own judgment), the agent's working method (skill),
# and the local model (started in the background if llama-server is present). Run shikii --init once in each project that uses it.
# Asking an agent "I want to use shikii in this project" also gets it initialized. Nothing is recorded in places that are not initialized.
set -eu
if ! command -v go >/dev/null 2>&1; then
  echo "install.sh: Go 1.27 or later is required (https://go.dev/dl/). To use Docker instead: docker compose up" >&2
  exit 1
fi
GO_MINOR=$(go version | sed -E 's/.*go1\.([0-9]+).*/\1/')
if [ "$GO_MINOR" -lt 27 ]; then
  echo "install.sh: Go 1.27 or later is required (current: $(go version))" >&2
  exit 1
fi
if [ -f go.mod ] && grep -q 'module github.com/hayaenfj/shikii' go.mod; then
  go install ./cmd/shikii ./cmd/shikii-readout      # inside the repo: from the current source
else
  go install github.com/hayaenfj/shikii/cmd/shikii@latest github.com/hayaenfj/shikii/cmd/shikii-readout@latest
fi
BIN="$(go env GOBIN)"; [ -n "$BIN" ] || BIN="$(go env GOPATH)/bin"
echo "Installed: $BIN/shikii $BIN/shikii-readout"
case ":$PATH:" in *":$BIN:"*) ;; *) echo "Add this to your PATH: $BIN (hooks use the absolute path of the binary)";; esac
"$BIN/shikii" hook install --yes
echo "Installation complete. Open a new terminal and run shikii --init once in each project that uses it."
echo "You can also ask an agent \"I want to use shikii in this project\" to initialize it. Nothing is recorded in places that are not initialized."
