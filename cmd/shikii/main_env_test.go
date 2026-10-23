package main

import (
	"os"
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
	os.Exit(m.Run())
}
