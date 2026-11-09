package main

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// keepRepliesAliveDuringCalibration keeps a reply that has not started writing alive while a calibration holds or waits for the lock (a
// background tune holds it for its model run and the calibration after it, up to 40 minutes): it works at the connection, whatever path the
// handler took to the lock, so no handler has to remember it. Before, each handler passed its writer to the lock, and audits A to G of
// 0.9.7 kept finding paths that passed none or another.
// The write timeout still bounds writing (eaf2c9b): once the handler starts writing, its deadline is the write timeout from then on, so a
// client that reads slowly is cut even during a calibration; and when no calibration is active any more, a reply that has not started
// writing gets the write timeout from then on (0.9.7 full audit 2-1, 2-2).
func keepRepliesAliveDuringCalibration(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := 2 * time.Minute
		if srv, ok := r.Context().Value(http.ServerContextKey).(*http.Server); ok && srv.WriteTimeout > 0 {
			limit = srv.WriteTimeout
		}
		tw := &trackedWriter{ResponseWriter: w, rc: http.NewResponseController(w), limit: limit}
		stop := watchReply(tw.check)
		defer func() {
			tw.finish()
			stop()
		}()
		h.ServeHTTP(tw, r)
	})
}

// trackedWriter is the writer of a reply in flight: it knows whether the handler started writing, and whether its deadline was extended.
type trackedWriter struct {
	http.ResponseWriter
	rc       *http.ResponseController
	limit    time.Duration
	mu       sync.Mutex
	started  bool // the handler wrote a header or a byte: the write timeout runs from then on
	extended bool
	finished bool
}

func (w *trackedWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *trackedWriter) start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.started && !w.finished {
		w.started = true
		if w.extended {
			_ = w.rc.SetWriteDeadline(replyNow().Add(w.limit))
		}
	}
}

func (w *trackedWriter) WriteHeader(code int) {
	w.start()
	w.ResponseWriter.WriteHeader(code)
}

func (w *trackedWriter) Write(b []byte) (int, error) {
	w.start()
	return w.ResponseWriter.Write(b)
}

// check runs every replyCheck: while a calibration is active, a reply that has not started writing is kept alive; once none is, a reply
// that was kept alive gets the write timeout from now.
func (w *trackedWriter) check() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started || w.finished {
		return
	}
	if calibrationsActive.Load() > 0 {
		_ = w.rc.SetWriteDeadline(replyNow().Add(longResponseLimit))
		w.extended = true
	} else if w.extended {
		_ = w.rc.SetWriteDeadline(replyNow().Add(w.limit))
		w.extended = false
	}
}

// Flush and ReadFrom keep what the connection's writer can do for a handler that asks (0.9.7 re-audit 2-7); both start the reply.
func (w *trackedWriter) Flush() {
	w.start()
	_ = w.rc.Flush()
}

func (w *trackedWriter) ReadFrom(r io.Reader) (int64, error) {
	w.start()
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(w.ResponseWriter, r)
}

func (w *trackedWriter) finish() {
	w.mu.Lock()
	w.finished = true // the response is not touched after the handler returns
	w.mu.Unlock()
}

// lockCalibration takes the calibration lock and returns its release. While it is held or waited for, replies in flight are kept alive
// (keepRepliesAliveDuringCalibration). Only this function touches calibMu (calib_lock_writer_test.go).
func (s *server) lockCalibration() func() {
	calibrationsActive.Add(1)
	s.calibMu.Lock()
	return func() {
		s.calibMu.Unlock()
		calibrationsActive.Add(-1)
	}
}

// longResponseLimit bounds a long handler's reply: a calibration that asks the model again for many earlier truths, or a tune over every fold.
// A background tune can hold the calibration lock for its 30-minute model run plus up to 10 minutes of calibration, so a reply waiting behind
// it needs more than 40 minutes (0.9.6 recheck, low 2).
const longResponseLimit = 45 * time.Minute

const (
	tuneRunLimit = 30 * time.Minute // a tune's model run
	replayLimit  = 10 * time.Minute // a calibration asking the model again for earlier truths
)
