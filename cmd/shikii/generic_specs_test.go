package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hayaenfj/shikii/internal/spec"
)

func TestGenericAgentSpecsKeepCodingCalibrationsSeparate(t *testing.T) {
	// 0.4.4の既製質問のハッシュ。汎用化した質問へ旧い線を引き継がない。
	previous := map[string]string{
		"prompt-kind": "68ed82631e63f2a2", "needs-plan": "66d4d3ef366b8b96",
		"changes-in-scope": "9381415d9f1d3696", "done-as-asked": "2bba11b5ee687b57",
	}
	dir := t.TempDir()
	if n, err := seedAgentSpecs(dir); err != nil || n != len(hookSpecs)+len(wikiSpecs) {
		t.Fatal("既製の質問(hook の4つと wiki の2つ)を準備する", n, err)
	}
	for id, oldHash := range previous {
		t.Run(id, func(t *testing.T) {
			path := filepath.Join(dir, id+".yaml")
			sp := mustLoad(t, path)
			sp.Calibration = []spec.Calibration{{Model: "fixture", SpecHash: oldHash, Method: "identity", N: 50}}
			if sp.Hash() == oldHash || sp.CalibrationFor("fixture") != nil || sp.LiveHash(oldHash) {
				t.Fatal("コーディング向けの旧い質問・較正を新しい質問から分離する")
			}
			if id == "needs-plan" && !needsPlanningContext(sp) {
				t.Fatal("原文付きの合意と未決事項を使う契約を保つ")
			}
			// 利用中の質問と線は再初期化・更新から保護する。
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, []byte("\n# 利用者の質問を保持\n")...)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := seedAgentSpecs(dir); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(data) {
				t.Fatal("既存の質問を汎用版で上書きしない", err)
			}
		})
	}
}
