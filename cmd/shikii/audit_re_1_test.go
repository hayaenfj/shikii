package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 0.9.7 re-audit 1: two project folders swapped with three mv on the same disk (A to T, B to A, T to B).
// Both stores were initialized on this machine and only moved, so by C1 both reconnect on the next request.
func TestRe1SwapTwoProjects(t *testing.T) {
	for _, git := range []bool{true, false} {
		t.Run(map[bool]string{true: "git", false: "nogit"}[git], func(t *testing.T) {
			mk := func() string {
				if git {
					return gitRepo(t)
				}
				return t.TempDir()
			}
			a := mk()
			hookEnv(t, nil, a)
			b := mk()
			initTestProject(t, b)
			a, b = feedbackScope(a), feedbackScope(b)
			idA, idB := storeID(filepath.Join(a, ".shikii"), false), storeID(filepath.Join(b, ".shikii"), false)
			tmp := filepath.Join(filepath.Dir(a), "swap-tmp")
			for _, mv := range [][2]string{{a, tmp}, {b, a}, {tmp, b}} {
				if err := os.Rename(mv[0], mv[1]); err != nil {
					t.Fatal(err)
				}
			}
			noteA := relinkMovedProject(a, false)
			noteB := relinkMovedProject(b, false)
			actA, _ := projectActive(filepath.Join(a, ".shikii"), a)
			actB, _ := projectActive(filepath.Join(b, ".shikii"), b)
			t.Logf("A \u306e\u30d1\u30b9(\u5143\u306f B \u306e\u7f6e\u304d\u5834): %q active=%v / B \u306e\u30d1\u30b9(\u5143\u306f A \u306e\u7f6e\u304d\u5834): %q active=%v", noteA, actA, noteB, actB)
			if !actA || !actB {
				t.Fatalf("C1: \u5165\u308c\u66ff\u3048\u305f 2 \u3064\u306e\u7f6e\u304d\u5834\u306e\u4e21\u65b9\u304c\u3064\u306a\u304e\u76f4\u308b\u306f\u305a: A=%v B=%v; B \u306e\u30d1\u30b9\u3067\u306e\u77e5\u3089\u305b: %q", actA, actB, copiedNotice(b))
			}
			if storeID(filepath.Join(a, ".shikii"), false) != idB || storeID(filepath.Join(b, ".shikii"), false) != idA {
				t.Fatal("store-id \u306f mv \u3057\u305f\u7f6e\u304d\u5834\u306e\u307e\u307e")
			}
		})
	}
}

// 0.9.7 re-audit 1: a usual rename: proj moved to proj-old, another project proj-new moved to proj. When the new proj asks first,
// proj-old (the original proj, only moved) was taken for a copy and not reconnected.
func TestRe1RenameThenReusePath(t *testing.T) {
	p := t.TempDir()
	hookEnv(t, nil, p)
	q := t.TempDir()
	initTestProject(t, q)
	p, q = feedbackScope(p), feedbackScope(q)
	old := filepath.Join(filepath.Dir(p), "proj-old")
	if err := os.Rename(p, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(q, p); err != nil {
		t.Fatal(err)
	}
	old = feedbackScope(old)
	t.Logf("\u65b0\u3057\u3044 proj: %q", relinkMovedProject(p, false))
	t.Logf("proj-old: %q", relinkMovedProject(old, false))
	if act, _ := projectActive(filepath.Join(old, ".shikii"), old); !act {
		t.Fatalf("C1: mv \u3057\u305f\u3060\u3051\u306e proj-old \u304c\u3064\u306a\u304e\u76f4\u3089\u306a\u3044; \u77e5\u3089\u305b: %q", copiedNotice(old))
	}
}

// 0.9.7 re-audit 1: after a reconnection failed inside finishProjectInit (the entry written, project.json not), the next request reconnects (C8).
// The failure is made by making .shikii unwritable (registerProject writes under ~/.shikii and passes; only project.json's temp file fails).
func TestRe1RelinkFailingAtProjectJSONRecoversNextTime(t *testing.T) {
	for _, mode := range []string{"fail-then-retry", "crash-after-register"} {
		t.Run(mode, func(t *testing.T) {
			p := t.TempDir()
			hookEnv(t, nil, p)
			p = feedbackScope(p)
			r := filepath.Join(t.TempDir(), "renamed")
			if err := os.Rename(p, r); err != nil {
				t.Fatal(err)
			}
			r = feedbackScope(r)
			root := filepath.Join(r, ".shikii")
			if mode == "fail-then-retry" {
				os.Chmod(root, 0o500)
				first := relinkMovedProject(r, false)
				os.Chmod(root, 0o700)
				if first != "" {
					t.Fatalf("\u524d\u63d0: 1 \u56de\u76ee\u306f\u5931\u6557\u3059\u308b\u306f\u305a: %q", first)
				}
			} else {
				// stopped after the first step of finishProjectInit (registerProject)
				if err := registerProject(r); err != nil {
					t.Fatal(err)
				}
			}
			second := relinkMovedProject(r, false)
			if act, _ := projectActive(root, r); !act {
				t.Fatalf("C8: \u9014\u4e2d\u3067\u5931\u6557\u3057\u305f\u3064\u306a\u304e\u76f4\u3057\u304c\u3001\u6b21\u306e\u4f9d\u983c\u3067\u63c3\u308f\u306a\u3044: %q; \u77e5\u3089\u305b: %q", second, copiedNotice(r))
			}
		})
	}
}

// 0.9.7 re-audit 1: on a case-insensitive disk (the macOS default), two sessions open the same folder by paths that differ only in case.
// Nothing was moved, yet every request reconnected it as moved and stopped the other path's page (C1, once).
func TestRe1CaseOnlyDifferentCwdFlipFlops(t *testing.T) {
	base := t.TempDir()
	p := filepath.Join(base, "Proj")
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	lower := filepath.Join(base, "proj")
	if _, err := os.Stat(lower); err != nil {
		t.Skip("\u5927\u6587\u5b57\u5c0f\u6587\u5b57\u3092\u533a\u5225\u3059\u308b\u30c7\u30a3\u30b9\u30af")
	}
	hookEnv(t, nil, p)
	told := 0
	for i := 0; i < 4; i++ {
		cwd := p
		if i%2 == 1 {
			cwd = lower
		}
		if n := relinkMovedProject(cwd, false); n != "" {
			told++
			t.Logf("%d \u56de\u76ee(%s): %.80s", i, filepath.Base(cwd), n)
		}
	}
	if told > 0 {
		t.Fatalf("C1: \u79fb\u3057\u3066\u3044\u306a\u3044\u306e\u306b\u3001\u5927\u6587\u5b57\u5c0f\u6587\u5b57\u3060\u3051\u9055\u3046\u30d1\u30b9\u306e\u4f9d\u983c\u3067 %d \u56de\u300c\u79fb\u3057\u305f\u300d\u3068\u3064\u306a\u304e\u76f4\u3057\u305f", told)
	}
}
