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
	f.path = fakebin + ":" + toolsWithoutGo(t, "/usr/bin", "/bin", "/usr/sbin", "/sbin")
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

// toolsWithoutGo is a folder of links to the commands in dirs (the first of each name wins, as on PATH) without go and gofmt: a
// machine may have Go in /usr/bin (GitHub's runners do), and the installer must be shown to work without it.
func toolsWithoutGo(t *testing.T, dirs ...string) string {
	t.Helper()
	tools := t.TempDir()
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if name == "go" || name == "gofmt" {
				continue
			}
			if _, err := os.Lstat(filepath.Join(tools, name)); err == nil {
				continue
			}
			if err := os.Symlink(filepath.Join(dir, name), filepath.Join(tools, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return tools
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

// runWith feeds script to shell like run does, with extraPath (stand-ins for commands) put first on PATH.
func (f installFixture) runWith(t *testing.T, shell string, script []byte, extraPath string) (string, error) {
	t.Helper()
	cmd := exec.Command(shell)
	cmd.Dir = f.work
	cmd.Stdin = bytes.NewReader(script)
	path := f.path
	if extraPath != "" {
		path = extraPath + ":" + path
	}
	cmd.Env = []string{"HOME=" + f.home, "PATH=" + path, "FAKE_RELEASES=" + f.releases}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func readInstallSh(t *testing.T) []byte {
	t.Helper()
	script, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	return script
}

// standIn writes an executable stand-in for a command into a new folder and returns the folder (to put first on PATH).
func standIn(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Ctrl-C while the archive downloads (the terminal sends SIGINT to the shell and to curl) leaves no working folder in ~/.local/bin,
// in every shell `curl | sh` may run: dash and zsh do not run the EXIT trap when a signal stops the script, bash does.
func TestInstallShRemovesItsWorkingFolderWhenInterrupted(t *testing.T) {
	ran := 0
	for _, shell := range []string{"/bin/sh", "/bin/dash", "/bin/bash", "/bin/zsh"} {
		if _, err := os.Stat(shell); err != nil {
			t.Logf("%s is not on this machine", shell)
			continue
		}
		ran++
		f := newInstallFixture(t, "0.10.0")
		fakebin := strings.SplitN(f.path, ":", 2)[0]
		curl := standIn(t, "curl", "#!/bin/sh\ncase \"$*\" in *tar.gz*) kill -INT $PPID; kill -INT $$; exit 130;; esac\nexec "+filepath.Join(fakebin, "curl")+" \"$@\"\n")
		out, err := f.runWith(t, shell, readInstallSh(t), curl)
		if err == nil {
			t.Errorf("%s: an interrupted install.sh must not succeed:\n%s", shell, out)
		}
		if entries, _ := os.ReadDir(filepath.Join(f.home, ".local", "bin")); len(entries) != 0 {
			t.Errorf("%s: an interrupted install.sh must not leave its working folder in ~/.local/bin, left %v\n%s", shell, entries, out)
		}
	}
	if ran == 0 {
		t.Skip("no shell to run install.sh with")
	}
}

// When `curl | sh` gets only part of install.sh (the connection drops), the shell runs none of it: no folder is made and nothing is
// downloaded. Every cut at a line end is tried.
func TestInstallShRunsNothingWhenOnlyPartOfItArrives(t *testing.T) {
	lines := strings.SplitAfter(string(readInstallSh(t)), "\n")
	for n := 1; n < len(lines); n++ {
		if strings.TrimSpace(strings.Join(lines[n:], "")) == "" {
			break // the rest is only blank lines: this is the whole script
		}
		f := newInstallFixture(t, "0.10.0")
		out, _ := f.runWith(t, "/bin/sh", []byte(strings.Join(lines[:n], "")), "")
		if _, err := os.Stat(filepath.Join(f.home, ".local")); err == nil {
			t.Fatalf("install.sh cut after line %d made ~/.local; a part of the script must run nothing\n%s", n, out)
		}
		if _, err := os.Stat(filepath.Join(f.releases, "requests")); err == nil {
			t.Fatalf("install.sh cut after line %d downloaded something; a part of the script must run nothing\n%s", n, out)
		}
	}
}

// When shikii cannot be moved into place after shikii-readout was, install.sh stops and says that only shikii-readout was updated
// (0.10: the old shikii-readout is no longer moved aside and back; doing so lost a person's folder there).
func TestInstallShSaysOnlyTheReadoutWasUpdatedWhenShikiiCannotBeMoved(t *testing.T) {
	f := newInstallFixture(t, "0.10.0")
	bin := filepath.Join(f.home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bin, "shikii"), fakeShikii("0.9.10"))
	out, err := f.runWith(t, "/bin/sh", readInstallSh(t), standIn(t, "mv", failShikiiMv))
	if err == nil || !strings.Contains(out, "only shikii-readout was updated; run this again") {
		t.Fatalf("install.sh must fail and say what was updated when shikii cannot be moved into place: %v\n%s", err, out)
	}
	if b, _ := os.ReadFile(filepath.Join(bin, "shikii")); string(b) != fakeShikii("0.9.10") {
		t.Fatalf("the old shikii must stay, it is %q\n%s", b, out)
	}
}

// A folder (or anything but a file) where shikii or shikii-readout goes is a person's: install.sh touches nothing and says so, and the
// folder and what is in it stay.
func TestInstallShLeavesAFolderWhereABinaryGoes(t *testing.T) {
	for _, name := range []string{"shikii", "shikii-readout"} {
		t.Run(name, func(t *testing.T) {
			f := newInstallFixture(t, "0.10.0")
			dir := filepath.Join(f.home, ".local", "bin", name)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(dir, "notes.txt"), "mine\n")
			out, err := f.runWith(t, "/bin/sh", readInstallSh(t), "")
			if err == nil || !strings.Contains(out, "is not a file; nothing was installed") {
				t.Fatalf("install.sh must stop on a folder where %s goes: %v\n%s", name, err, out)
			}
			if b, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(b) != "mine\n" {
				t.Fatalf("the person's file in the folder must stay, it is %q\n%s", b, out)
			}
			for _, other := range []string{"shikii", "shikii-readout"} {
				if fi, err := os.Lstat(filepath.Join(f.home, ".local", "bin", other)); err == nil && fi.Mode().IsRegular() {
					t.Fatalf("nothing is placed when it stops, but %s is there\n%s", other, out)
				}
			}
		})
	}
}

// failShikiiMv is a stand-in for mv that cannot move a file to ~/.local/bin/shikii (and moves everything else).
const failShikiiMv = "#!/bin/sh\nfor last; do :; done\ncase \"$last\" in */.local/bin/shikii) echo \"mv: cannot move\" >&2; exit 1;; esac\nexec /bin/mv \"$@\"\n"

// A release whose archive holds a link instead of a file is refused before anything runs or is placed: chmod 755 would otherwise change
// the mode of the person's file the link points at, and ~/.local/bin would get a link to it.
func TestInstallShRefusesAnArchiveWithALinkInsteadOfAFile(t *testing.T) {
	f := newInstallFixture(t, "0.10.0")
	secret := filepath.Join(f.home, "secret")
	writeFile(t, secret, "key\n")
	if err := os.Chmod(secret, 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"LICENSE", "LICENSE-APACHE", "LICENSE-MIT", "THIRD-PARTY-NOTICES", "shikii"} {
		body := "text\n"
		if name == "shikii" {
			body = fakeShikii("0.10.0")
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	if err := tw.WriteHeader(&tar.Header{Name: "shikii-readout", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: secret}); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	asset := "shikii_0.10.0_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	dl := filepath.Join(f.releases, "download", "v0.10.0")
	sum := sha256.Sum256(buf.Bytes())
	writeFile(t, filepath.Join(dl, asset), buf.String())
	writeFile(t, filepath.Join(dl, "SHA256SUMS"), hex.EncodeToString(sum[:])+"  "+asset+"\n")
	out, err := f.run(t)
	if err == nil {
		t.Fatalf("install.sh must refuse an archive with a link in it:\n%s", out)
	}
	if info, _ := os.Stat(secret); info.Mode().Perm() != 0o600 {
		t.Fatalf("install.sh changed the mode of the file a link in the archive points at to %o", info.Mode().Perm())
	}
	if entries, _ := os.ReadDir(filepath.Join(f.home, ".local", "bin")); len(entries) != 0 {
		t.Fatalf("nothing may be placed from an archive with a link in it, there are %v\n%s", entries, out)
	}
	if _, err := os.Stat(filepath.Join(f.home, "shikii-calls")); err == nil {
		t.Fatal("hook install must not run after the archive was refused")
	}
}
