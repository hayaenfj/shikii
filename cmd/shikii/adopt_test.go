package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
)

// serve + tune の道具立て: 文言で答えが変わる偽モデル、候補付きの spec、正解のファイル、wiki。
func newTuneFixture(t *testing.T) (srvURL, specsDir, wiki, labeledPath string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	specsDir = filepath.Join(dir, "decisions")
	specWithVariants(t, dir) // decisions/intent.yaml with variants strict / worse; the wiki is expected next to decisions/, with no flag
	labeledPath = filepath.Join(dir, "rows.json")
	rows := bankRows()
	if err := os.WriteFile(labeledPath, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	// eval.labeled を spec に書いておく(serve の tune はフラグで渡せないので)
	p := filepath.Join(specsDir, "intent.yaml")
	b, _ := os.ReadFile(p)
	src := strings.Replace(string(b), "eval:\n  min_n: 2\n", "eval:\n  min_n: 2\n  labeled: "+labeledPath+"\n", 1)
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	wiki = filepath.Join(dir, ".shikii", "wiki")
	model := wordingSensitiveModel(t)
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	h := newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(dir, ".shikii"), ef: ef, autoCalibrate: true})
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s.URL, specsDir, wiki, labeledPath
}

// 画面から tune を回し、最新の結果を読み、人の「意味は同じ」で採用する。「違う」なら採用せず run に残る。
func TestServeRunsTuneAndAdoptsOnlyWithHumanMeaningCheck(t *testing.T) {
	url, specsDir, wiki, _ := newTuneFixture(t)
	code, r := postJSON(t, url+"/v1/tune/run", map[string]any{"spec": "intent"})
	if code != 202 {
		t.Fatalf("tune/run must be accepted and run in the background: %d %v", code, r)
	}
	// 終わるまで待つ(偽モデルなので一瞬)
	var st map[string]any
	for i := 0; i < 200; i++ {
		getJSON(t, url+"/v1/tune?spec=intent", &st)
		if st["running"] == false && st["latest"] != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	latest, _ := st["latest"].(map[string]any)
	if latest == nil || latest["adoptable"] != true || latest["winner"] != "strict" {
		t.Fatalf("latest tune result must be exposed with the winner and whether it may be adopted: %v", st)
	}
	if w, _ := latest["winner_wording"].(map[string]any); w == nil || !strings.Contains(w["question"].(string), "strict") {
		t.Fatalf("the page needs the winner's wording to let a person check its meaning: %v", latest)
	}
	// 「意味が違う」→ 採用しない。却下は run に残る
	code, rej := postJSON(t, url+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": latest["run"], "meaning_same": false, "note": "it now asks about tone"})
	if code != 200 || rej["adopted"] != false {
		t.Fatalf("rejecting must not adopt: %d %v", code, rej)
	}
	if _, err := os.Stat(filepath.Join(specsDir, "intent-v2.yaml")); err == nil {
		t.Fatal("rejected wording must not be written")
	}
	runs, _ := filepath.Glob(filepath.Join(wiki, "runs", "*.md"))
	if b, _ := os.ReadFile(runs[0]); !strings.Contains(string(b), "meaning") || !strings.Contains(string(b), "it now asks about tone") {
		t.Fatalf("the person's rejection and note must be recorded in the run page:\n%s", b)
	}
	// 「意味は同じ」→ 採用、新しい版、較正済み
	code, ok := postJSON(t, url+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": latest["run"], "meaning_same": true})
	if code != 200 || ok["adopted"] != true {
		t.Fatalf("adopt: %d %v", code, ok)
	}
	v2, err := spec.Load(filepath.Join(specsDir, "intent-v2.yaml"))
	if err != nil || !strings.Contains(v2.Question, "strict") || v2.CalibrationFor("m") == nil {
		t.Fatalf("adoption must write v2 and calibrate it: %v %+v", err, v2)
	}
	// 採用後の結果は adopted と出る。同じ run を二度は採用できない
	getJSON(t, url+"/v1/tune?spec=intent", &st)
	if st["latest"].(map[string]any)["adopted"] != true {
		t.Fatalf("after adoption the result must say so: %v", st)
	}
	if code, _ := postJSON(t, url+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": latest["run"], "meaning_same": true}); code != 409 {
		t.Fatalf("adopting twice must be 409, got %d", code)
	}
	// 意味の確認無しの採用は受け付けない
	if code, _ := postJSON(t, url+"/v1/tune/adopt", map[string]any{"spec": "intent", "run": latest["run"]}); code != 400 {
		t.Fatalf("adopt without meaning_same must be 400, got %d", code)
	}
}

// MCP は tune を回して結果を読めるが、採用の道具は無い(意味の確認は人の仕事)。
func TestMCPCanTuneButCannotAdopt(t *testing.T) {
	_, specsDir, _, _ := newTuneFixture(t)
	model := wordingSensitiveModel(t)
	var ef endpointFlags
	ef.model, ef.local = "m", model.URL
	h := newServer(serveOpts{specsDir: specsDir, storeDir: filepath.Join(filepath.Dir(specsDir), ".shikii"), ef: ef})
	var in, out bytes.Buffer
	send := func(req map[string]any) map[string]any {
		in.Reset()
		out.Reset()
		b, _ := json.Marshal(req)
		in.Write(append(b, '\n'))
		if err := runMCP(bufioReader(&in), &out, h); err != nil {
			t.Fatal(err)
		}
		var resp map[string]any
		_ = json.Unmarshal(out.Bytes(), &resp)
		return resp
	}
	r := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}))
	names := map[string]bool{}
	for _, tl := range r["tools"].([]any) {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	if !names["shikii_tune"] {
		t.Fatalf("shikii_tune must exist: %v", names)
	}
	for n := range names {
		if strings.Contains(n, "adopt") {
			t.Fatalf("no adopt tool may be exposed to agents: %v", names)
		}
	}
	r = result(t, send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{"name": "shikii_tune", "arguments": map[string]any{"spec": "intent"}}}))
	sc, _ := r["structuredContent"].(map[string]any)
	if r["isError"] == true || sc == nil || sc["winner"] != "strict" || sc["adoptable"] != true {
		t.Fatalf("shikii_tune must run synchronously and return the result: %v", r)
	}
}

// 画面に「聞き方」の欄がある: 結果を読み、tune を回し、人が意味を確かめて採用 / 却下する。両言語。
func TestServeUIHasWordingSection(t *testing.T) {
	url, _, _, _ := newTuneFixture(t)
	html := uiText(t, url)
	for _, must := range []string{"v1/tune?spec=", "v1/tune/run", "v1/tune/adopt", "meaning_same", "意味は同じ", "意味が違う", "same meaning", "meaning changed", "v1/tune/variant", "言い換えの案を書く", "write a rewording"} {
		if !strings.Contains(html, must) {
			t.Fatalf("UI must have the wording section; missing %q", must)
		}
	}
}

func bufioReader(b *bytes.Buffer) *bufio.Reader { return bufio.NewReader(b) }

// 人が画面で候補の言い方を書ける: POST /v1/tune/variant が spec の variants に足し(他の項目は触らない)、GET /v1/tune が並べ、次の tune が採点する。
// 名前が無い・中身が無い → 400、同じ名前 → 409、spec に無い選択肢の id → 400(検証してから書く)。
func TestServeAddsAPersonsRewordingToVariants(t *testing.T) {
	url, specsDir, _, _ := newTuneFixture(t)
	path := filepath.Join(specsDir, "intent.yaml")
	before, _ := os.ReadFile(path)
	code, r := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": "plain-words", "question": "What does this message want?", "options": map[string]string{"other": "greetings and thanks only", "": "ignored"}})
	if code != 200 || r["added"] != "plain-words" {
		t.Fatalf("adding a rewording: %d %v", code, r)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "name: plain-words") || !strings.Contains(string(after), "greetings and thanks only") || !strings.Contains(string(after), "name: strict") {
		t.Fatalf("the rewording must be saved into variants next to the existing ones:\n%s", after)
	}
	if !strings.Contains(string(after), "question: What is asked?") {
		t.Fatalf("the spec's own wording must be untouched:\n%s\nbefore:\n%s", after, before)
	}
	var st map[string]any
	getJSON(t, url+"/v1/tune?spec=intent", &st)
	vs, _ := st["variants"].([]any)
	names := ""
	for _, v := range vs {
		names += v.(map[string]any)["name"].(string) + ","
	}
	if !strings.Contains(names, "plain-words,") || !strings.Contains(names, "strict,") {
		t.Fatalf("GET /v1/tune must list the candidates: %v", st["variants"])
	}
	// 誤り: 名前なし、中身なし、同じ名前、無い選択肢
	if code, r := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "question": "x"}); code != 400 {
		t.Fatalf("no name must be 400: %d %v", code, r)
	}
	if code, r := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": "empty"}); code != 400 {
		t.Fatalf("no question and no options must be 400: %d %v", code, r)
	}
	if code, r := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": "plain-words", "question": "again"}); code != 409 {
		t.Fatalf("a duplicate name must be 409: %d %v", code, r)
	}
	if code, r := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": "bad-id", "options": map[string]string{"nope": "not an option"}}); code != 400 || !strings.Contains(fmt.Sprint(r["error"]), "nope") {
		t.Fatalf("an unknown option id must be refused before writing: %d %v", code, r)
	}
	// 名前は英数字・ハイフン・下線だけ(wiki/runs のファイル名に入る): 区切り文字や改行は 400
	for _, bad := range []string{"x/../../outside", "with space", "a\nb", strings.Repeat("n", 41)} {
		if code, _ := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": bad, "question": "q"}); code != 400 {
			t.Fatalf("name %q must be refused: %d", bad, code)
		}
	}
	if code, _ := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "intent", "name": "too-long", "question": strings.Repeat("q", 2001)}); code != 400 {
		t.Fatalf("a question over 2000 bytes must be refused: %d", code)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "bad-id") || strings.Contains(string(b), "name: empty") {
		t.Fatalf("refused rewordings must not be written:\n%s", b)
	}
	if code, _ := postJSON(t, url+"/v1/tune/variant", map[string]any{"spec": "nope", "name": "x", "question": "y"}); code != 404 {
		t.Fatalf("unknown spec must be 404: %d", code)
	}
	// 次の tune が人の候補も採点する
	if code, r := postJSON(t, url+"/v1/tune/run", map[string]any{"spec": "intent"}); code != 202 {
		t.Fatalf("tune/run: %d %v", code, r)
	}
	for i := 0; i < 200; i++ {
		getJSON(t, url+"/v1/tune?spec=intent", &st)
		if st["running"] == false && st["latest"] != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	latest, _ := st["latest"].(map[string]any)
	cands, _ := latest["candidates"].([]any)
	scored := ""
	for _, c := range cands {
		scored += c.(map[string]any)["name"].(string) + ","
	}
	if !strings.Contains(scored, "plain-words,") {
		t.Fatalf("the next tune must score the person's rewording: %v", scored)
	}
}
