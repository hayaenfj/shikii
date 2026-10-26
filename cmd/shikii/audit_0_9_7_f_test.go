package main

import "testing"

// 0.9.7 audit F-1: the allowlist of lock callers only looked one step up. A listed handler called from elsewhere with another writer (a
// recorder, nil) waited for the lock without extending the waiting reply's deadline, and the general check before it had caught that.
// F-3: a new helper between a handler and the lock, listed as a caller, was itself called from anywhere. Every function that waits for the
// lock is a key of lockCallers, so its own callers are bound too; a handler is called only through the HTTP registration.
func TestReauditFLockCallersAreBoundAllTheWayUp(t *testing.T) {
	head := "package main\nimport (\"net/http\"; \"net/http/httptest\")\nvar _ = httptest.NewRecorder\n"
	for name, body := range map[string]string{
		"a listed handler called with nil":        "func (s *server) handleOther(w http.ResponseWriter, r *http.Request) { s.handleLabel(nil, r) }\n",
		"a listed handler called with a recorder": "func (s *server) handleBatch(w http.ResponseWriter, r *http.Request) { rec := httptest.NewRecorder(); s.handleTuneSync(rec, r) }\n",
		"a listed handler called with its writer": "func (s *server) handleBatch(w http.ResponseWriter, r *http.Request) { s.handleTuneSync(w, r) }\n",
	} {
		if got := nilWriters(t, map[string]string{"bad.go": head + body}); len(got) == 0 {
			t.Errorf("%s: not found", name)
		}
	}
	// every caller listed with its own writer ("w") waits for the lock too, so it is a key of the table (its callers are bound)
	for callee, callers := range lockCallers {
		for caller, how := range callers {
			if how == "w" && lockCallers[caller] == nil {
				t.Errorf("%s calls %s with its writer but is not a key of lockCallers: its own callers are not bound", caller, callee)
			}
		}
	}
}
