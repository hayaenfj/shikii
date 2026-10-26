package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// re3-2 (F4, new by 3509108): the short reflection form lists correction_result as a field of every memory item, next to the values of
// result, without "for a delivered correction". An agent that only has the short form (after compaction) and fills every field it lists
// sends correction_result for an ordinary lesson; that item is refused. The full text qualifies it.
func TestRe3ShortFormCorrectionResultOnlyForCorrections(t *testing.T) {
	st, scope, h, draft, revision := memoryEffectFixture(t)
	memoryEffectPrompt(t, scope, "cr")
	id := memoryEffectComplete(t, st, scope, "cr")
	key := "lesson:" + draft.ID + ":" + revision
	submit := func(withCR bool) map[string]any {
		item := map[string]string{"source_id": id, "key": key, "result": "unknown", "decision": "", "evidence": "\u4e0d\u660e"}
		if withCR {
			item["correction_result"] = "unknown" // the short form lists the field with no condition
		}
		return reflectCall(t, h, map[string]any{"operation": "submit", "memory": []map[string]string{item}})
	}
	short := reflectAskShort("X reflect", []string{id})
	qualified := strings.Contains(short, "delivered correction")
	refused := submit(true)
	t.Logf("with correction_result: %v", refused)
	if refused["complete"] == true {
		t.Skip("an ordinary lesson accepts correction_result: no trap")
	}
	if ok := submit(false); ok["complete"] != true {
		t.Fatalf("control: without the field the same item is saved: %v", ok)
	}
	if !qualified {
		t.Fatalf("F4: the short form lists correction_result for every memory item, but an ordinary lesson refuses it (%v); the full text says it is for a delivered correction\nshort: %q", refused["errors"], short)
	}
}

// re3-3 (F4): the short organizing notice points to the details as the full text does (the skill's references/planning.md); a bare
// references/planning.md reads as a path in the project.
func TestRe3ShortPlanningNoticeNamesTheSkillsReference(t *testing.T) {
	full, short := planningNotice("s", "t"), planningNoticeShort("s", "t")
	const where = "the skill's references/planning.md"
	if !strings.Contains(full, where) {
		t.Fatalf("precondition: the full text says %q", where)
	}
	if !strings.Contains(short, where) {
		t.Fatalf("F4: the short form names the details differently from the full text: %q", short)
	}
	if !strings.Contains(reflectAskShort("X", nil), "the skill's references/wiki.md") {
		t.Fatal("control: the short reflection form names the skill's file")
	}
}

// re3-4 (C5, wording): the other-path notice says when to ask the person as the skill's wiki.md does: copy the missing files, keep the
// old store, ask before deleting it. The notice says to copy when the request is done and also to check with the person first.
func TestRe3OtherPathNoticeAsksWhenTheSkillDoes(t *testing.T) {
	b, err := os.ReadFile("../../skills/shikii/references/wiki.md")
	if err != nil {
		t.Fatal(err)
	}
	skill := string(b)
	if !strings.Contains(skill, "copy the missing files, keep the other store, and ask the person before deleting it") {
		t.Fatalf("precondition: wiki.md says to ask before deleting")
	}
	if strings.Contains(movedHow, "check with the person first") {
		t.Fatalf("C5: the notice asks the person before copying, wiki.md only before deleting: %q", movedHow)
	}
}

// re3-5 (docs, redone with the index keyed by the store's id): an older shikii rewrote the old path's entry in the shape 0.9.6 writes (no
// store id). The store's own entry is named by its id and is untouched, so the hook still reconnects the move by itself, once, and the old
// path's entry is removed.
func TestRe3InitAfterOldShikiiRewroteTheEntry(t *testing.T) {
	guides := map[string]string{}
	for _, guide := range []string{"../../docs/guide.md", "../../docs/guide.ja.md"} {
		b, err := os.ReadFile(guide) // before movedProject changes the working folder
		if err != nil {
			t.Fatal(err)
		}
		guides[guide] = string(b)
	}
	common, old, moved := movedProject(t)
	legacy, _ := json.Marshal(map[string]any{"id": projectID(old), "name": filepath.Base(old), "path": old, "url": "/p/" + projectID(old) + "/"})
	if err := os.WriteFile(filepath.Join(common, "projects", projectID(old)+".json"), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	before := storeID(filepath.Join(moved, ".shikii"), false)
	if n := relinkMovedProject(moved, false); !strings.Contains(n, "moved or renamed") {
		t.Fatalf("the hook reconnects it: %q", n)
	}
	if n := relinkMovedProject(moved, false); n != "" {
		t.Fatalf("and says so once: %q", n)
	}
	if after := storeID(filepath.Join(moved, ".shikii"), false); after != before {
		t.Fatalf("the store keeps its id: %s -> %s", before, after)
	}
	for _, p := range registeredPaths(common) {
		if p == old {
			t.Fatalf("the old path's entry is removed: %v", registeredPaths(common))
		}
	}
	for guide, b := range guides {
		if strings.Contains(b, "`shikii --init` reconnects it") || strings.Contains(b, "`shikii --init` \u3067\u3064\u306a\u304e\u76f4\u305b\u307e\u3059") || strings.Contains(b, "fixed names") {
			t.Fatalf("%s describes the old design", guide)
		}
	}
}
