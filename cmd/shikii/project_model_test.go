package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// modelStore makes a project store whose project.json chose model ("" for none).
func modelStore(t *testing.T, model string) string {
	t.Helper()
	scope := t.TempDir()
	store := filepath.Join(scope, ".shikii")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(projectActivation{Version: projectInitVersionNow, Scope: scope, Model: model})
	if err := os.WriteFile(filepath.Join(store, "project.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return store
}

func endpointKinds(store string) []string {
	var kinds []string
	for _, ep := range hookEndpoints(store) {
		kinds = append(kinds, ep.Kind)
	}
	return kinds
}

// A project's own choice comes before SHIKII_MODEL, for the hooks, for starting the local model and for the page's AI state.
func TestTheProjectsModelComesBeforeSHIKII_MODEL(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("DECISIONS_API_KEY", "")
	local, hosted, none := modelStore(t, "local"), modelStore(t, "hosted"), modelStore(t, "")

	t.Setenv("SHIKII_MODEL", "hosted")
	if got := endpointKinds(local); !slices.Equal(got, []string{"local"}) {
		t.Fatalf("a project that chose local stays local even with a key and SHIKII_MODEL=hosted: %v", got)
	}
	if usesHosted(local) || upBlockedByHosted(local) != "" {
		t.Fatalf("a project that chose local does not use Jev, and the local model may start for it")
	}
	if got := endpointKinds(none); !slices.Equal(got, []string{"hosted"}) {
		t.Fatalf("a project that chose nothing follows SHIKII_MODEL: %v", got)
	}

	t.Setenv("SHIKII_MODEL", "local")
	if got := endpointKinds(hosted); !slices.Equal(got, []string{"hosted"}) {
		t.Fatalf("a project that chose hosted uses Jev even with SHIKII_MODEL=local: %v", got)
	}
	if !usesHosted(hosted) || upBlockedByHosted(hosted) == "" {
		t.Fatalf("a project that chose hosted uses Jev, and the local model is not started for it")
	}
	if got := endpointKinds(none); !slices.Equal(got, []string{"local"}) {
		t.Fatalf("a project that chose nothing follows SHIKII_MODEL: %v", got)
	}
}

// shikii decide (what chiten runs) follows the project of its --store.
func TestDecideFollowsTheProjectOfItsStore(t *testing.T) {
	hostedModel := rawAnswerModel(t, `{"type":"noul","noul":0.6}`)
	localModel := rawAnswerModel(t, `{"type":"noul","noul":0.3}`)
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("DECISIONS_API_KEY", "")
	specPath := writeNoulSpec(t, t.TempDir())
	b, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte(strings.Replace(string(b), "send_to: [local]", "send_to: [hosted, local]", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ project, env, want string }{
		{"local", "hosted", "endpoint=local"},
		{"hosted", "local", "endpoint=hosted"},
		{"", "local", "endpoint=local"},
	} {
		t.Setenv("SHIKII_MODEL", c.env)
		out := capture(t, func() error {
			return cmdDecide([]string{specPath, "--no-record", "--store", modelStore(t, c.project), "--hosted", hostedModel.URL, "--local", localModel.URL, "-i", "a=x"})
		})
		if !strings.Contains(out, c.want) {
			t.Fatalf("project %q with SHIKII_MODEL=%s goes to %s:\n%s", c.project, c.env, c.want, out)
		}
	}
}

// The page and MCP decide for their own project.
func TestTheServerDecidesForItsOwnProject(t *testing.T) {
	store := modelStore(t, "local")
	s := newServerState(serveOpts{storeDir: store})
	if got := s.o.ef.storeDir(); got != store {
		t.Fatalf("the server's endpoints follow its store %s: %q", store, got)
	}
}

// A hook in a project that chose local starts the local model for it even with a key, and tells up which project it is for.
func TestTheHookStartsTheLocalModelForAProjectThatChoseLocal(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("DECISIONS_API_KEY", "")
	t.Setenv("SHIKII_MODEL", "")
	oldLocal, oldSpawn, oldLook := hookLocalURL, hookSpawn, lookPath
	hookLocalURL, hookSpawn, spawned = "http://127.0.0.1:1", recordSpawn, nil
	lookPath = func(string) (string, error) { return "/usr/local/bin/llama-server", nil }
	t.Cleanup(func() { hookLocalURL, hookSpawn, lookPath = oldLocal, oldSpawn, oldLook })
	store := modelStore(t, "local")
	ensureModel(t.TempDir(), store)
	if !slices.Equal(spawned, []string{"up --store " + store}) {
		t.Fatalf("the hook starts up for the project that chose local: %v", spawned)
	}
	spawned = nil
	ensureModel(t.TempDir(), modelStore(t, ""))
	if len(spawned) != 0 {
		t.Fatalf("with a key, a project that chose nothing does not start the local model: %v", spawned)
	}
}

// shikii init --model sets the choice; init again without it keeps the choice.
func TestInitKeepsTheProjectsModel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	scope := t.TempDir()
	root := filepath.Join(scope, ".shikii")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ set, want string }{{"local", "local"}, {"", "local"}, {"hosted", "hosted"}, {"", "hosted"}} {
		if err := finishProjectInitModel(root, scope, projectInitVersionNow, c.set); err != nil {
			t.Fatal(err)
		}
		if got := projectModel(root); got != c.want {
			t.Fatalf("after init --model %q the project's model is %q: %q", c.set, c.want, got)
		}
	}
	if err := finishProjectInit(root, scope, projectInitVersionNow); err != nil || projectModel(root) != "hosted" {
		t.Fatalf("a re-initialization (or a move) keeps the choice: %q %v", projectModel(root), err)
	}
}

func TestInitModelArg(t *testing.T) {
	for args, want := range map[string]string{"": "", "--model local": "local", "--model hosted": "hosted", "--model=local": "local"} {
		got, err := initModelArg(strings.Fields(args))
		if err != nil || got != want {
			t.Fatalf("%q: got %q %v, want %q", args, got, err, want)
		}
	}
	for _, args := range []string{"--model", "--model jev", "--yes", "--model local extra"} {
		if _, err := initModelArg(strings.Fields(args)); err == nil {
			t.Fatalf("%q is refused", args)
		}
	}
}

// modelReads lists where SHIKII_MODEL, modelChoice and project.json's model are read, outside the functions allowed to read them.
// The model choice has one owner (modelChoiceFor): anything that reads SHIKII_MODEL or the project's model by itself would skip the
// project's choice or SHIKII_MODEL.
func modelReads(fset *token.FileSet, files map[string]*ast.File) []string {
	allowed := map[string]string{
		"modelChoice":     "SHIKII_MODEL", // reads the environment variable, for modelChoiceFor only
		"modelChoiceFor":  "modelChoice",
		"projectModel":    "Model",
		"setProjectModel": "Model", // writes the choice the page sets; it never decides which model answers
	}
	var bad []string
	for name, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				what := ""
				switch x := n.(type) {
				case *ast.BasicLit:
					if x.Value == `"SHIKII_MODEL"` {
						what = "SHIKII_MODEL"
					}
				case *ast.CallExpr:
					if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "modelChoice" {
						what = "modelChoice"
					}
				case *ast.SelectorExpr:
					if x.Sel.Name == "Model" {
						if id, ok := x.X.(*ast.Ident); ok && id.Name == "a" || ok && id.Name == "activation" {
							what = "Model"
						}
					}
				}
				if what != "" && allowed[fn.Name.Name] != what {
					bad = append(bad, filepath.Base(name)+": "+fn.Name.Name+" reads "+what)
				}
				return true
			})
		}
	}
	slices.Sort(bad)
	return bad
}

func TestOnlyModelChoiceForDecidesTheModel(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bad := modelReads(fset, pkgs["main"].Files); len(bad) > 0 {
		t.Fatalf("the model choice is read only through modelChoiceFor: %v", bad)
	}
}

func TestModelReadsNamesAReadOutsideTheOwner(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "stray.go", `package main
import "os"
func stray() bool { return modelChoice() == "local" || os.Getenv("SHIKII_MODEL") != "" }
func modelChoiceFor(store string) string { return modelChoice() }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := modelReads(fset, map[string]*ast.File{"stray.go": f})
	want := []string{"stray.go: stray reads SHIKII_MODEL", "stray.go: stray reads modelChoice"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// The page sets the project's choice through v1/model; the status shows it, a request from another origin is refused, and the
// next decisions of this project follow it.
func TestThePageSetsTheProjectsModel(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("DECISIONS_API_KEY", "")
	t.Setenv("SHIKII_MODEL", "")
	store := modelStore(t, "")
	if err := os.MkdirAll(filepath.Join(store, "decisions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := seedAgentSpecs(filepath.Join(store, "decisions")); err != nil {
		t.Fatal(err)
	}
	h := newServer(serveOpts{storeDir: store})
	post := func(body, origin string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/model", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	if code := post(`{"model":"local"}`, ""); code != 200 || projectModel(store) != "local" || usesHosted(store) {
		t.Fatalf("the page chooses local: %d %q", code, projectModel(store))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var st struct {
		Model struct {
			Name, Choice string
			HasKey       bool `json:"has_key"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil || st.Model.Choice != "local" || st.Model.Name != "local" || !st.Model.HasKey {
		t.Fatalf("the status shows the choice and the AI that answers: %+v %v", st.Model, err)
	}
	if code := post(`{"model":"hosted"}`, "https://example.com"); code != 403 || projectModel(store) != "local" {
		t.Fatalf("another site cannot change it: %d %q", code, projectModel(store))
	}
	if code := post(`{"model":"jev"}`, ""); code != 400 || projectModel(store) != "local" {
		t.Fatalf("an unknown model is refused: %d %q", code, projectModel(store))
	}
	if code := post(`{"model":""}`, ""); code != 200 || projectModel(store) != "" || !usesHosted(store) {
		t.Fatalf("an empty choice follows SHIKII_MODEL again: %d %q", code, projectModel(store))
	}
	b, _ := os.ReadFile(filepath.Join(store, "project.json"))
	var a projectActivation
	if json.Unmarshal(b, &a) != nil || a.Version != projectInitVersionNow || a.Scope == "" {
		t.Fatalf("changing the model keeps the initialization mark: %s", b)
	}
	if code := post(`{"model":"local"}`, ""); code != 200 {
		t.Fatal(code)
	}
	if err := os.Remove(filepath.Join(store, "project.json")); err != nil {
		t.Fatal(err)
	}
	if code := post(`{"model":"local"}`, ""); code != 400 {
		t.Fatalf("a store that is not an initialized project is not made one: %d", code)
	}
}

// shikii up run by hand follows the project of the current folder: one that chose local starts the model even with a key.
func TestUpByHandFollowsTheProjectOfTheCurrentFolder(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	t.Setenv("DECISIONS_API_KEY", "")
	t.Setenv("SHIKII_MODEL", "")
	t.Setenv("HOME", t.TempDir())
	local := modelStore(t, "local")
	t.Chdir(filepath.Dir(local))
	if msg := upBlocked(""); msg != "" {
		t.Fatalf("in a project that chose local, shikii up starts the model even with a key: %s", msg)
	}
	t.Chdir(filepath.Dir(modelStore(t, "")))
	if msg := upBlocked(""); msg == "" {
		t.Fatalf("in a project that chose nothing, the key keeps the local model from starting")
	}
}
