package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
	"github.com/hayaenfj/shikii/internal/store"
)

func reasoningHistoryPrepare(t *testing.T, root string, book notebook.Book) map[string]any {
	t.Helper()
	result, err := workCall(t, book, "prepare", map[string]any{"session_id": "reasoning", "turn_id": loadSession(root, "reasoning").TurnID, "query": "CSV"})
	if err != nil {
		t.Fatal("\u6e96\u5099\u306b\u5931\u6557", err)
	}
	return result
}

func reasoningHistoryArticle(t *testing.T, book notebook.Book, receipt any) string {
	t.Helper()
	source, err := book.CaptureObserved(notebook.Observation{Kind: "test", Title: "\u4e0d\u5909\u306e\u5408\u6210\u51fa\u5178", Content: "\u540c\u3058\u8a18\u4e8b\u7248\u3092\u4f7f\u3046\u3002", Reference: "\u5408\u6210\u691c\u67fb", Version: "test", ObservedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	body := "\u65e2\u5b58\u306e\u6761\u4ef6\u3092\u4fdd\u3064\u3002"
	note, err := book.Ingest(notebook.Draft{Title: "CSV\u6761\u4ef6", Lesson: "\u65e2\u5b58\u6761\u4ef6\u3092\u4fdd\u3064\u3002", Body: &body, AppliesTo: "CSV", Kind: "howto", Scene: "CSV\u691c\u67fb", Sources: []string{source.ID}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := workCall(t, book, "prepare", map[string]any{"receipt": receipt, "query": "lesson:" + note.Page.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range view["knowledge"].([]any) {
		item := loopObject(t, raw)
		if item["id"] == note.Page.ID {
			return item["ref"].(string)
		}
	}
	t.Fatal("\u8a18\u4e8b\u53c2\u7167\u304c\u306a\u3044")
	return ""
}

func TestReasoningCurrentSourceStableAfterPreparation(t *testing.T) {
	repo, root, book, prepared := reasoningFixture(t)
	saveReasoning(t, book, prepared["receipt"], reasoningInput("start", "current"), nil)
	finish := reasoningInput("finish", "current")
	saveReasoning(t, book, prepared["receipt"], finish, nil)
	previous := loadSession(root, "reasoning").Reasoning.Finish
	observed, err := workCall(t, book, "record", map[string]any{"receipt": prepared["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u5f8c\u306e\u8a3c\u62e0", "content": "\u5225\u306e\u6761\u4ef6\u304c\u672a\u78ba\u8a8d\u3002"}}})
	if err != nil || loopObject(t, observed["reasoning"])["complete"] != false {
		t.Fatal(observed, err)
	}
	next := reasoningHistoryPrepare(t, root, book)
	replay := saveReasoning(t, book, next["receipt"], finish, nil)
	current := loadSession(root, "reasoning").Reasoning.Finish
	if workHash(previous.Evidence) != workHash(current.Evidence) || previous.Generation != current.Generation || loopObject(t, replay["reasoning"])["complete"] != false {
		t.Fatalf("RH-002: \u518d\u6e96\u5099\u3057\u305fcurrent\u306e\u518d\u9001\u304c\u53e4\u3044\u7d42\u4e86\u4e16\u4ee3\u3092\u66f8\u304d\u63db\u3048\u305f: previous=%d current=%d complete=%v", previous.Generation, current.Generation, loopObject(t, replay["reasoning"])["complete"])
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "\u5408\u6210\u306e\u78ba\u8a8d\u7d50\u679c\u3067\u3059\u3002"})
	records, err := (store.Store{Dir: root}).Records("done-as-asked", 0)
	if err != nil || loadSession(root, "reasoning").Judged || len(records) != 0 || !strings.Contains(out, "reasoning-before-end") {
		t.Fatal("\u53e4\u3044\u7d42\u4e86\u3067Stop\u304c\u9032\u3093\u3060", out, err)
	}
}

func TestReasoningHistoryRetainsOlderFinishAcrossPreparation(t *testing.T) {
	repo, root, book, prepared := reasoningFixture(t)
	ref := reasoningHistoryArticle(t, book, prepared["receipt"])
	saveReasoning(t, book, prepared["receipt"], reasoningInput("start", ref), nil)
	finishA := reasoningInput("finish", ref)
	saveReasoning(t, book, prepared["receipt"], finishA, nil)
	original := loadSession(root, "reasoning").Reasoning.Finish
	finishB := reasoningInput("finish", ref)
	finishB["result"] = "\u5225\u306e\u7d50\u679c\u3092\u78ba\u8a8d\u3057\u305f\u3002"
	saveReasoning(t, book, prepared["receipt"], finishB, nil)
	saveReasoning(t, book, prepared["receipt"], reasoningInput("update", ref), map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "\u8ffd\u52a0\u6761\u4ef6", "content": "\u5225\u6761\u4ef6\u306f\u672a\u78ba\u8a8d\u3002"}}})
	next := reasoningHistoryPrepare(t, root, book)
	nextRef := ""
	state := loadSession(root, "reasoning")
	for alias, article := range state.Planning.Work.Articles {
		if article.ID == strings.TrimPrefix(strings.Split(original.Evidence[0], "@")[0], "article:") {
			nextRef = alias
		}
	}
	if nextRef == "" {
		t.Fatal("\u540c\u3058\u8a18\u4e8b\u306e\u518d\u6e96\u5099\u304c\u3067\u304d\u306a\u3044")
	}
	replayInput := reasoningInput("finish", nextRef)
	replay := saveReasoning(t, book, next["receipt"], replayInput, nil)
	if loopObject(t, replay["reasoning"])["complete"] != false {
		now := loadSession(root, "reasoning").Reasoning.Finish
		t.Fatalf("RH-003: \u540c\u3058\u8a3c\u62e0\u30fb\u30ad\u30fc\u306e\u53e4\u3044finish\u304c\u65b0\u4e16\u4ee3\u306b\u306a\u3063\u305f: same_key=%v original=%d now=%d", original.Key == now.Key, original.Generation, now.Generation)
	}
	out := runUnreviewedHook(t, "stop", map[string]any{"session_id": "reasoning", "cwd": repo, "last_assistant_message": "\u5408\u6210\u306e\u78ba\u8a8d\u7d50\u679c\u3067\u3059\u3002"})
	if loadSession(root, "reasoning").Judged || !strings.Contains(out, "reasoning-before-end") {
		t.Fatal(out)
	}
}

func TestReasoningHistoryOutlivesReceiptCapacity(t *testing.T) {
	_, root, book, prepared := reasoningFixture(t)
	ref := reasoningHistoryArticle(t, book, prepared["receipt"])
	saveReasoning(t, book, prepared["receipt"], reasoningInput("start", ref), nil)
	finish := reasoningInput("finish", ref)
	saveReasoning(t, book, prepared["receipt"], finish, nil)
	original := loadSession(root, "reasoning").Reasoning.Finish
	saveReasoning(t, book, prepared["receipt"], reasoningInput("update", ref), map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "\u66f4\u65b0\u306e\u51fa\u5178", "content": "\u66f4\u65b0\u3059\u308b\u6761\u4ef6\u3002"}}})
	for i := 0; i < workSavedLimit+2; i++ {
		if i%64 == 0 {
			prepared = reasoningHistoryPrepare(t, root, book)
			for alias, article := range loadSession(root, "reasoning").Planning.Work.Articles {
				if "article:"+article.ID+"@"+article.Revision == original.Evidence[0] {
					ref = alias
				}
			}
		}
		update := reasoningInput("update", ref)
		update["focus"] = fmt.Sprintf("\u5408\u6210\u306e\u66f4\u65b0%d", i)
		saveReasoning(t, book, prepared["receipt"], update, nil)
	}
	finish = reasoningInput("finish", ref)
	replay := saveReasoning(t, book, prepared["receipt"], finish, nil)
	if loopObject(t, replay["reasoning"])["complete"] != false {
		t.Fatal("RH-003: \u30bb\u30c3\u30b7\u30e7\u30f3\u306e\u4fdd\u5b58\u4ef6\u6570\u4e0a\u9650\u3092\u8d85\u3048\u305f\u5c65\u6b74\u304c\u6d88\u3048\u305f")
	}
}

func TestReasoningHistoryAliasesRetainTheFirstGeneration(t *testing.T) {
	_, root, book, prepared := reasoningFixture(t)
	saveReasoning(t, book, prepared["receipt"], reasoningInput("start", "current"), nil)
	finish := reasoningInput("finish", "current")
	saveReasoning(t, book, prepared["receipt"], finish, nil)
	original := loadSession(root, "reasoning").Reasoning.Finish
	later := reasoningInput("finish", "current")
	later["result"] = "\u5f8c\u306e\u7d42\u4e86\u5224\u65ad\u3002"
	saveReasoning(t, book, prepared["receipt"], later, nil)
	saveReasoning(t, book, prepared["receipt"], reasoningInput("update", "current"), map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "\u65b0\u3057\u3044\u5408\u6210\u8a3c\u62e0", "content": "\u5225\u6761\u4ef6\u304c\u672a\u78ba\u8a8d\u3002"}}})
	next := reasoningHistoryPrepare(t, root, book)
	alias := ""
	for ref, id := range loadSession(root, "reasoning").Planning.Work.Sources {
		if "source:"+id == original.Evidence[0] && ref != "current" {
			alias = ref
			break
		}
	}
	if alias == "" {
		t.Fatal("\u540c\u3058\u51fa\u5178\u306e\u5225\u53c2\u7167\u304c\u4f9b\u7d66\u3055\u308c\u306a\u3044")
	}
	replayInput := reasoningInput("finish", alias)
	replay := saveReasoning(t, book, next["receipt"], replayInput, nil)
	if loopObject(t, replay["reasoning"])["complete"] != false {
		t.Fatal("\u5225\u53c2\u7167\u540d\u3067\u53e4\u3044\u7d42\u4e86\u3092\u9069\u7528\u3057\u305f")
	}
	state := loadSession(root, "reasoning")
	stored, err := readReasoningHistory(book, state.Reasoning, original.Key)
	if err != nil || stored == nil || stored.Entry.Generation != original.Generation {
		t.Fatal("\u6700\u521d\u306e\u4e16\u4ee3\u304c\u5909\u308f\u3063\u305f", err)
	}
}

func TestReasoningHistorySaveInterruptionRecoversItsFirstEntry(t *testing.T) {
	for _, point := range []string{"before", "after", "after-with-evidence"} {
		t.Run(point, func(t *testing.T) {
			_, root, book, prepared := reasoningFixture(t)
			saveReasoning(t, book, prepared["receipt"], reasoningInput("start", "current"), nil)
			originalAfter, originalSync := workAfterWrite, store.SyncTemp
			t.Cleanup(func() { workAfterWrite = originalAfter; store.SyncTemp = originalSync })
			armed := true
			if point == "before" {
				store.SyncTemp = func(f *os.File) error {
					if armed && strings.HasPrefix(filepath.Base(f.Name()), ".reasoning-") {
						armed = false
						return errors.New("\u5408\u6210\u306e\u5c65\u6b74\u516c\u958b\u524d\u5931\u6557")
					}
					return originalSync(f)
				}
			} else {
				workAfterWrite = func(kind string) error {
					if armed && kind == "reasoning-history" {
						armed = false
						return errors.New("\u5408\u6210\u306e\u5c65\u6b74\u516c\u958b\u5f8c\u5931\u6557")
					}
					return originalAfter(kind)
				}
			}
			finish := reasoningInput("finish", "current")
			failed, err := workCall(t, book, "record", map[string]any{"receipt": prepared["receipt"], "reasoning": finish})
			workAfterWrite, store.SyncTemp = originalAfter, originalSync
			if err != nil || failed["complete"] != false || armed {
				t.Fatal("\u5c65\u6b74\u4fdd\u5b58\u306e\u4e2d\u65ad\u3092\u518d\u73fe\u3067\u304d\u306a\u3044", failed, err)
			}
			failedSource := ""
			for _, raw := range failed["errors"].([]any) {
				item := loopObject(t, raw)
				if item["kind"] == "reasoning" {
					failedSource, _ = item["id"].(string)
				}
			}
			if failedSource == "" {
				t.Fatal("\u7d42\u4e86\u539f\u8cc7\u6599\u306e\u4fdd\u5b58\u524d\u306b\u5931\u6557\u3057\u305f")
			}
			state := loadSession(root, "reasoning")
			key := "reasoning:" + workHash(workReasoning{Phase: "finish", Focus: finish["focus"].(string), Assessment: finish["assessment"].(string), Reason: finish["reason"].(string), Evidence: []string{"source:" + state.Reasoning.CurrentSource}, NextAction: finish["next_action"].(string), Result: finish["result"].(string)})
			before, err := readReasoningHistory(book, state.Reasoning, key)
			if err != nil {
				t.Fatal(err)
			}
			if point == "before" && before != nil {
				t.Fatal("\u516c\u958b\u524d\u306b\u5c65\u6b74\u304c\u898b\u3048\u308b")
			}
			if point != "before" && before == nil {
				t.Fatal("\u516c\u958b\u5f8c\u306e\u5c65\u6b74\u304c\u306a\u3044")
			}
			if point == "after-with-evidence" {
				saveReasoning(t, book, prepared["receipt"], reasoningInput("update", "current"), map[string]any{"observations": []any{map[string]any{"kind": "test", "title": "\u4e2d\u65ad\u5f8c\u306e\u8a3c\u62e0", "content": "\u65b0\u3057\u3044\u6761\u4ef6\u3002"}}})
			}
			replay := saveReasoning(t, book, prepared["receipt"], finish, nil)
			state = loadSession(root, "reasoning")
			after, err := readReasoningHistory(book, state.Reasoning, key)
			if err != nil || after == nil || after.Entry.Source != failedSource {
				t.Fatal("\u6700\u521d\u306e\u7d42\u4e86\u539f\u8cc7\u6599\u304c\u5909\u308f\u3063\u305f", err)
			}
			if before != nil && workHash(before) != workHash(after) {
				t.Fatal("\u4e2d\u65ad\u5f8c\u306e\u518d\u9001\u304c\u6700\u521d\u306e\u5c65\u6b74\u3092\u66f8\u304d\u63db\u3048\u305f")
			}
			shouldComplete := point != "after-with-evidence"
			if loopObject(t, replay["reasoning"])["complete"] != shouldComplete {
				t.Fatal("\u4e2d\u65ad\u306e\u56de\u5fa9\u304c\u65b0\u3057\u3044\u8a3c\u62e0\u3092\u8d8a\u3048\u305f", replay)
			}
		})
	}
}

func TestReasoningHistoryRejectsUnverifiableFiles(t *testing.T) {
	for _, broken := range []string{"link", "size", "owner"} {
		t.Run(broken, func(t *testing.T) {
			_, root, book, prepared := reasoningFixture(t)
			saveReasoning(t, book, prepared["receipt"], reasoningInput("start", "current"), nil)
			finish := reasoningInput("finish", "current")
			saveReasoning(t, book, prepared["receipt"], finish, nil)
			original := loadSession(root, "reasoning").Reasoning.Finish
			_, err := workCall(t, book, "record", map[string]any{"receipt": prepared["receipt"], "observations": []any{map[string]any{"kind": "test", "title": "\u8ffd\u52a0\u306e\u691c\u67fb\u6761\u4ef6", "content": "\u5225\u6761\u4ef6\u3002"}}})
			if err != nil {
				t.Fatal(err)
			}
			identity, name, err := reasoningHistoryIdentity(book, loadSession(root, "reasoning").Reasoning, original.Key)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "records", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			backup := path + ".backup"
			if err = os.Rename(path, backup); err != nil {
				t.Fatal(err)
			}
			switch broken {
			case "link":
				err = os.Symlink(filepath.Base(backup), path)
			case "size":
				err = os.WriteFile(path, append(append([]byte{}, data...), []byte(strings.Repeat(" ", reasoningHistoryLimit+1-len(data)))...), 0600)
			case "owner":
				var record reasoningHistoryRecord
				if err = json.Unmarshal(data, &record); err == nil {
					record.Session = identity.Session + "-other"
					data, err = json.Marshal(record)
					if err == nil {
						err = os.WriteFile(path, data, 0600)
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := workCall(t, book, "record", map[string]any{"receipt": prepared["receipt"], "reasoning": finish})
			if err == nil && result["complete"] == true {
				t.Fatal("\u691c\u8a3c\u3067\u304d\u306a\u3044\u5c65\u6b74\u3067\u518d\u9001\u3092\u53d7\u7406\u3057\u305f", broken)
			}
			if workHash(loadSession(root, "reasoning").Reasoning.Finish) != workHash(original) {
				t.Fatal("\u4e0d\u6b63\u306a\u5c65\u6b74\u3067\u7d42\u4e86\u304c\u5909\u308f\u3063\u305f")
			}
		})
	}
}

func TestReasoningHistorySeparatesSessionTurnAndStore(t *testing.T) {
	_, _, book, prepared := reasoningFixture(t)
	saveReasoning(t, book, prepared["receipt"], reasoningInput("start", "current"), nil)
	token, err := workDecode(prepared["receipt"].(string))
	if err != nil {
		t.Fatal(err)
	}
	first := &reasoningProgress{SessionID: token.Session, TurnID: token.Turn}
	key := "reasoning:" + strings.Repeat("a", 64)
	_, firstName, err := reasoningHistoryIdentity(book, first, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range []*reasoningProgress{{SessionID: first.SessionID + "-other", TurnID: first.TurnID}, {SessionID: first.SessionID, TurnID: first.TurnID + "-other"}} {
		_, name, err := reasoningHistoryIdentity(book, other, key)
		if err != nil || name == firstName {
			t.Fatal("\u5225\u306e\u30bb\u30c3\u30b7\u30e7\u30f3\u304b\u30bf\u30fc\u30f3\u306e\u5c65\u6b74\u540d\u304c\u885d\u7a81", err)
		}
	}
	otherBook, _ := loopBook(t)
	_, otherName, err := reasoningHistoryIdentity(otherBook, first, key)
	if err != nil || otherName == firstName {
		t.Fatal("\u5225\u30b9\u30c8\u30a2\u306e\u5c65\u6b74\u540d\u304c\u885d\u7a81", err)
	}
}
