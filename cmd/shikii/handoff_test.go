package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 画面: /v1/wiki が作業記録の一覧を返し、1 件の口は画像を画像として、文字(HTML も)を text/plain で配る。置き場の外は 400、無い物は 404。
func TestServeListsHandoffAndServesFilesSafely(t *testing.T) {
	url, _, storeDir := newServeFixture(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	dir := filepath.Join(storeDir, "handoff")
	if err := os.MkdirAll(filepath.Join(dir, "audit"), 0o700); err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\nfake")
	files := map[string][]byte{"audit/report-A.md": []byte("# 監査 A\n"), "audit/before.png": png, "page.html": []byte("<script>alert(1)</script>"), "data.bin": {0, 1, 2}}
	for p, b := range files {
		if err := os.WriteFile(filepath.Join(dir, p), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(storeDir, "secret.txt"), []byte("外"), 0o600); err != nil {
		t.Fatal(err)
	}
	var list struct {
		Handoff []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
			Size int64  `json:"size"`
		} `json:"handoff"`
	}
	if getJSON(t, url+"/v1/wiki", &list) != 200 || len(list.Handoff) != 4 {
		t.Fatalf("the knowledge view must list all 4 handoff files: %+v", list.Handoff)
	}
	get := func(p string) (int, http.Header, string) {
		t.Helper()
		resp, err := http.Get(url + "/v1/handoff/file?path=" + p)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, string(b)
	}
	if code, h, body := get("audit/report-A.md"); code != 200 || !strings.HasPrefix(h.Get("Content-Type"), "text/plain") || body != "# 監査 A\n" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("text: %d %v %q", code, h, body)
	}
	if code, h, body := get("audit/before.png"); code != 200 || h.Get("Content-Type") != "image/png" || body != string(png) {
		t.Fatalf("image: %d %v", code, h)
	}
	if code, h, _ := get("page.html"); code != 200 || !strings.HasPrefix(h.Get("Content-Type"), "text/plain") || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("html must be served as plain text in a sandbox: %d %v", code, h)
	}
	if code, h, _ := get("data.bin"); code != 200 || h.Get("Content-Disposition") != "attachment" {
		t.Fatalf("other files are downloads: %d %v", code, h)
	}
	if code, _, body := get("../secret.txt"); code != 400 || strings.Contains(body, "外") {
		t.Fatalf("outside the folder: %d %q", code, body)
	}
	if code, _, _ := get("nothing.md"); code != 404 {
		t.Fatalf("missing file: %d", code)
	}
}

// MCP: shikii_wiki の handoff_* で書いて、足して、一覧して、読む。置き場の外は isError。
func TestMCPWritesAndReadsHandoff(t *testing.T) {
	send, _ := mcpSession(t, "false", map[string]float64{"change_request": 0.9, "question": 0.1, "other": 0})
	call := func(args map[string]any) (bool, string) {
		t.Helper()
		r := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "shikii_wiki", "arguments": args}}))
		text := r["content"].([]any)[0].(map[string]any)["text"].(string)
		return r["isError"] == true, text
	}
	tools := result(t, send(map[string]any{"jsonrpc": "2.0", "id": 0, "method": "tools/list"}))
	b, _ := json.Marshal(tools)
	for _, must := range []string{"handoff_write", "handoff_read", "handoff_list", "handoff_append", ".shikii/handoff/"} {
		if !strings.Contains(string(b), must) {
			t.Fatalf("tools/list must describe %q", must)
		}
	}
	if bad, text := call(map[string]any{"operation": "handoff_write", "path": "release-0.8.1/steps.md", "content": "1. 監査\n"}); bad {
		t.Fatal(text)
	}
	if bad, text := call(map[string]any{"operation": "handoff_append", "path": "release-0.8.1/steps.md", "content": "2. 公開\n"}); bad {
		t.Fatal(text)
	}
	if bad, text := call(map[string]any{"operation": "handoff_list"}); bad || !strings.Contains(text, "release-0.8.1/steps.md") {
		t.Fatalf("list: %s", text)
	}
	if bad, text := call(map[string]any{"operation": "handoff_read", "path": "release-0.8.1/steps.md"}); bad || !strings.Contains(text, "1. 監査\\n2. 公開\\n") {
		t.Fatalf("read: %s", text)
	}
	if bad, _ := call(map[string]any{"operation": "handoff_write", "path": "../escape.md", "content": "x"}); !bad {
		t.Fatal("writing outside the folder must be an error")
	}
}

// CLI: shikii handoff write / append / list / read。
func TestCLIHandoff(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	capture(t, func() error { return cmdInit(nil) })
	if fi, err := os.Stat(filepath.Join(dir, ".shikii", "handoff")); err != nil || !fi.IsDir() {
		t.Fatal("init must make .shikii/handoff/")
	}
	withStdin(t, "# 手順\n", func() {
		if out := capture(t, func() error { return cmdHandoff([]string{"write", "notes/steps.md"}) }); !strings.Contains(out, ".shikii/handoff/notes/steps.md") {
			t.Fatalf("write: %s", out)
		}
	})
	withStdin(t, "- 足した\n", func() { capture(t, func() error { return cmdHandoff([]string{"append", "notes/steps.md"}) }) })
	if out := capture(t, func() error { return cmdHandoff([]string{"list"}) }); !strings.Contains(out, "notes/steps.md") {
		t.Fatalf("list: %s", out)
	}
	if out := capture(t, func() error { return cmdHandoff([]string{"read", "notes/steps.md"}) }); out != "# 手順\n- 足した\n" {
		t.Fatalf("read: %q", out)
	}
	if err := cmdHandoff([]string{"read", "../x"}); err == nil {
		t.Fatal("outside the folder must fail")
	}
}

func withStdin(t *testing.T, s string, fn func()) {
	t.Helper()
	old := stdin
	stdin = strings.NewReader(s)
	defer func() { stdin = old }()
	fn()
}
