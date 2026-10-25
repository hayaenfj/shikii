#!/bin/sh
# Goes once around the decide → label → calibrate → check → drift → status → gc loop with no key and no GPU.
# It uses a fake model (cmd/fake-systemone), so the numbers are not real. It is for seeing the steps and the shape of the output.
#   scripts/demo.sh            # the normal fake model
#   scripts/demo.sh --overconfident   # a fake model with probabilities pushed to the edges (compare the raw and calibrated confusion tables)
set -eu
cd "$(dirname "$0")/.."
go build -o bin/shikii ./cmd/shikii
go build -o bin/fake-systemone ./cmd/fake-systemone

PORT=8099
WORK="$(mktemp -d)"
SERVER=""
cleanup() {
  if [ -n "$SERVER" ]; then kill "$SERVER" 2>/dev/null || true; wait "$SERVER" 2>/dev/null || true; fi
  rm -rf "$WORK"
}
trap cleanup EXIT
bin/fake-systemone --port $PORT "$@" >"$WORK/server.log" 2>&1 &
SERVER=$!
until curl -sf "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; do sleep 0.2; done

SPEC="$WORK/intent.yaml"
cp examples/prompt-intent.yaml "$SPEC"   # start from the example without calibration (write-backs go into this copy)
STORE="$WORK/store"
D="bin/shikii"
L="--local http://127.0.0.1:$PORT --store $STORE"

step() { printf '\n\033[1m== %s\033[0m\n' "$*"; }

step "1. decide (no calibration yet, so act does not appear)"
$D decide "$SPEC" $L -i 'message=Fix the typo in README.md'
$D decide "$SPEC" $L -i 'message=この関数は何をしているのか説明して'

step "2. give the truth (to the latest record; in production it is picked up from the user's actions or results)"
$D label --store "$STORE" --spec-id prompt-intent --last --source ask question

step "3. calibrate with the labeled file (62 items). Compare identity / vector with 5-fold CV and write back to the spec"
$D calibrate "$SPEC" --labeled testdata/prompt-intent-labeled.json $L --model fake-systemone/1 --write | grep -E '^(spec=|raw:|at act|calibrated:|vector 5-fold|choose|wrote|WARNING)'

step "4. decide after calibration (act now appears)"
$D decide "$SPEC" $L -i 'message=Fix the typo in README.md' | head -2

step "5. contract check (for CI)"
$D check --store "$STORE" "$SPEC"

step "6. changing one word of the wording invalidates the calibration, and check fails"
sed -i.bak 's/What is the user asking the assistant to do?/What is the user asking the assistant to do? Be strict./' "$SPEC" && rm -f "$SPEC.bak"
$D check --store "$STORE" "$SPEC" || true
$D decide "$SPEC" $L -i 'message=Fix the typo in README.md' | head -2

step "6b. the truths people gave are not gone. Running calibrate without a file re-asks the stored texts with the new wording and the line comes back"
$D calibrate "$SPEC" --labeled none $L --model fake-systemone/1 --write | grep -E '^(replayed|spec=|choose|wrote|WARNING|note:)'
$D check --store "$STORE" "$SPEC"
$D decide "$SPEC" $L -i 'message=Fix the typo in README.md' | head -2

step "7. inventory and drift monitoring (whether the line redrawn in 6b has degraded on recent truths)"
$D status --store "$STORE" "$SPEC"
$D drift "$SPEC" --store "$STORE" --model fake-systemone/1

step "8. clean up (display only; labeled records are kept)"
$D gc --store "$STORE" --older-than 0 "$SPEC"

printf '\nDone. This is a fake model, so the numbers mean nothing. To point at a real one, use --local with llama.cpp (/v1/systemone), or set a key and use --hosted.\n'
