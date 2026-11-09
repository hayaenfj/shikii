package main

import (
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/hayaenfj/shikii/internal/systemone"
)

// shikii serve: in one process, serves the HTTP endpoints for programs and the page for people.
//
//	POST /v1/decide  {"spec":"id","inputs":{...},"tainted":false}   → Decision (same shape as decide --json). Recorded
//	POST /v1/label   {"trace_id":"…","truth":"…","weight":1,"source":"ui"} → the label given. Once min_n is reached it calibrates automatically and writes back
//	GET  /v1/queue?spec=id&limit=50                                 → records without a truth (ask band first). The text is attached if it is still kept
//	GET  /v1/status                                                 → per-spec state of records, truths and (model, hash)
//	GET  /v1/specs                                                  → the specs loaded
//	GET  /                                                          → the page (the Svelte page with web/dist embedded; no external loads)
//
// Specs are reread from the folder every time, so editing a file takes effect at once. It does no inference itself (that is left to --local / --hosted).

type serveOpts struct {
	projectScope  string // the work that started the process. Do not mix wiki sources with other work
	port          int
	specsDir      string
	storeDir      string
	ef            endpointFlags
	autoCalibrate bool
	wikiDir       string // the wording wiki. Empty means ./wiki. Holds tune's results, read by the page and MCP
	proposer      string // URL of the proposer (a chat LLM) that drafts candidates. Empty means only the spec's variants
	proposerModel string
}

// bindHost is where to listen. On a host, only 127.0.0.1 (never exposed to the LAN). Inside a container (Docker, where /.dockerenv exists) it listens on every interface,
// and whether it is exposed is decided by the host's port publishing (compose limits it to 127.0.0.1:8095). There is no setting to choose.
func bindHost() string { return systemone.BindHost() }

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	var o serveOpts
	o.ef.bind(fs)
	fs.IntVar(&o.port, "port", 8095, "port to listen on (127.0.0.1 only; 0 takes a free port and writes it to ~/.shikii/agent/pages/; the hook starts it this way)")
	fs.StringVar(&o.specsDir, "specs", "", "folder of specs (*.yaml) (default: <store>/decisions)")
	fs.StringVar(&o.storeDir, "store", ".shikii", "where records and labels are kept")
	fs.BoolVar(&o.autoCalibrate, "auto-calibrate", true, "calibrate automatically and write back to the spec once truths reach eval.min_n")
	fs.StringVar(&o.proposer, "proposer", "", "URL of the proposer (a chat LLM) that drafts wording candidates. Empty measures only the spec's variants")
	fs.StringVar(&o.proposerModel, "proposer-model", "", "model name of the proposer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := resolveStoreFlag(fs); err != nil {
		return err
	}
	o.ef.useForHookDecisions()
	o.resolve()
	common, _ := agentHome()
	absStore, _ := filepath.Abs(o.storeDir)
	hub := absStore == common
	if !hub {
		if _, err := loadSpecs(o.specsDir); err != nil {
			return err
		}
	}
	scope := pageScope(absStore)
	// the project's page is a serve of a project's .shikii on a free port; a store under another name started by hand is not (D4)
	auto := startedByHook(hub, o.port, absStore)
	if auto {
		lock, ok := holdPageLock(common, scope)
		if !ok { // one page per project: another page of this project holds the lock
			log.Printf("shikii serve: another page of this project is running (it holds %s); not starting another", pageLockPath(common, scope))
			return nil
		}
		defer lock.Close()
		// A page of an earlier version (no lock) that answers. Not livePage: this serve holds the lock itself, so a recorded pid reused by another process would look up
		if r, ok := readPageRecord(common, scope); ok && pageUp(r) {
			log.Printf("shikii serve: this project's page is already up at %s/ ; not starting another", r.url())
			return nil
		}
	} else if err := waitForLocalModel(o.ef.local, usesHosted(), systemone.WaitReady); err != nil {
		return err // a serve started by hand (Docker etc.) keeps the old behavior: the port does not open until the model answers
	}
	ln, err := listenPage(common, scope, hub, o.port)
	if err != nil {
		return err
	}
	log.Printf("shikii serve: specs=%s store=%s local=%q hosted=%q auto-calibrate=%v  →  http://%s/", o.specsDir, o.storeDir, systemone.RedactURL(o.ef.local), systemone.RedactURL(o.ef.hosted), o.autoCalibrate, ln.Addr())
	var handler http.Handler = newServer(o)
	if hub {
		handler = newProjectHub(common, o)
	}
	handler = localHostOnly(handler) // so another site cannot read records or work records through DNS rebinding (audit 0.8.1-1)
	httpServer := newHTTPServer(handler)
	if !auto {
		return httpServer.Serve(ln)
	}
	go func() { // stop when this is no longer the project's page (its record was removed, or its store is gone)
		for range pageOwnerTicks() {
			if !pageStillOwned(common, scope, absStore, os.Getpid()) {
				log.Printf("shikii serve: this is no longer the project's page (its record or store is gone); stopping")
				ln.Close()
				return
			}
		}
	}()
	// Wait for the local model after opening the port (status, queue and wiki can answer without a model; the hook does not wait).
	// A model that never comes up closes the listener with its error; whichever closes it, Serve ends the same way (re-audit C-6).
	modelErr := make(chan error, 1)
	go func() {
		if err := waitForLocalModel(o.ef.local, usesHosted(), systemone.WaitReady); err != nil {
			modelErr <- err
			ln.Close()
		}
	}()
	if err := httpServer.Serve(ln); !errors.Is(err, net.ErrClosed) {
		return err
	}
	select {
	case err := <-modelErr:
		return err
	default:
		return nil // stopped by the check above
	}
}

// startedByHook tells the project's page started by the hook (0.8.0: free port, mark, browser; the local model is waited for after the port
// opens) from a serve started by hand. A store under another name started by hand is not the project's page (D4), so the owner check never
// stops it (re-audit C-1).
func startedByHook(hub bool, port int, absStore string) bool {
	return !hub && port == 0 && filepath.Base(absStore) == ".shikii"
}

// newHTTPServer bounds every phase of an HTTP connection. The longest handler operation is a
// model decision with a 60-second context deadline, so the write deadline leaves another minute
// for the response while slow request headers and bodies are cut off much earlier. Replies in flight
// while a calibration holds or waits for the lock are kept alive (keepRepliesAliveDuringCalibration).
func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           keepRepliesAliveDuringCalibration(handler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

// calibrationsActive counts the holders and waiters of every calibration lock in this process (lockCalibration keeps it).
var calibrationsActive atomic.Int64

// replyCheck is how often a reply in flight looks whether a calibration is active; it is well under WriteTimeout.
const replyCheck = 20 * time.Second

// watchReply runs check every replyCheck until the returned stop is called; tests replace it to run check at the moment they choose.
var watchReply = func(check func()) (stop func()) {
	t := time.NewTicker(replyCheck)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-t.C:
				check()
			}
		}
	}()
	return func() {
		t.Stop()
		close(done)
	}
}

// replyNow is the clock a reply in flight sets its write deadline from; tests move it so a deadline is already past instead of waiting.
var replyNow = time.Now
