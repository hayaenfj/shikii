package notebook

import (
	"fmt"
	"sync"
	"testing"

	"github.com/hayaenfj/shikii/internal/store"
)

// classify と同時に別のエージェントが教訓を直すと、classify が読んだ古い教訓で上書きし、直した教訓が黙って戻る。
func TestAudit090ClassifyDoesNotRevertConcurrentEdit(t *testing.T) {
	root := t.TempDir()
	scope := "/audit/race"
	if err := (store.Store{Dir: root}).AppendFeedback(scope, store.Feedback{ID: "00000000000003e7", Request: "r", Summary: "s", Reply: "x"}); err != nil {
		t.Fatal(err)
	}
	b := Book{Root: root, Scope: scope}
	// 記事を多めに置き、Live の読みに時間がかかる実際の wiki に近づける
	for i := 0; i < 40; i++ {
		if _, err := b.Ingest(Draft{Sources: []string{"00000000000003e7"}, Title: fmt.Sprintf("他の記事 %d", i), Lesson: "教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"}); err != nil {
			t.Fatal(err)
		}
	}
	lost := 0
	for i := 0; i < 60; i++ {
		res, err := b.Ingest(Draft{Sources: []string{"00000000000003e7"}, Title: fmt.Sprintf("競う記事 %d", i), Lesson: "古い教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
		if err != nil {
			t.Fatal(err)
		}
		id := res.Page.ID
		if err := b.RewriteWithoutKindScene(id); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var editErr error
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = b.Classify(id, "fact", "作業する時") }()
		go func() {
			defer wg.Done()
			_, editErr = b.Ingest(Draft{ID: id, Sources: []string{"00000000000003e7"}, Title: fmt.Sprintf("競う記事 %d", i), Lesson: "直した教訓。", AppliesTo: "a", Kind: "rule", Scene: "作業する時"})
		}()
		wg.Wait()
		live, _ := b.Live()
		for _, p := range live {
			if p.ID == id && editErr == nil && p.Lesson == "古い教訓。" {
				lost++
			}
		}
	}
	if lost > 0 {
		t.Fatalf("60 回中 %d 回、保存に成功した教訓の直しが classify で古い教訓に戻った", lost)
	}
}
