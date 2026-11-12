package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// The notice before a request and the check at Stop look at pending sources through the same window: the two oldest in capture
// order. The notice never names a third, and a named source that is still pending never leaves the window (later captures queue
// behind it), so the Stop check cannot lose a source the notice named. The third waits for the next notice.
func TestStopCheckSeesEveryObservationTheNoticeNamed(t *testing.T) {
	repo := gitRepo(t)
	root := hookEnv(t, nil, repo)
	_, scope, err := projectStoreAt(repo)
	if err != nil {
		t.Fatal(err)
	}
	book := notebook.Book{Root: root, Scope: scope}
	capture := func(i int) string {
		t.Helper()
		o, err := book.CaptureObserved(notebook.Observation{Kind: "test", Title: fmt.Sprintf("Check %d", i), Content: fmt.Sprintf("synthetic check %d passed", i), Reference: "synthetic", Version: "1", ObservedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		return o.ID
	}
	first, second, third := capture(1), capture(2), capture(3)

	out := runHook(t, "user-prompt-submit", map[string]any{"session_id": "w", "cwd": repo, "prompt": "Update the quick start section of the guide."})
	asked := loadSession(root, "w").ReflectAsked
	t.Logf("three pending observations %v; the notice named %v", []string{first, second, third}, asked)
	if fmt.Sprint(asked) != fmt.Sprint([]string{first, second}) {
		t.Fatalf("the notice names the two oldest pending observations and no third: %v", asked)
	}
	if ctx := feedbackContext(t, out); !strings.Contains(ctx, first) || !strings.Contains(ctx, second) || strings.Contains(ctx, third) {
		t.Fatalf("the notice's text names the same two: %s", ctx)
	}
	leftIDs := func() []string {
		t.Helper()
		left, err := reflectLeft(root, scope, asked)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, l := range left {
			ids = append(ids, l.id)
		}
		return ids
	}
	if got := leftIDs(); fmt.Sprint(got) != fmt.Sprint([]string{first, second}) {
		t.Fatalf("both named observations are left: %v", got)
	}
	// a source captured during the turn queues behind the named ones: it does not push a named one out of the window
	fourth := capture(4)
	if got := leftIDs(); fmt.Sprint(got) != fmt.Sprint([]string{first, second}) {
		t.Fatalf("after a later capture both named observations are still left: %v", got)
	}
	submit := func(id string) {
		t.Helper()
		result, err := runReflect(root, scope, reflectRequest{Operation: "submit", Wiki: []notebook.Draft{{Sources: []string{id}}}})
		if saved, ok := result.(reflectResult); err != nil || !ok || !saved.Complete {
			t.Fatalf("sorting %s with sources only: %+v %v", id, result, err)
		}
	}
	submit(first)
	if got := leftIDs(); fmt.Sprint(got) != fmt.Sprint([]string{second}) {
		t.Fatalf("after sorting the first, the second named observation is still left: %v", got)
	}
	submit(second)
	if got := leftIDs(); len(got) != 0 {
		t.Fatalf("after sorting both named observations nothing asked is left: %v", got)
	}
	// the ones the notice did not name wait for the next notice
	_, next, err := reflectContext(root, scope)
	t.Logf("after both are sorted, left of the asked ones: none; the next notice names %v", next)
	if err != nil || fmt.Sprint(next) != fmt.Sprint([]string{third, fourth}) {
		t.Fatalf("the next notice names the next two pending observations: %v %v", next, err)
	}
}
