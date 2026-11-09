package main

import (
	"net/http"

	"github.com/hayaenfj/shikii/internal/store"
)

func newServer(o serveOpts) http.Handler { return newServerState(o).handler() }

func newServerState(o serveOpts) *server {
	o.resolve()
	return &server{o: o, st: store.Store{Dir: o.storeDir}, lastCal: map[string]calibrationReport{}, tuning: map[string]bool{}, tuneErr: map[string]string{}}
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleUI)
	mux.HandleFunc("/v1/specs", s.handleSpecs)
	mux.HandleFunc("/v1/decide", s.handleDecide)
	mux.HandleFunc("/v1/label", s.handleLabel)
	mux.HandleFunc("/v1/queue", s.handleQueue)
	mux.HandleFunc("/v1/answered", s.handleAnswered)
	mux.HandleFunc("/v1/redo", s.handleRedo)
	mux.HandleFunc("/v1/status", s.handleStatus)
	mux.HandleFunc("/v1/page-health", s.handlePageHealth)
	mux.HandleFunc("/v1/tune", s.handleTuneGet)
	mux.HandleFunc("/v1/tune/run", s.handleTuneRun)
	mux.HandleFunc("/v1/tune/sync", s.handleTuneSync)
	mux.HandleFunc("/v1/tune/adopt", s.handleTuneAdopt)
	mux.HandleFunc("/v1/tune/review", s.handleTuneReview)
	mux.HandleFunc("/v1/tune/variant", s.handleTuneVariant)
	mux.HandleFunc("/v1/wiki", s.handleWikiList)
	mux.HandleFunc("/v1/wiki/claim", s.handleWikiClaim)
	mux.HandleFunc("/v1/wiki/notebook", s.handleNotebook)
	mux.HandleFunc("/v1/wiki/article", s.handleArticle)
	mux.HandleFunc("/v1/handoff/file", s.handleHandoffFile)
	mux.HandleFunc("/v1/review", s.handleReview)
	mux.HandleFunc("/v1/reflect", s.handleReflect)
	return mux
}
