package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// stubWarmMarkers maps the path of each stub writeStub made to the file its warm-up run wrote, so that a test can see that the first run
// of the stub happened inside writeStub.
var stubWarmMarkers sync.Map

// writeStub writes the shell script dir/name (body is the script without its #!/bin/sh line) with the execute bit, runs it once without
// letting it do its work, and returns its path. Use it for every file a test makes to be run (a stand-in for git, for the agent, for a
// binary), and never write such a file with os.WriteFile: TestAnExecutableWrittenOutsideTheStubHelpersIsRefused fails on that.
//
// Why the run: on macOS the first run of a file that was just written is slow, because the system looks at it before it lets it
// start (about 0.2 s alone, 13 s seen while the eight groups of make test ran together). A stand-in for git that is run for the first
// time by the hook is run under hookGitTimeout (3 s in a real hook), so the hook gave up before the script had done anything, and a
// test of git failed at random. The run here, with SHIKII_STUB_WARM set, leaves at the first line (the script only writes the marker
// file the variable names), so that the slow first run is paid here, outside any limit, and the run the test cares about is the second.
func writeStub(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\nif [ -n \"$SHIKII_STUB_WARM\" ]; then : > \"$SHIKII_STUB_WARM\"; exit 0; fi\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "warmed")
	cmd := exec.Command(path)
	cmd.Env = append(os.Environ(), "SHIKII_STUB_WARM="+marker)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("warming %s: %v\n%s", path, err, out)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the first run of %s did not leave at the warm-up line: %v", path, err)
	}
	stubWarmMarkers.Store(path, marker)
	return path
}

// writeUnrunExecutable writes a file with the execute bit that no test ever runs: something the product only looks at (doctor asking
// whether a path can run, an update that replaces a binary and keeps its mode, a registration that names a wrapper). It is not warmed,
// because a run would be a pointless start of a process, and the content need not be a script. A file that a test or the product will
// run is made by writeStub.
func writeUnrunExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

// The stub's first run happens in writeStub and does none of the stub's work; the runs after it do the work.
func TestAStubIsRunOnceInsideWriteStubWithoutDoingItsWork(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	path := writeStub(t, filepath.Join(dir, "bin"), "tool", "echo \"$*\" >> '"+log+"'\n")
	marker, ok := stubWarmMarkers.Load(path)
	if !ok {
		t.Fatalf("writeStub did not record the warm-up run of %s", path)
	}
	if _, err := os.Stat(marker.(string)); err != nil {
		t.Fatalf("the stub was not run inside writeStub (its warm-up run writes %s): %v", marker, err)
	}
	if b, _ := os.ReadFile(log); len(b) != 0 {
		t.Fatalf("the warm-up run did the stub's work: %q", b)
	}
	if out, err := exec.Command(path, "a", "b").CombinedOutput(); err != nil {
		t.Fatalf("the stub does not run after the warm-up: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(log); string(b) != "a b\n" {
		t.Fatalf("the run after the warm-up must do the stub's work once, got %q", b)
	}
}
