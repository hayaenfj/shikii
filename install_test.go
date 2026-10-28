package shikii

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeCurl stands in for curl on PATH: it answers GitHub's release URLs from the folder in FAKE_RELEASES, so install.sh runs
// without the network. /releases/latest redirects to the tag named in FAKE_RELEASES/latest, like GitHub does.
const fakeCurl = `#!/bin/sh
out= ; w= ; url=
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2 ;;
    -w) w=$2; shift 2 ;;
    -*) shift ;;
    *) url=$1; shift ;;
  esac
done
echo "$url" >> "$FAKE_RELEASES/requests"
base=https://github.com/hayaenfj/shikii/releases
case "$url" in
  "$base/latest") [ -n "$w" ] && printf '%s' "$base/tag/$(cat "$FAKE_RELEASES/latest")"; exit 0 ;;
  "$base/download/"*) f="$FAKE_RELEASES/download/${url#"$base/download/"}"; [ -f "$f" ] || exit 22; cp "$f" "$out" ;;
  *) exit 6 ;;
esac
`

// fakeShikii stands in for the released binary: it reports its version and records every other call (hook install --yes)
// instead of touching the agents' settings or starting anything.
func fakeShikii(version string) string {
	return "#!/bin/sh\nif [ \"$1\" = --version ]; then echo \"shikii " + version + "\"; exit 0; fi\necho \"$0 $*\" >> \"$HOME/shikii-calls\"\n"
}

type installFixture struct {
	home, work, releases, path string
}

// newInstallFixture lays out a release v<version> for this machine the way scripts/build-release.py does (a flat tar.gz with
// the binaries and the license files, and SHA256SUMS), a temporary HOME, and a PATH without Go.
func newInstallFixture(t *testing.T, version string) installFixture {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("install.sh supports macOS and Linux")
	}
	f := installFixture{home: t.TempDir(), work: t.TempDir(), releases: t.TempDir()}
	fakebin := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakebin, "curl"), []byte(fakeCurl), 0o755); err != nil {
		t.Fatal(err)
	}
	f.path = fakebin + ":/usr/bin:/bin:/usr/sbin:/sbin"
	for _, dir := range filepath.SplitList(f.path) {
		if _, err := os.Stat(filepath.Join(dir, "go")); err == nil {
			t.Fatalf("the PATH for this test must not have Go, but %s has it", dir)
		}
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string]string{"shikii": fakeShikii(version), "shikii-readout": "#!/bin/sh\n", "LICENSE": "license\n", "LICENSE-APACHE": "apache\n", "LICENSE-MIT": "mit\n", "THIRD-PARTY-NOTICES": "notices\n"}
	for _, name := range []string{"LICENSE", "LICENSE-APACHE", "LICENSE-MIT", "THIRD-PARTY-NOTICES", "shikii", "shikii-readout"} {
		mode := int64(0o644)
		if strings.HasPrefix(name, "shikii") {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(files[name])), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(files[name]))
	}
	tw.Close()
	gz.Close()
	asset := "shikii_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	dl := filepath.Join(f.releases, "download", "v"+version)
	if err := os.MkdirAll(dl, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	writeFile(t, filepath.Join(dl, asset), buf.String())
	writeFile(t, filepath.Join(dl, "SHA256SUMS"), hex.EncodeToString(sum[:])+"  "+asset+"\n")
	writeFile(t, filepath.Join(f.releases, "latest"), "v"+version)
	return f
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// run feeds install.sh to sh on stdin, as README's `curl -fsSL .../install.sh | sh` does, from a folder that is not a checkout.
func (f installFixture) run(t *testing.T) (string, error) {
	t.Helper()
	script, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh")
	cmd.Dir = f.work
	cmd.Stdin = bytes.NewReader(script)
	cmd.Env = []string{"HOME=" + f.home, "PATH=" + f.path, "FAKE_RELEASES=" + f.releases}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The one line in the README installs the release that GitHub's /releases/latest names (not the Go module proxy, which lagged
// behind a new release), without Go, into the fixed ~/.local/bin, and then runs hook install --yes with that binary.
func TestInstallShInstallsTheLatestReleaseWithoutGo(t *testing.T) {
	f := newInstallFixture(t, "0.10.0")
	out, err := f.run(t)
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	bin := filepath.Join(f.home, ".local", "bin")
	if got, err := exec.Command(filepath.Join(bin, "shikii"), "--version").Output(); err != nil || strings.TrimSpace(string(got)) != "shikii 0.10.0" {
		t.Fatalf("~/.local/bin/shikii must be the latest release 0.10.0: %q %v\n%s", got, err, out)
	}
	for _, p := range []string{"shikii-readout", filepath.Join("shikii-licenses", "THIRD-PARTY-NOTICES")} {
		if _, err := os.Stat(filepath.Join(bin, p)); err != nil {
			t.Fatalf("install.sh must put %s in ~/.local/bin: %v\n%s", p, err, out)
		}
	}
	calls, _ := os.ReadFile(filepath.Join(f.home, "shikii-calls"))
	if strings.TrimSpace(string(calls)) != filepath.Join(bin, "shikii")+" hook install --yes" {
		t.Fatalf("install.sh must run ~/.local/bin/shikii hook install --yes once, ran: %q\n%s", calls, out)
	}
	requests, _ := os.ReadFile(filepath.Join(f.releases, "requests"))
	if !strings.HasPrefix(string(requests), "https://github.com/hayaenfj/shikii/releases/latest\n") {
		t.Fatalf("install.sh must ask GitHub's /releases/latest first, asked:\n%s", requests)
	}
	if !strings.Contains(out, "Add this to your PATH: "+bin) {
		t.Fatalf("install.sh must say that ~/.local/bin is not on PATH:\n%s", out)
	}
	if entries, _ := os.ReadDir(bin); len(entries) != 3 {
		t.Fatalf("install.sh must leave only shikii, shikii-readout and shikii-licenses in ~/.local/bin, left %v", entries)
	}
}

// An archive whose hash is not the one in SHA256SUMS is not installed, and hook install is not run.
func TestInstallShRefusesAnArchiveThatDoesNotMatchSHA256SUMS(t *testing.T) {
	f := newInstallFixture(t, "0.10.0")
	sums := filepath.Join(f.releases, "download", "v0.10.0", "SHA256SUMS")
	body, _ := os.ReadFile(sums)
	writeFile(t, sums, strings.Repeat("0", 64)+string(body[64:]))
	out, err := f.run(t)
	if err == nil || !strings.Contains(out, "does not match SHA256SUMS") {
		t.Fatalf("install.sh must stop on a hash that does not match: %v\n%s", err, out)
	}
	bin := filepath.Join(f.home, ".local", "bin")
	if entries, _ := os.ReadDir(bin); len(entries) != 0 {
		t.Fatalf("nothing may be left in ~/.local/bin, left %v", entries)
	}
	if _, err := os.Stat(filepath.Join(f.home, "shikii-calls")); err == nil {
		t.Fatal("hook install must not run after a hash mismatch")
	}
}
