package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/store"
	"github.com/hayaenfj/shikii/internal/systemone"
)

func TestProjectDefaultCLIRejectsLinkedStore(t *testing.T) {
	a, b := gitRepo(t), gitRepo(t)
	bStore := filepath.Join(b, ".shikii")
	if err := os.MkdirAll(filepath.Join(bStore, "decisions"), 0700); err != nil {
		t.Fatal(err)
	}
	writeSpec(t, filepath.Join(bStore, "decisions"), "true")
	if err := os.Symlink(bStore, filepath.Join(a, ".shikii")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(a)
	if _, _, err := currentProjectStore(); err == nil {
		t.Fatal("the precondition that the check rejects links does not hold")
	}
	model := fakeSystemOne(t, "m", map[string]float64{"change_request": .9, "question": .1, "other": 0})
	var output bytes.Buffer
	old := stdout
	stdout = &output
	defer func() { stdout = old }()
	err := cmdDecide([]string{".shikii/decisions/intent.yaml", "--local", model.URL, "--model", "m", "-i", "message=AUDIT_PROJECT_A_SYNTHETIC"})
	if err == nil {
		t.Fatal("must reject on the store check error and not save into another work")
	}
	for _, command := range []func([]string) error{cmdMCP, cmdServe} {
		err := command([]string{"--specs", filepath.Join(a, "missing-specs")})
		if err == nil || !strings.Contains(err.Error(), "link") {
			t.Fatalf("MCP and the page must also carry over the store refusal: %v", err)
		}
	}
	records, readErr := (store.Store{Dir: bStore}).Records("intent", 0)
	if readErr != nil || len(records) != 0 {
		t.Fatalf("must not change the refused link target: records=%d err=%v", len(records), readErr)
	}

}

func TestProjectCalibrationsKeepTheirReports(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, b := gitRepo(t), gitRepo(t)
	aStore, aScope := initTestProject(t, a)
	bStore, bScope := initTestProject(t, b)
	enteredA, enteredB, releaseA, releaseB := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req systemone.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(req.State, "AUDIT_REPLAY_A") {
			close(enteredA)
			<-releaseA
		} else if strings.Contains(req.State, "AUDIT_REPLAY_B") {
			close(enteredB)
			<-releaseB
		} else {
			t.Errorf("unexpected synthetic input")
			return
		}
		ans := map[string]systemone.Answer{}
		for key := range req.Questions {
			ans[key] = systemone.Answer{Type: "choice", Choice: "change_request", Probabilities: map[string]float64{"change_request": .9, "question": .1, "other": 0}, Confidence: .9}
		}
		_ = json.NewEncoder(w).Encode(systemone.Response{Model: "m", Answers: ans})
	}))
	defer func() {
		select {
		case <-releaseA:
		default:
			close(releaseA)
		}
		select {
		case <-releaseB:
		default:
			close(releaseB)
		}
		model.Close()
	}()
	seed := func(root, name string) string {
		path := writeSpec(t, filepath.Join(root, "decisions"), "true")
		sp := mustLoad(t, path)
		st := store.Store{Dir: root}
		target := ""
		for i := 0; i < 3; i++ {
			input := map[string]string{"message": fmt.Sprintf("AUDIT_CURRENT_%s_%d", name, i)}
			hash := sp.Hash()
			if i == 2 {
				input["message"] = "AUDIT_REPLAY_" + name
				hash = "old-wording"
			}
			rec := store.Record{TraceID: store.NewTraceID(), SpecID: "intent", SpecHash: hash, Model: "m", Type: "choice", Inputs: input, InputsHash: store.HashInputs(input), Raw: map[string]float64{"change_request": .9, "question": .1, "other": 0}, Top: "change_request", P: .9, Band: "ask", At: time.Now()}
			if err := st.AppendRecord(rec); err != nil {
				t.Fatal(err)
			}
			if i == 1 {
				target = rec.TraceID
				continue
			}
			label, err := store.LabelFor(&rec, "change_request", 1, "ui")
			if err != nil {
				t.Fatal(err)
			}
			if err = st.AppendLabel(label); err != nil {
				t.Fatal(err)
			}
		}
		return target
	}
	traceA, traceB := seed(aStore, "A"), seed(bStore, "B")
	common, _ := agentHome()
	var ef endpointFlags
	ef.local, ef.model = model.URL, "m"
	hub := newProjectHub(common, serveOpts{ef: ef, autoCalibrate: true})
	var initial bytes.Buffer
	old := stdout
	stdout = &initial
	defer func() { stdout = old }()
	type result struct {
		code int
		body string
	}
	doneA, doneB := make(chan result, 1), make(chan result, 1)
	invoke := func(scope, trace string, ch chan result) {
		body := `{"trace_id":"` + trace + `","truth":"change_request","source":"ui"}`
		r := httptest.NewRequest("POST", "/p/"+projectID(scope)+"/v1/label", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		hub.ServeHTTP(w, r)
		ch <- result{w.Code, w.Body.String()}
	}
	go invoke(aScope, traceA, doneA)
	select {
	case <-enteredA:
	case <-time.After(5 * time.Second):
		t.Fatal("A's calibration does not reach the model")
	}
	go invoke(bScope, traceB, doneB)
	select {
	case <-enteredB:
	case <-time.After(5 * time.Second):
		t.Fatal("B's calibration does not reach the model")
	}
	close(releaseA)
	ra := <-doneA
	close(releaseB)
	rb := <-doneB
	wrongRestore := stdout != &initial
	stdout = &initial
	leaked := strings.Contains(rb.body, filepath.Join(aStore, "decisions", "intent.yaml"))
	ownMissing := !strings.Contains(ra.body, filepath.Join(aStore, "decisions", "intent.yaml"))
	if ra.code != 200 || rb.code != 200 || leaked || ownMissing || wrongRestore {
		t.Fatalf("concurrent calibrations must not mix reports and outputs: A=%d B=%d leaked=%v ownMissing=%v wrongRestore=%v", ra.code, rb.code, leaked, ownMissing, wrongRestore)
	}

}

func TestProjectCommandsShareRootFromSubdirectories(t *testing.T) {
	for _, operation := range []string{"new", "init", "tune"} {
		t.Run(operation, func(t *testing.T) {
			repo := gitRepo(t)
			root := hookEnv(t, nil, repo)
			sub := filepath.Join(repo, "docs")
			if err := os.Mkdir(sub, 0700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(sub)
			switch operation {
			case "new":
				capture(t, func() error {
					return cmdNew([]string{"audit-question", "--type", "noul", "--question", "Is the synthetic task complete?", "--input", "message", "--pii", "true", "--send-to", "local"})
				})
				if !fileExists(filepath.Join(root, "decisions", "audit-question.yaml")) {
					t.Fatal("new must create the spec at the project root")
				}
			case "init":
				capture(t, func() error { return cmdInit(nil) })
				for _, name := range []string{".mcp.json", "opencode.json", ".shikii/wiki/index.md"} {
					if !fileExists(filepath.Join(repo, name)) {
						t.Fatalf("init must prepare at the project root: %s", name)
					}
				}
			case "tune":
				path := specWithVariants(t, root)
				sp := mustLoad(t, path)
				st := store.Store{Dir: root}
				for _, id := range []string{"00000000000000a1", "00000000000000a2"} {
					input := map[string]string{"message": "fix " + id}
					if err := st.AppendLabel(store.Label{TraceID: id, SpecID: sp.ID, SpecHash: sp.Hash(), Type: "choice", Model: "m", Inputs: input, InputsHash: store.HashInputs(input), Truth: "change_request", Weight: 1, Source: "manual", Raw: map[string]float64{"change_request": .9, "question": .1, "other": 0}}); err != nil {
						t.Fatal(err)
					}
				}
				model := wordingSensitiveModel(t)
				out := capture(t, func() error { return cmdTune([]string{path, "--labeled", "none", "--local", model.URL}) })
				if !strings.Contains(out, "2 truths") {
					t.Fatal("tune must use the same project's truths")
				}
			}
			if fileExists(filepath.Join(sub, ".shikii")) || fileExists(filepath.Join(sub, ".mcp.json")) {
				t.Fatal("must not create a second store or settings in a subfolder")
			}
		})
	}
}

func TestProjectHubRedirectKeepsQuery(t *testing.T) {
	repo := gitRepo(t)
	hookEnv(t, nil, repo)
	common, _ := agentHome()
	hub := newProjectHub(common, serveOpts{})
	path := "/p/" + projectID(feedbackScope(repo))
	query := "spec=done-as-asked&trace=0000000000000001"
	rec := httptest.NewRecorder()
	hub.ServeHTTP(rec, httptest.NewRequest("GET", path+"?"+query, nil))
	if rec.Code != 307 || rec.Header().Get("Location") != path+"/?"+query {
		t.Fatalf("the trailing / redirect must keep the record being opened: status=%d location=%s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestProjectTuningKeepsProcessOutput(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "a.yaml"), filepath.Join(dir, "b.yaml")}
	for _, path := range paths {
		if err := syscall.Mkfifo(path, 0600); err != nil {
			t.Fatal(err)
		}
	}
	openWriter := func(path string) *os.File {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			f, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0600)
			if err == nil {
				return f
			}
			if !errors.Is(err, syscall.ENXIO) {
				t.Fatalf("cannot open the synthetic pipe: %v", err)
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("the other project's tune did not start in time")
		return nil
	}
	original := stdout
	defer func() { stdout = original }()
	a, b := &server{}, &server{}
	doneA, doneB := make(chan struct{}), make(chan struct{})
	go func() { defer close(doneA); a.runTuneCaptured(tuneOpts{specPath: paths[0]}) }()
	writerA := openWriter(paths[0])
	defer writerA.Close()
	go func() { defer close(doneB); b.runTuneCaptured(tuneOpts{specPath: paths[1]}) }()
	writerB := openWriter(paths[1])
	defer writerB.Close()
	writerA.Close()
	<-doneA
	writerB.Close()
	<-doneB
	if stdout != original {
		t.Fatal("overlapping tune runs of other works must not change the process's output")
	}
}
