#!/bin/sh
# Go のテストを流す(make test・CI)。cmd/shikii のテストは HOME・作業フォルダ・標準入出力などの全体の値を差し替えるので、
# 同じプロセスの中では並べられない(t.Parallel は使えない)。そこで、テストの名前で組に分け、組ごとに別のプロセスで同時に流す。
# 組の数は決まり: CPU の数の半分(1〜8)。他のパッケージは go test がもともと並べて流す。どれか 1 つでも落ちれば 1 で終わる。
set -eu
cd "$(dirname "$0")/.."

cpus=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 2)
n=$((cpus / 2))
[ "$n" -lt 1 ] && n=1
[ "$n" -gt 8 ] && n=8

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

others=$(go list ./... | grep -v '/cmd/shikii$')
(go test $others > "$tmp/others.log" 2>&1; echo $? > "$tmp/others.rc") &

go test -c -o "$tmp/shikii.test" ./cmd/shikii
"$tmp/shikii.test" -test.list '.*' | grep '^Test' > "$tmp/names"
total=$(wc -l < "$tmp/names" | tr -d ' ')

i=0
while [ "$i" -lt "$n" ]; do
	# i 番目の組: 名前の一覧の i 行目から n 行おき
	awk -v n="$n" -v i="$i" 'NR % n == i' "$tmp/names" | paste -sd '|' - > "$tmp/shard$i"
	if [ -s "$tmp/shard$i" ]; then
		(cd cmd/shikii && "$tmp/shikii.test" -test.run "^($(cat "$tmp/shard$i"))\$" > "$tmp/shard$i.log" 2>&1; echo $? > "$tmp/shard$i.rc") &
	else
		echo 0 > "$tmp/shard$i.rc"
	fi
	i=$((i + 1))
done
wait

fail=0
if [ "$(cat "$tmp/others.rc")" != 0 ]; then
	echo "--- cmd/shikii 以外のパッケージが落ちた:"
	cat "$tmp/others.log"
	fail=1
fi
i=0
while [ "$i" -lt "$n" ]; do
	if [ "$(cat "$tmp/shard$i.rc")" != 0 ]; then
		echo "--- cmd/shikii の組 $i が落ちた:"
		grep -E -- '--- FAIL|_test.go:|panic|FAIL' "$tmp/shard$i.log" | head -40
		fail=1
	fi
	i=$((i + 1))
done
if [ "$fail" -ne 0 ]; then
	echo "go-test: FAIL"
	exit 1
fi
echo "go-test: ok (cmd/shikii の $total 本を $n 組で同時に流した)"
