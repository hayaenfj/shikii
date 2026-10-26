package main

import "testing"

// 0.9.5 check: MCP and the page take --local / --hosted, but decisions they make through hookDecide (reflect place: where a lesson goes) used the
// hook's fixed model URL, so `shikii mcp --local <other model>` still asked http://127.0.0.1:8080 for those (and a page it started got :8080 too).
// The URLs given to mcp and serve are now the ones hookDecide uses in that process.
func TestMCPLocalReachesHookDecisions(t *testing.T) {
	oldLocal, oldHosted := hookLocalURL, hookHostedURL
	t.Cleanup(func() { hookLocalURL, hookHostedURL = oldLocal, oldHosted })
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	hookLocalURL, hookHostedURL = "http://127.0.0.1:8080", "https://api.typesafe.ai"
	if _, err := runMCPProcess(t, []string{"--local", "http://127.0.0.1:47391", "--hosted", "https://example.invalid/decisions"}, mcpInit(1)); err != nil {
		t.Fatal(err)
	}
	if hookLocalURL != "http://127.0.0.1:47391" || hookHostedURL != "https://example.invalid/decisions" {
		t.Fatalf("mcp's --local and --hosted reach the decisions made through hookDecide: local=%s hosted=%s", hookLocalURL, hookHostedURL)
	}
}

// The page (serve) does the same; a store without specs makes it return right after reading its flags, so no page is started.
func TestServeLocalReachesHookDecisions(t *testing.T) {
	oldLocal := hookLocalURL
	t.Cleanup(func() { hookLocalURL = oldLocal })
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	hookLocalURL = "http://127.0.0.1:8080"
	if err := cmdServe([]string{"--store", t.TempDir(), "--local", "http://127.0.0.1:47392"}); err == nil {
		t.Fatal("a store without specs is refused before a page starts")
	}
	if hookLocalURL != "http://127.0.0.1:47392" {
		t.Fatalf("serve's --local reaches the decisions made through hookDecide: %s", hookLocalURL)
	}
}
