package notebook

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func upgradeRegressionView(t *testing.T, b Book) (map[string]any, bool) {
	t.Helper()
	fn := reflect.ValueOf(b).MethodByName("MemoryUpgrade")
	if !fn.IsValid() {
		return nil, false
	}
	vals := fn.Call(nil)
	if !vals[1].IsNil() {
		t.Fatal("upgrade setup", vals[1].Interface())
	}
	raw, e := json.Marshal(vals[0].Interface())
	if e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatal(e)
	}
	return out, true
}

func TestMemoryUpgradeReplacementRemainsReviewable(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(fmt.Sprint(replace), func(t *testing.T) {
			b := Book{Root: t.TempDir(), Scope: "/synthetic-r3-project"}
			source := seed(t, b, 1)
			old, e := b.Ingest(draft(source, "\u5408\u6210\u306e\u65e2\u5b58\u624b\u9806"))
			if e != nil {
				t.Fatal("setup", e)
			}
			u, ok := upgradeRegressionView(t, b)
			if !ok {
				t.Log("baseline: MemoryUpgrade feature absent")
				return
			}
			tasks, ok := u["tasks"].([]any)
			if !ok || len(tasks) != 1 {
				t.Fatal("setup inventory", u)
			}
			task := tasks[0].(map[string]any)
			if replace {
				d := draft(seed(t, b, 2), "\u5408\u6210\u306e\u7f6e\u63db\u5f8c\u306e\u624b\u9806")
				d.Sources = append(d.Sources, source)
				d.Replaces = []string{old.Page.ID}
				if _, e = b.Ingest(d); e != nil {
					t.Fatal("setup replacement", e)
				}
			} else {
				_, e = b.Maintain(MaintenanceReview{PageID: task["page_id"].(string), Revision: task["revision"].(string), Key: task["key"].(string), Reason: "\u5408\u6210\u306e\u539f\u8cc7\u6599\u3068\u73fe\u5728\u306e\u624b\u9806\u3092\u78ba\u8a8d\u3057\u3066\u4fdd\u6301\u3059\u308b\u3002"})
				if e != nil {
					t.Fatal(e)
				}
			}
			u, _ = upgradeRegressionView(t, Book{Root: b.Root, Scope: b.Scope})
			tasks, _ = u["tasks"].([]any)
			blocked, _ := u["unavailable_sources"].([]any)
			t.Logf("replace=%v status=%v total=%v reviewed=%v tasks=%d unavailable=%d", replace, u["status"], u["total"], u["reviewed"], len(tasks), len(blocked))
			if replace {
				_, err := b.Maintain(MaintenanceReview{PageID: task["page_id"].(string), Revision: task["revision"].(string), Key: task["key"].(string), Reason: "\u5408\u6210\u306e\u7f6e\u63db\u524d\u539f\u8cc7\u6599\u3068\u65b0\u3057\u3044\u624b\u9806\u3092\u78ba\u8a8d\u3057\u305f\u3002"})
				t.Logf("original target maintain error=%v", err)
				keys, e := b.MaintenanceKeys()
				if e != nil {
					t.Fatal(e)
				}
				_, recoverable := keys[old.Page.ID]
				t.Logf("original target has current maintenance key=%v", recoverable)
				if recoverable {
					latest := keys[old.Page.ID]
					if _, e := b.Maintain(MaintenanceReview{PageID: latest.PageID, Revision: latest.Revision, Key: latest.Key, Reason: "Checked original sources and replacement conditions."}); e != nil {
						t.Fatal(e)
					}
					final, _ := upgradeRegressionView(t, b)
					if final["status"] != "complete" || final["reviewed"] != float64(1) {
						t.Fatal("the frozen target must complete after its current review", final)
					}
				}

				if u["status"] == "upgrading" && len(tasks) == 0 && len(blocked) == 0 && !recoverable {
					t.Error("upgrade is unfinished with no actionable task or missing-source diagnostic after ordinary source-preserving replacement")
				}
			} else if u["status"] != "complete" || u["reviewed"] != float64(1) {
				t.Fatal("unchanged control cannot complete", u)
			}
		})
	}
}
