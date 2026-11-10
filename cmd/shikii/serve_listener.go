package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// listenPage starts listening. For the project's page started by the hook (port 0), it takes a free port, writes a mark with the port and pid
// to ~/.shikii/agent/pages/, and opens it once in the browser. A serve started by hand (fixed port) writes no mark (so it never overwrites the hook page's mark; audit R5).
func listenPage(common, scope string, hub bool, port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", bindHost(), port))
	if err != nil {
		return nil, err
	}
	if !hub && port == 0 {
		p := ln.Addr().(*net.TCPAddr).Port
		if err := writePageRecord(common, scope, p, os.Getpid()); err != nil {
			ln.Close()
			return nil, err
		}
		openBrowser(fmt.Sprintf("http://127.0.0.1:%d/", p))
	}
	return ln, nil
}

// waitForLocalModel waits by itself for the local model's endpoint to come up (when Docker starts the model first; it does not rely on curl / wget).
// Without --local (hosted only) it does not wait: hosted is an outside service, not something to wait to start.
// It also does not wait when there is a key and Jev is used (hosted = usesHosted()): the rule is not to start a local model (0.7.1), so waiting would keep the page from ever opening
// (the hook always starts serve with --local; observed on a real machine on 2026-10-23). At most 15 minutes. When the URL is wrong, the first line makes it clear.
func waitForLocalModel(local string, hosted bool, wait func(context.Context, string, time.Duration, func(string, ...any)) error) error {
	if local == "" {
		return nil
	}
	if hosted {
		log.Printf("key present: decisions go to TypeSafe Jev; not waiting for the local model at %s (SHIKII_MODEL=local would wait)", systemone.RedactURL(local))
		return nil
	}
	log.Printf("waiting for the local model at %s (if the URL is wrong, stop now and fix --local; serve does not open its port until the model answers)", systemone.RedactURL(local))
	wctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	return wait(wctx, local, 3*time.Second, log.Printf)
}

type calibrationResult struct {
	Reason      string `json:"reason"`
	HumanTruths int    `json:"human_truths"`
	MinN        int    `json:"min_n"`
	Hash        string `json:"hash"`
	Model       string `json:"model"`
	At          string `json:"at"`
	// TriesWordings: when no line fits, the agent is asked for other wordings (the hook's questions only: variantsContext, hook variants submit).
	// The page says "the AI will try other wordings" only then (0.9.4 audit)
	TriesWordings bool `json:"tries_wordings"`
}

type calibrationReport struct {
	log    string
	result calibrationResult
}

type server struct {
	wikiMu  sync.Mutex // serializes wiki operations from the page (tune's updateClaims is separate when in another process; writes are atomic)
	o       serveOpts
	st      store.Store
	calibMu sync.Mutex                   // serializes calibration and candidate saving for specs of the same project
	lastCal map[string]calibrationReport // spec id → report of the last automatic calibration
	mu      sync.Mutex
	tuning  map[string]bool   // spec id → tune is running in the background
	tuneErr map[string]string // spec id → the last tune failure
	// restoreMu serializes the person's undos of the AI's judgments on past answers: an undo is checked against what is in place
	// and written under it, so a second click on the same undo is refused instead of written twice
	restoreMu sync.Mutex
}

// resolve fills in the store defaults: specs in <store>/decisions, the wiki in <store>/wiki (the basic layout; there are no other flags).
func (o *serveOpts) resolve() {
	if o.projectScope == "" {
		o.projectScope = currentProjectScope()
	}
	if o.specsDir == "" {
		o.specsDir = specsDirOf(o.storeDir)
	}
	if o.wikiDir == "" {
		o.wikiDir = wikiDirOf(o.storeDir)
	}
}
