package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wiki のパターンのページは「主張 + 条件 + 証拠 + 再検証の手順」。前書き(YAML)を機械が読み、本文は人のもの。
func TestClaimPageFrontMatterRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.md")
	src := "---\nclaim: 素のチャット用モデルには plain 配置\nstatus: tentative\napplies_to:\n  type: choice\n  models: plain chat models through shikii-readout\nretest:\n  spec: decisions/intent.yaml\n  better: plain\n  than: current\n---\n# 題\n\n本文はそのまま。\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	pg, body, err := readClaimPage(path)
	if err != nil {
		t.Fatal(err)
	}
	if pg.Claim == "" || pg.Status != "tentative" || pg.Retest == nil || pg.Retest.Better != "plain" || pg.Retest.Than != "current" || pg.AppliesTo["type"] != "choice" {
		t.Fatalf("front matter: %+v", pg)
	}
	pg.Evidence = append(pg.Evidence, claimEvidence{Date: "2026-10-18", Model: "m", Result: "plain 0.9 vs current 0.5", Verdict: "supports"})
	if err := writeClaimPage(path, pg, body); err != nil {
		t.Fatal(err)
	}
	again, body2, err := readClaimPage(path)
	if err != nil || len(again.Evidence) != 1 || body2 != body || !strings.Contains(body2, "本文はそのまま。") {
		t.Fatalf("the body is the person's and must survive a machine write: %v %+v\n%s", err, again, body2)
	}
	// 前書きの無いページは主張ではない(読めるが retest 無し)
	if err := os.WriteFile(path, []byte("# 古い形のページ\n\nstatus: tentative\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pg, _, err = readClaimPage(path)
	if err != nil || pg.Retest != nil {
		t.Fatalf("a page without front matter has no retest: %v %+v", err, pg)
	}
}

// wiki verify は各主張の再検証を今のモデルで回し、支持 / 反証 / 不確定を返して証拠を書き足す。状態は変えない(人が変える)。
func TestWikiVerifyRunsRetestsAndRecordsEvidence(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	srv := layoutSensitiveModel(t) // 既定の配置では全部 change_request、plain なら当てる
	if err := os.MkdirAll(filepath.Join(dir, "decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	specPath := writeSpec(t, filepath.Join(dir, "decisions"), "false")
	b, _ := os.ReadFile(specPath)
	src := string(b) + "variants:\n  - name: plain\n    layout: plain\n  - name: worse\n    question: What is asked? Anything goes.\n"
	if err := os.WriteFile(specPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(bankRows()), 0o644); err != nil {
		t.Fatal(err)
	}
	pdir := filepath.Join(dir, "wiki", "patterns")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	page := func(name, better, than string) string {
		p := filepath.Join(pdir, name+".md")
		src := "---\nclaim: " + name + "\nstatus: tentative\napplies_to:\n  type: choice\nretest:\n  spec: decisions/intent.yaml\n  better: " + better + "\n  than: " + than + "\n---\n# " + name + "\n"
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	supported := page("plain-helps", "plain", "current")
	refuted := page("worse-helps", "worse", "current")
	plain := filepath.Join(pdir, "no-retest.md")
	if err := os.WriteFile(plain, []byte("# 手で書いた古いページ\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdWiki([]string{"verify", "--local", srv.URL, "--labeled", labeledPath, "--store", filepath.Join(dir, ".shikii")})
	})
	for _, must := range []string{"plain-helps", "supports", "worse-helps", "contradicts", "no-retest.md: no retest"} {
		if !strings.Contains(out, must) {
			t.Fatalf("verify must report every page; missing %q:\n%s", must, out)
		}
	}
	pg, _, err := readClaimPage(supported)
	if err != nil || len(pg.Evidence) != 1 || pg.Evidence[0].Verdict != "supports" || pg.Evidence[0].Model == "" || pg.Status != "tentative" {
		t.Fatalf("supporting evidence is recorded and the status is left to a person: %v %+v", err, pg)
	}
	if !strings.Contains(pg.Evidence[0].Result, "plain 1.000 vs current 0.500") {
		t.Fatalf("the result carries both numbers: %+v", pg.Evidence[0])
	}
	pg, _, _ = readClaimPage(refuted)
	if len(pg.Evidence) != 1 || pg.Evidence[0].Verdict != "contradicts" {
		t.Fatalf("contradicting evidence is recorded too: %+v", pg)
	}
	// 同じモデルで繰り返しても「独立した観察」は増えない。2 つ目のモデルで支持されたら confirmable と言う
	out = capture(t, func() error {
		return cmdWiki([]string{"verify", "--local", srv.URL, "--labeled", labeledPath, "--store", filepath.Join(dir, ".shikii"), supported})
	})
	if strings.Contains(out, "confirmable") {
		t.Fatalf("one model observed twice is still one observation:\n%s", out)
	}
	pg, body, _ := readClaimPage(supported)
	pg.Evidence = append(pg.Evidence, claimEvidence{Date: "2026-10-18", Model: "another-model", Result: "x", Verdict: "supports"})
	if err := writeClaimPage(supported, pg, body); err != nil {
		t.Fatal(err)
	}
	out = capture(t, func() error {
		return cmdWiki([]string{"verify", "--local", srv.URL, "--labeled", labeledPath, "--store", filepath.Join(dir, ".shikii"), supported})
	})
	if !strings.Contains(out, "confirmable") {
		t.Fatalf("two independent models supporting → a person may confirm:\n%s", out)
	}
}

// 自動で育つ: tune の run が終わるごとに、現状に有意に勝った候補を主張の下書きとして起草し、
// その spec についての既存の主張には run の採点から証拠を足す(モデルは呼び直さない)。人は下書きを一般化して patterns/ に移すだけ。
func TestTuneDraftsClaimsAndFeedsEvidenceToExistingOnes(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	srv := wordingSensitiveModel(t)      // strict が why を当て、worse は current と同じ
	specPath := specWithVariants(t, dir) // variants: strict, worse
	labeledPath := filepath.Join(dir, "rows.json")
	if err := os.WriteFile(labeledPath, []byte(bankRows()), 0o644); err != nil {
		t.Fatal(err)
	}
	pdir := filepath.Join(dir, "wiki", "patterns")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(pdir, "strict-beats-worse.md") // worse には主張を置かない: 勝っていない候補が下書きにならないことを見るため
	if err := os.WriteFile(existing, []byte("---\nclaim: strict beats worse\nstatus: tentative\nretest:\n  spec: decisions/intent.yaml\n  better: strict\n  than: worse\n---\n# strict beats worse\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", filepath.Join(dir, ".shikii")})
	})
	if !strings.Contains(out, "1 claim(s) drafted") || !strings.Contains(out, "1 claim(s) given new evidence") {
		t.Fatalf("tune must draft a claim for the significant winner and feed the existing claim:\n%s", out)
	}
	draft := filepath.Join(pdir, "drafts", "intent-strict.md")
	pg, body, err := readClaimPage(draft)
	if err != nil {
		t.Fatalf("draft must exist: %v", err)
	}
	if pg.Status != "draft" || pg.Retest == nil || pg.Retest.Better != "strict" || pg.Retest.Than != "current" || pg.Retest.Spec != "decisions/intent.yaml" || len(pg.Evidence) != 1 || pg.Evidence[0].Verdict != "supports" || pg.Evidence[0].Run == "" || pg.AppliesTo["type"] != "choice" {
		t.Fatalf("draft front matter: %+v", pg)
	}
	if !strings.Contains(body, "起草") {
		t.Fatalf("the draft body tells a person what to do with it:\n%s", body)
	}
	ex, _, _ := readClaimPage(existing)
	if len(ex.Evidence) != 1 || ex.Evidence[0].Verdict != "supports" || ex.Evidence[0].Run == "" || ex.Status != "tentative" {
		t.Fatalf("the existing claim gets this run as evidence without a model call, status untouched: %+v", ex)
	}
	// 2 回目の run: 下書きは重複せず証拠が 2 件に。worse は下書きにならない(有意に勝っていない)
	capture(t, func() error {
		return cmdTune([]string{specPath, "--labeled", labeledPath, "--local", srv.URL, "--store", filepath.Join(dir, ".shikii")})
	})
	pg, _, _ = readClaimPage(draft)
	if len(pg.Evidence) != 2 {
		t.Fatalf("a second run adds evidence to the same draft: %+v", pg.Evidence)
	}
	if _, err := os.Stat(filepath.Join(pdir, "drafts", "intent-worse.md")); err == nil {
		t.Fatalf("a candidate that does not beat current must not become a claim")
	}
}
