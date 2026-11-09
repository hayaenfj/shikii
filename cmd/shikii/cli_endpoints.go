package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/systemone"
)

// endpointFlags are the endpoint flags shared by decide / calibrate.
type endpointFlags struct {
	model, hosted, local string
	tainted              bool
}

func (f *endpointFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&f.model, "model", "jev-latest", "model name (sent in the request; in calibrate it is also the calibration key)")
	fs.StringVar(&f.hosted, "hosted", "https://api.typesafe.ai", "hosted URL. A URL with a path is used as is (e.g. https://openrouter.ai/api/alpha/decisions). Key from DECISIONS_API_KEY or TYPESAFE_API_KEY")
	fs.StringVar(&f.local, "local", "", "base URL of a local server (llama.cpp etc.)")
	fs.BoolVar(&f.tainted, "tainted", false, "session that read production data: never send to hosted")
}

// useForHookDecisions makes decisions this process makes through hookDecide (reflect place, a page it starts) use the URLs given to mcp or
// serve instead of the hook's fixed ones (0.9.5).
func (f endpointFlags) useForHookDecisions() {
	if f.local != "" {
		hookLocalURL = f.local
	}
	if f.hosted != "" {
		hookHostedURL = f.hosted
	}
}

// hostedKey is the key for hosted: DECISIONS_API_KEY (generic) or TYPESAFE_API_KEY. This process never fetches keys itself.
func hostedKey() string {
	if k := os.Getenv("DECISIONS_API_KEY"); k != "" {
		return k
	}
	return os.Getenv("TYPESAFE_API_KEY")
}

// modelChoice is SHIKII_MODEL (local / hosted). Use local to keep the local model even when a key is set. Empty means both (hosted first if there is a key).
func modelChoice() string {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv("SHIKII_MODEL"))); v {
	case "local", "hosted":
		return v
	}
	return ""
}

func (f *endpointFlags) endpoints() []decide.Endpoint {
	var eps []decide.Endpoint
	if key := hostedKey(); key != "" && f.hosted != "" && modelChoice() != "local" {
		eps = append(eps, decide.Endpoint{Kind: "hosted", Client: systemone.New(f.hosted, key, f.model)})
	}
	if f.local != "" && modelChoice() != "hosted" {
		eps = append(eps, decide.Endpoint{Kind: "local", Client: systemone.New(f.local, "", f.model)})
	}
	return eps
}

func bindStore(fs *flag.FlagSet) *string {
	return fs.String("store", ".shikii", "where records and labels are stored")
}

type kvFlag map[string]string

func (k kvFlag) String() string { return "" }

func (k kvFlag) Set(v string) error {
	i := strings.IndexByte(v, '=')
	if i <= 0 {
		return fmt.Errorf("want key=value, got %q", v)
	}
	k[v[:i]] = v[i+1:]
	return nil
}

// The report destination is passed per call. HTTP handling never swaps the process-wide stdout.
func reportWriter(writers ...io.Writer) io.Writer {
	if len(writers) > 0 && writers[0] != nil {
		return writers[0]
	}
	return stdout
}
