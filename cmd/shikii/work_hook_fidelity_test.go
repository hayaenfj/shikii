package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

var (
	fidelityHookSizes     = []int{3034, 4000, 5002, 6519, 7500, 9000, 12000}
	fidelityCLISizes      = []int{24805, 25200, 25624}
	fidelityExplicitSizes = []int{4000, 5002, 6027, 6437}
)

func fidelityGolden(t *testing.T) map[string]fidelityRecord {
	t.Helper()
	raw, err := os.ReadFile("testdata/work_hook_fidelity_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	golden := map[string]fidelityRecord{}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	if want := len(fidelityHookSizes) + len(fidelityCLISizes) + len(fidelityExplicitSizes); len(golden) != want {
		t.Fatalf("the golden file holds %d deliveries, want %d", len(golden), want)
	}
	return golden
}

func sameDelivery(t *testing.T, key, got string, golden map[string]fidelityRecord) {
	t.Helper()
	have, want := fidelitySummary(got), golden[key]
	if have.Hash != want.Hash {
		t.Fatalf("%s is not what the code before the last fields of the bound delivered (a field it delivered is missing or changed):\nbefore: %s\n after: %s", key, strings.Join(want.Summary, "\n    "), strings.Join(have.Summary, "\n    "))
	}
}

// A request that the hook delivered before (whole, or as its head) is delivered the same after: the last fields of the bound are the
// hook's third try, after its first (the whole request) and second (its head) were refused, so a request whose head fits is never
// traded for a reply without its lessons' text, attention, pending and observations. The golden file is what befc20f4 delivered for
// the same requests (about 5,000 bytes was the size at which the head fitted and the whole request did not once the last fields
// were left out first).
func TestHookDeliversWhatItDeliveredBeforeTheLastFieldsOfTheBound(t *testing.T) {
	golden := fidelityGolden(t)
	for _, n := range fidelityHookSizes {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			got := fidelityHookReply(t, n)
			sameDelivery(t, fmt.Sprintf("hook@%d", n), got, golden)
			if n != 5002 {
				return
			}
			// spelled out for the size that went wrong: the head of the request, and each lesson with its summary, conditions and
			// orientation, with the attention, pending and observations beside them
			var reply map[string]any
			if err := json.Unmarshal([]byte(got), &reply); err != nil {
				t.Fatal(err)
			}
			context, _ := reply["task_context"].(map[string]any)
			entries, _ := reply["knowledge"].([]any)
			if len(entries) != 2 || context["text_status"] != "delivered_head" || reply["attention"] == nil || reply["pending"] == nil || reply["observations"] == nil {
				t.Fatalf("the hook delivers the head of the request, two lessons and the fields beside them: %v", reply)
			}
			for _, raw := range entries {
				e := raw.(map[string]any)
				for _, key := range []string{"summary", "applies_to", "rationale", "orientation", "title", "query", "reading_notice"} {
					if e[key] == nil {
						t.Errorf("a lesson lost its %s: %v", key, e)
					}
				}
			}
			if output, ok := reply["output"].(map[string]any); ok && strings.Contains(fmt.Sprint(output["omitted"]), "knowledge[]") {
				t.Errorf("nothing of the lessons is left out: %v", output["omitted"])
			}
		})
	}
}

// The CLI's prepare without max_bytes is refused at 28 KiB for a request of about 25,000 bytes, and widens to 64 KiB: the reply holds
// each article whole and leaves nothing out. The last fields of the bound are the hook's, never the CLI's.
func TestCLIPrepareWithoutMaxBytesWidensAsBefore(t *testing.T) {
	golden := fidelityGolden(t)
	for _, n := range fidelityCLISizes {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			got := fidelityCLIReply(t, n)
			sameDelivery(t, fmt.Sprintf("cli@%d", n), got, golden)
			var reply map[string]any
			if err := json.Unmarshal([]byte(got), &reply); err != nil {
				t.Fatal(err)
			}
			entries, _ := reply["knowledge"].([]any)
			if len(entries) != 2 || reply["output"] != nil || entries[0].(map[string]any)["text"] == nil && entries[0].(map[string]any)["article"] == nil {
				t.Fatalf("the widened reply has both articles with their text and no output: %v", reply["output"])
			}
		})
	}
}

// An explicit read under a named bound that the fixed fields nearly fill is refused with the words it always was, naming the larger
// max_bytes: the caller's next step is in the sentence. The reply is not an article with only its reference.
func TestExplicitReadUnderASmallBoundIsRefusedAsBefore(t *testing.T) {
	golden := fidelityGolden(t)
	for _, n := range fidelityExplicitSizes {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			got := fidelityExplicitReply(t, n)
			sameDelivery(t, fmt.Sprintf("explicit@%d", n), got, golden)
			if n == 6437 && !strings.HasPrefix(got, "refused: the task and reference inventory exceed max_bytes (8192); prepare again with a larger max_bytes (up to 393216)") {
				t.Fatalf("a named bound that does not fit is refused with the sentence that names the larger max_bytes: %.200s", got)
			}
		})
	}
}
