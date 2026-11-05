package main

import (
	"encoding/json"
	"fmt"
	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReasoningCapacityRetainsPayloadWhenArchiveFails(t *testing.T) {
	for _, kind := range []string{"note", "capture"} {
		t.Run(kind, func(t *testing.T) {
			_, root, b, p := reasoningFixture(t)
			saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
			saveReasoning(t, b, p["receipt"], reasoningInput("finish", "current"), nil)
			interimReadableBoundary(t, root, b)
			queue := filepath.Join(root, "records", "wiki-sources", store.StoreName(root, b.Scope), ".captured.json")
			original, err := os.ReadFile(queue)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.WriteFile(queue, original, 0600) })
			if err = os.WriteFile(queue, []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
			input := map[string]any{"receipt": p["receipt"]}
			note := map[string]any{"title": "Required note after archive failure", "lesson": "Keep the original condition.", "body": strings.Repeat("c", 4096), "applies_to": "synthetic condition", "kind": "howto", "scene": "synthetic recovery", "sources": []string{"current"}}
			observation := map[string]any{"kind": "test", "title": "Capture after archive failure", "content": strings.Repeat("d", 4096)}
			if kind == "note" {
				input["notes"] = []any{note}
			} else {
				input["observations"] = []any{observation}
			}
			failed, recordErr := workCall(t, b, "record", input)
			if recordErr == nil && failed["complete"] == true {
				t.Fatal("the combined storage failure was not reached")
			}
			if err = os.WriteFile(queue, original, 0600); err != nil {
				t.Fatal(err)
			}
			state := loadSession(root, "reasoning")
			if state.Planning.Work != nil || reasoningView(state.Reasoning).Complete {
				t.Fatal("capacity and archive failure erased the obligation")
			}
			ref := ""
			if kind == "note" {
				for key, entry := range state.Reasoning.RequiredNotes {
					ref = key
					if entry.Inline == nil || entry.Source != "" {
						t.Fatal("the original note payload was not retained")
					}
				}
				if ref == "" {
					t.Fatal("required note is missing")
				}
			} else {
				if len(state.Reasoning.PendingCaptures) != 1 {
					t.Fatal("capture is missing")
				}
				for _, entry := range state.Reasoning.PendingCaptures {
					if entry.Original == nil {
						t.Fatal("the original capture payload was not retained")
					}
				}
			}
			next, err := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": state.TurnID, "query": "CSV"})
			if err != nil {
				t.Fatal(err)
			}
			retry := map[string]any{"receipt": next["receipt"]}
			if kind == "note" {
				retry["notes"] = []any{map[string]any{"ref": ref, "target": "new"}}
			} else {
				retry["observations"] = []any{observation}
			}
			recovered, err := workCall(t, b, "record", retry)
			if err != nil || recovered["complete"] != true {
				t.Fatal("the original payload could not recover", recovered, err)
			}
			end := reasoningInput("finish", "current")
			end["result"] = "The original payload was saved after storage recovered."
			final := saveReasoning(t, b, next["receipt"], end, nil)
			if loopObject(t, final["reasoning"])["complete"] != true {
				t.Fatal(final)
			}
		})
	}
}

// This readable synthetic boundary models retained successful capture caches.
// It does not claim the new compact writer grows this way through public calls.
func interimReadableBoundary(t *testing.T, root string, book notebook.Book) {
	t.Helper()
	s := loadSession(root, "reasoning")
	w := s.Planning.Work
	originals := make([]workObservation, 18)
	at := time.Unix(100, 0).UTC()
	for i := range originals {
		originals[i] = workObservation{Kind: "test", Title: fmt.Sprintf("Synthetic old cache %d", i), Content: "", Reference: "synthetic boundary", Version: "synthetic"}
		ref := fmt.Sprintf("observed%d", 100+i)
		w.Captures[workHash(originals[i])] = workCapture{Original: &originals[i], ID: "0000000000000001", Ref: ref, At: at, Version: "synthetic"}
		w.Sources[ref] = "0000000000000001"
	}
	raw, e := encodeHookSession(s)
	if e != nil {
		t.Fatal("boundary fixture unavailable")
	}
	remaining := hookSessionRecordLimit - len(raw) - 128
	for i := range originals {
		oldkey := workHash(originals[i])
		memo := w.Captures[oldkey]
		delete(w.Captures, oldkey)
		length := remaining
		if length > 60000 {
			length = 60000
		}
		remaining -= length
		originals[i].Content = strings.Repeat("x", length)
		o := originals[i]
		captured, e := book.Capture(notebook.Observation{Kind: o.Kind, Title: o.Title, Content: o.Content, Reference: o.Reference, Version: o.Version, ObservedAt: at})
		if e != nil {
			t.Fatal("cache source fixture failed")
		}
		if _, e = book.Ingest(notebook.Draft{Sources: []string{captured.ID}}); e != nil {
			t.Fatal("cache review fixture failed")
		}
		memo.ID = captured.ID
		w.Captures[workHash(o)] = memo
		w.Sources[memo.Ref] = captured.ID
	}
	if remaining != 0 {
		t.Fatal("cache boundary was not filled")
	}
	if e = saveSession(root, "reasoning", s); e != nil {
		t.Fatal("readable boundary fixture failed")
	}
	if _, e = readSession(root, "reasoning"); e != nil {
		t.Fatal("boundary fixture is unreadable")
	}
}
func TestReasoningNoteCapacityRecovery(t *testing.T) {
	repo, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	saveReasoning(t, b, p["receipt"], reasoningInput("finish", "current"), nil)
	interimReadableBoundary(t, root, b)
	_, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "notes": []any{map[string]any{"title": "Required synthetic capacity note", "lesson": "Keep the owner condition.", "body": strings.Repeat("a", 4096), "applies_to": "owner export", "kind": "howto", "scene": "synthetic review", "sources": []string{"current"}}}})
	if e == nil {
		t.Fatal("reservation did not reach capacity")
	}
	state := loadSession(root, "reasoning")
	if state.Planning.Work != nil || len(state.Reasoning.RequiredNotes) != 1 || reasoningView(state.Reasoning).Complete {
		t.Fatal("required state or receipt fallback is incorrect")
	}
	ref, source := "", ""
	for key, n := range state.Reasoning.RequiredNotes {
		ref, source = key, n.Source
	}
	archive, e := b.ReadSources([]string{source})
	raw, _ := json.Marshal(archive)
	if e != nil || !strings.Contains(string(raw), "Required synthetic capacity note") {
		t.Fatal("original note missing from archive")
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "Synthetic work still needs recovery."})
	if loadSession(root, "reasoning").Judged || (!strings.Contains(out, "\"block\"") && !strings.Contains(out, "\"continue\":false")) {
		t.Fatal("pending capacity work was not stopped")
	}
	next, e := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": state.TurnID, "query": "CSV conditions"})
	if e != nil {
		t.Fatal("capacity preparation failed")
	}
	fixed, e := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "notes": []any{map[string]any{"ref": ref, "target": "new"}}})
	if e != nil || fixed["complete"] != true {
		t.Fatal("archived required ref could not restore original note")
	}
	end := reasoningInput("finish", "current")
	end["result"] = "The original capacity note was saved and checked."
	final := saveReasoning(t, b, next["receipt"], end, nil)
	if loopObject(t, final["reasoning"])["complete"] != true {
		t.Fatal("capacity recovery did not complete")
	}
	t.Log("readable boundary -> reservation refused -> old receipt retired -> archive and obligation retained -> Stop blocked -> prepare -> original ref saved -> finish complete")
}
func TestReasoningCaptureCapacityRecovery(t *testing.T) {
	_, root, b, p := reasoningFixture(t)
	saveReasoning(t, b, p["receipt"], reasoningInput("start", "current"), nil)
	saveReasoning(t, b, p["receipt"], reasoningInput("finish", "current"), nil)
	interimReadableBoundary(t, root, b)
	obs := map[string]any{"kind": "test", "title": "Synthetic capacity capture", "content": strings.Repeat("b", 4096)}
	failed, e := workCall(t, b, "record", map[string]any{"receipt": p["receipt"], "observations": []any{obs}})
	if e != nil || failed["complete"] != false {
		t.Fatal("capture did not reach capacity")
	}
	state := loadSession(root, "reasoning")
	if state.Planning.Work != nil || len(state.Reasoning.PendingCaptures) != 1 || reasoningView(state.Reasoning).Complete {
		t.Fatal("capture obligation lost")
	}
	next, e := workCall(t, b, "prepare", map[string]any{"session_id": "reasoning", "turn_id": state.TurnID, "query": "CSV conditions"})
	if e != nil {
		t.Fatal("capture preparation failed")
	}
	recovered, e := workCall(t, b, "record", map[string]any{"receipt": next["receipt"], "observations": []any{obs}})
	if e != nil || recovered["complete"] != true {
		t.Fatal("archive capture retry failed")
	}
	end := reasoningInput("finish", "current")
	end["result"] = "The capacity capture was restored and checked."
	final := saveReasoning(t, b, next["receipt"], end, nil)
	if loopObject(t, final["reasoning"])["complete"] != true {
		t.Fatal("capture recovery did not finish")
	}
	t.Log("capture capacity -> archive -> prepare -> same observation retry -> finish complete")
}
