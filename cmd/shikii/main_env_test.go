package main

import (
	"os"
	"reflect"
	"syscall"
	"testing"
)

// TestMain は、開発者の環境にある鍵(TYPESAFE_API_KEY / DECISIONS_API_KEY)と SHIKII_MODEL をテストの前に外す。
// 外さないと、手元の偽モデルを前提にしたテストが本物の api.typesafe.ai へ要求を送り、
// 置き場の記録・較正・帯が変わって落ちる(2026-10-23、鍵を環境変数に置いた直後に 6 件が落ちた)。
// Jev を試すテスト(hook_hosted_test.go)は自分で t.Setenv する。
func TestMain(m *testing.M) {
	for _, k := range []string{"TYPESAFE_API_KEY", "DECISIONS_API_KEY", "SHIKII_MODEL"} {
		os.Unsetenv(k)
	}
	// 本物の裏のプロセス(up・serve)とブラウザは、どのテストでも起こさない(土台を通らないテストの差し替え忘れで、本物が残った。0.9.1)
	hookSpawn = recordSpawn
	openBrowser = func(url string) { opened = append(opened, url) }
	os.Exit(m.Run())
}

// テストは本物の裏のプロセス(up・serve)を起こさず、ブラウザも開かない(0.9.1)。土台(hookEnv)を通らずに hook を動かすテストでも、
// llama-server のある機械では本物の `shikii.test up` が起き、片付けられずに残った(2026-10-25、75 本が git を呼び続け、
// git のたびに xcodebuild -license check が起きて機械の負荷が 150 を超えた)
func TestTestsNeverStartRealProcesses(t *testing.T) {
	if reflect.ValueOf(hookSpawn).Pointer() == reflect.ValueOf(spawnShikii).Pointer() {
		t.Fatal("テストの間、hookSpawn は本物の spawnShikii ではなく記録だけの物")
	}
	if reflect.ValueOf(openBrowser).Pointer() == reflect.ValueOf(openInBrowser).Pointer() {
		t.Fatal("テストの間、openBrowser は本物のブラウザを開かない")
	}
}

// 本体の側の守り(0.9.1): go のテスト用の実行ファイル(*.test)からは、差し替え忘れがあっても本物の裏のプロセスを起こさず、ブラウザも開かない
func TestSpawnRefusesFromATestBinary(t *testing.T) {
	if pid := spawnShikii(t.TempDir(), "up.log", "up"); pid != 0 {
		_ = syscall.Kill(-pid, syscall.SIGKILL) // 守りが無い時に起きた分は、増える前にその組ごと止める
		t.Fatalf("テスト用の実行ファイルから本物の up を起こした: pid %d", pid)
	}
	if !runningAsTestBinary() {
		t.Fatal("go test の実行ファイルを見分けられない")
	}
}
