#!/bin/sh
# Install shikii in one line: curl -fsSL https://raw.githubusercontent.com/hayaenfj/shikii/main/install.sh | sh
# It downloads the latest release for this machine (macOS or Linux, amd64 or arm64) from GitHub, checks it against SHA256SUMS,
# and puts shikii and shikii-readout in ~/.local/bin (a fixed place that does not move when Go or a version manager is upgraded).
# Inside a source checkout (`sh install.sh` in its folder) it builds the current source with Go 1.27 or later instead, into the same place.
# Then it runs `shikii hook install --yes`: hooks for Claude Code and Codex (which measure the AI's own judgment), the agent's working method (skill),
# and the local model (started in the background if llama-server is present). Run shikii --init once in each project that uses it.
# Asking an agent "I want to use shikii in this project" also gets it initialized. Nothing is recorded in places that are not initialized.
# Everything is in main, called on the last line: when `curl | sh` gets only part of this file, the shell runs none of it.
set -eu
REPO=https://github.com/hayaenfj/shikii
fail() { echo "install.sh: $*" >&2; exit 1; }
main() {
  BIN="$HOME/.local/bin"
  mkdir -p "$BIN"
  # Prepare everything in a folder next to the destination, then move each file into place with mv
  # (overwriting a binary in use makes it fail with Killed: 9 on macOS).
  # what is where the files go must be a file (or a link to one): a folder or anything else there is a person's, and is never moved
  # into or removed. Checked before anything is downloaded or placed, so a stop here changes nothing
  for f in shikii shikii-readout; do
    if [ -e "$BIN/$f" ] && [ ! -f "$BIN/$f" ]; then fail "$BIN/$f is not a file; nothing was installed (move it away and run this again)"; fi
  done
  if [ -e "$BIN/shikii-licenses" ] && [ ! -d "$BIN/shikii-licenses" ]; then fail "$BIN/shikii-licenses is not a folder; nothing was installed (move it away and run this again)"; fi
  for f in LICENSE LICENSE-APACHE LICENSE-MIT THIRD-PARTY-NOTICES; do
    if [ -e "$BIN/shikii-licenses/$f" ] && [ ! -f "$BIN/shikii-licenses/$f" ]; then fail "$BIN/shikii-licenses/$f is not a file; nothing was installed (move it away and run this again)"; fi
  done
  STAGE=$(mktemp -d "$BIN/.shikii-install-XXXXXX")
  trap 'rm -rf "$STAGE"' EXIT
  # dash and zsh skip the EXIT trap when a signal (Ctrl-C) stops the script; exiting from the signal runs it
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM
  if [ -f go.mod ] && grep -q '^module github.com/hayaenfj/shikii$' go.mod; then
    command -v go >/dev/null 2>&1 || fail "building from this source checkout needs Go 1.27 or later (https://go.dev/dl/). Run the one line from another folder to install the release instead"
    GO_MINOR=$(go version | sed -E 's/.*go1\.([0-9]+).*/\1/')
    [ "$GO_MINOR" -ge 27 ] || fail "Go 1.27 or later is required (current: $(go version))"
    go build -o "$STAGE/shikii" ./cmd/shikii
    go build -o "$STAGE/shikii-readout" ./cmd/shikii-readout
    echo "Built shikii from the source in $(pwd)"
  else
    case "$(uname -s)" in
      Darwin) OS=darwin ;;
      Linux) OS=linux ;;
      *) fail "no release is provided for $(uname -s); build from source with Go instead (see the README)" ;;
    esac
    case "$(uname -m)" in
      x86_64 | amd64) ARCH=amd64 ;;
      arm64 | aarch64) ARCH=arm64 ;;
      *) fail "no release is provided for $(uname -m); build from source with Go instead (see the README)" ;;
    esac
    # GitHub redirects /releases/latest to the newest release's tag as soon as it is published.
    LATEST=$(curl -fsSL -o /dev/null -w '%{url_effective}' "$REPO/releases/latest") || fail "cannot reach $REPO"
    TAG=${LATEST##*/}
    case "$TAG" in
      v[0-9]*.[0-9]*.[0-9]*) ;;
      *) fail "cannot find the latest release at $REPO/releases" ;;
    esac
    VERSION=${TAG#v}
    ASSET="shikii_${VERSION}_${OS}_${ARCH}.tar.gz"
    echo "Downloading shikii $VERSION ($OS/$ARCH)…"
    curl -fsSL -o "$STAGE/$ASSET" "$REPO/releases/download/$TAG/$ASSET" || fail "cannot download $ASSET"
    curl -fsSL -o "$STAGE/SHA256SUMS" "$REPO/releases/download/$TAG/SHA256SUMS" || fail "cannot download SHA256SUMS"
    WANT=$(awk -v f="$ASSET" '$2 == f || $2 == "*" f { print $1 }' "$STAGE/SHA256SUMS")
    if command -v sha256sum >/dev/null 2>&1; then
      GOT=$(sha256sum "$STAGE/$ASSET" | awk '{ print $1 }')
    else
      GOT=$(shasum -a 256 "$STAGE/$ASSET" | awk '{ print $1 }')
    fi
    [ -n "$WANT" ] && [ "$WANT" = "$GOT" ] || fail "the hash of $ASSET does not match SHA256SUMS; nothing was installed"
    mkdir "$STAGE/files"
    tar -xzf "$STAGE/$ASSET" -C "$STAGE/files" shikii shikii-readout LICENSE LICENSE-APACHE LICENSE-MIT THIRD-PARTY-NOTICES || fail "cannot extract $ASSET"
    # only plain files: chmod and mv below would otherwise act on whatever a link in the archive points at
    for f in shikii shikii-readout LICENSE LICENSE-APACHE LICENSE-MIT THIRD-PARTY-NOTICES; do
      [ -f "$STAGE/files/$f" ] && [ ! -L "$STAGE/files/$f" ] || fail "$f in $ASSET is not a plain file; nothing was installed"
    done
    [ "$("$STAGE/files/shikii" --version)" = "shikii $VERSION" ] || fail "the downloaded shikii does not start as version $VERSION; nothing was installed"
    mkdir -p "$BIN/shikii-licenses"
    for f in LICENSE LICENSE-APACHE LICENSE-MIT THIRD-PARTY-NOTICES; do mv -f "$STAGE/files/$f" "$BIN/shikii-licenses/$f"; done
    mv -f "$STAGE/files/shikii" "$STAGE/files/shikii-readout" "$STAGE/"
  fi
  chmod 755 "$STAGE/shikii" "$STAGE/shikii-readout"
  mv -f "$STAGE/shikii-readout" "$BIN/shikii-readout" || fail "cannot put shikii-readout in $BIN; nothing was changed"
  mv -f "$STAGE/shikii" "$BIN/shikii" || fail "cannot put shikii in $BIN: only shikii-readout was updated; run this again"
  echo "Installed: $BIN/shikii $BIN/shikii-readout"
  case ":$PATH:" in *":$BIN:"*) ;; *) echo "Add this to your PATH: $BIN (for example, add export PATH=\"\$HOME/.local/bin:\$PATH\" to your shell's profile; hooks use the absolute path of the binary)";; esac
  "$BIN/shikii" hook install --yes
  echo "Installation complete. Open a new terminal and run shikii --init once in each project that uses it."
  echo "You can also ask an agent \"I want to use shikii in this project\" to initialize it. Nothing is recorded in places that are not initialized."
}
main "$@"
