#!/bin/sh
# Runs the Go tests (make test, CI). The cmd/shikii tests replace global values such as HOME, the working folder and standard input/output,
# so they cannot run side by side in one process (t.Parallel cannot be used). So they are split into groups by test name, and each group runs at the same time in its own process.
# The number of groups is fixed: half the number of CPUs (at least 1). The binary is started directly, so -test.timeout=10m and -test.paniconexit0, which go test would add, are added here, and the last line of each group's log is checked to end with PASS (if a test prints text without a newline, PASS lands on the same line). Inside a group, the exit code is taken with if and always written to a file so that set -e does not stop it. Other packages are run side by side by go test itself. If any one fails, the script exits with 1.
set -eu
cd "$(dirname "$0")/.."

cpus=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 2)
n=$((cpus / 2))
[ "$n" -lt 1 ] && n=1

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
# When interrupted, stop the groups running in the background too (they start ignoring SIGINT)
trap 'trap "" TERM; kill -TERM 0 2>/dev/null; exit 130' INT TERM

# Build first (so that a failure does not leave go test running in the background)
go test -c -o "$tmp/shikii.test" ./cmd/shikii
others=$(go list ./... | grep -v '/cmd/shikii$')
(if go test $others > "$tmp/others.log" 2>&1; then echo 0; else echo 1; fi > "$tmp/others.rc") &

"$tmp/shikii.test" -test.list '.*' | grep -E '^(Test|Fuzz|Example)' > "$tmp/names"
total=$(wc -l < "$tmp/names" | tr -d ' ')

i=0
while [ "$i" -lt "$n" ]; do
	# group i: every n-th line of the name list, starting at line i
	awk -v n="$n" -v i="$i" 'NR % n == i' "$tmp/names" | paste -sd '|' - > "$tmp/shard$i"
	if [ -s "$tmp/shard$i" ]; then
		(cd cmd/shikii && if "$tmp/shikii.test" -test.timeout=10m -test.paniconexit0 -test.run "^($(cat "$tmp/shard$i"))\$" > "$tmp/shard$i.log" 2>&1 && tail -n 1 "$tmp/shard$i.log" | grep -q 'PASS$'; then echo 0; else echo 1; fi > "$tmp/shard$i.rc") &
	else
		echo 0 > "$tmp/shard$i.rc"
	fi
	i=$((i + 1))
done
wait

fail=0
if [ ! -f "$tmp/others.rc" ] || [ "$(cat "$tmp/others.rc")" != 0 ]; then
	echo "--- packages other than cmd/shikii failed:"
	cat "$tmp/others.log"
	fail=1
fi
i=0
while [ "$i" -lt "$n" ]; do
	if [ ! -f "$tmp/shard$i.rc" ] || [ "$(cat "$tmp/shard$i.rc")" != 0 ]; then
		echo "--- cmd/shikii group $i failed:"
		tail -n 200 "$tmp/shard$i.log"
		fail=1
	fi
	i=$((i + 1))
done
if [ "$fail" -ne 0 ]; then
	echo "go-test: FAIL"
	exit 1
fi
echo "go-test: ok (ran $total cmd/shikii tests in $n groups at the same time)"
