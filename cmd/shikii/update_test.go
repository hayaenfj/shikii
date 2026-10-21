package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type updateFixture struct {
	opts                                  updateOptions
	tag                                   string
	files                                 map[string][]byte
	badHash, unsafeURL, draft, prerelease bool
	entryType                             byte
	extraPath                             string
	requests                              atomic.Int32
	downloads                             atomic.Int32
	finished                              bool
}

func fixtureUpdate(t *testing.T) *updateFixture {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "my bin")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shikii", "shikii-readout"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old-"+name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f := &updateFixture{tag: "v0.4.2", entryType: tar.TypeReg, files: map[string][]byte{
		"shikii":         []byte("#!/bin/sh\nprintf 'shikii 0.4.2\\n'\n"),
		"shikii-readout": []byte("new-readout"), "LICENSE": []byte("license"), "LICENSE-APACHE": []byte("apache"), "LICENSE-MIT": []byte("mit"), "THIRD-PARTY-NOTICES": []byte("notices"),
	}}
	f.opts = updateOptions{executable: filepath.Join(dir, "shikii"), version: "0.4.1", goos: "darwin", goarch: "arm64"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		version := strings.TrimPrefix(f.tag, "v")
		name := "shikii_" + version + "_darwin_arm64.tar.gz"
		archive := fixtureUpdateArchive(t, f)
		digest := sha256.Sum256(archive)
		sum := hex.EncodeToString(digest[:])
		if f.badHash {
			sum = strings.Repeat("0", 64)
		}
		prefix := f.opts.downloadBase + "/hayaenfj/shikii/releases/download/" + f.tag + "/"
		if f.unsafeURL {
			prefix = "https://example.invalid/"
		}
		switch {
		case r.URL.Path == "/latest":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "draft": f.draft, "prerelease": f.prerelease, "assets": []any{
				map[string]any{"name": name, "browser_download_url": prefix + name},
				map[string]any{"name": "SHA256SUMS", "browser_download_url": prefix + "SHA256SUMS"},
			}})
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			f.downloads.Add(1)
			fmt.Fprintf(w, "%s  %s\n", sum, name)
		case strings.HasSuffix(r.URL.Path, "/"+name):
			f.downloads.Add(1)
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	f.opts.latestURL = server.URL + "/latest"
	f.opts.downloadBase = server.URL
	f.opts.client = server.Client()
	f.opts.finish = func(_ context.Context, exe string) error {
		f.finished = true
		if body, _ := os.ReadFile(exe); !bytes.Equal(body, f.files["shikii"]) {
			return errors.New("本体が未更新")
		}
		if body, _ := os.ReadFile(filepath.Join(filepath.Dir(exe), "shikii-readout")); !bytes.Equal(body, f.files["shikii-readout"]) {
			return errors.New("readoutが未更新")
		}
		return nil
	}
	return f
}

func fixtureUpdateArchive(t *testing.T, f *updateFixture) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"shikii", "shikii-readout", "LICENSE", "LICENSE-APACHE", "LICENSE-MIT", "THIRD-PARTY-NOTICES"} {
		body, ok := f.files[name]
		if !ok {
			continue
		}
		h := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if name == "shikii-readout" && f.entryType != tar.TypeReg {
			h.Typeflag = f.entryType
			h.Linkname = "outside"
			h.Size = 0
			body = nil
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if f.extraPath != "" {
		if err := tw.WriteHeader(&tar.Header{Name: f.extraPath, Mode: 0o644, Size: 1}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte("x"))
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assertUpdateUnchanged(t *testing.T, f *updateFixture) {
	t.Helper()
	for _, name := range []string{"shikii", "shikii-readout"} {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(f.opts.executable), name))
		if err != nil || string(b) != "old-"+name {
			t.Fatalf("失敗時も既存の%sを保持: %q %v", name, b, err)
		}
	}
	if f.finished {
		t.Fatal("失敗時に更新後の処理を実行した")
	}
}

func TestUpdateInstallsVerifiedPairAtExecutable(t *testing.T) {
	f := fixtureUpdate(t)
	sentinel := filepath.Join(t.TempDir(), "records.jsonl")
	os.WriteFile(sentinel, []byte("keep-records"), 0o600)
	if err := runUpdate(context.Background(), f.opts); err != nil {
		t.Fatal(err)
	}
	if !f.finished {
		t.Fatal("更新後の反映を実行していない")
	}
	for _, name := range []string{"LICENSE", "LICENSE-APACHE", "LICENSE-MIT", "THIRD-PARTY-NOTICES"} {
		b, err := os.ReadFile(filepath.Join(filepath.Dir(f.opts.executable), "shikii-licenses", name))
		if err != nil || !bytes.Equal(b, f.files[name]) {
			t.Fatalf("通知の同梱: %s %v", name, err)
		}
	}
	if b, _ := os.ReadFile(sentinel); string(b) != "keep-records" {
		t.Fatal("会話の記録を変更した")
	}
	info, err := os.Stat(f.opts.executable)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatal("実行権限がない")
	}
}

func TestUpdateDoesNotDowngradeOrRedownload(t *testing.T) {
	for _, version := range []string{"0.4.2", "0.5.0"} {
		t.Run(version, func(t *testing.T) {
			f := fixtureUpdate(t)
			f.opts.version = version
			if err := runUpdate(context.Background(), f.opts); err != nil {
				t.Fatal(err)
			}
			assertUpdateUnchanged(t, f)
			if f.downloads.Load() != 0 {
				t.Fatal("最新版・新しい開発版でも配布物を取得した")
			}
		})
	}
}

func TestUpdateRejectsUnverifiedReleases(t *testing.T) {
	cases := map[string]func(*updateFixture){
		"ハッシュ不一致":   func(f *updateFixture) { f.badHash = true },
		"readout不足": func(f *updateFixture) { delete(f.files, "shikii-readout") },
		"リンク":       func(f *updateFixture) { f.entryType = tar.TypeSymlink },
		"ハードリンク":    func(f *updateFixture) { f.entryType = tar.TypeLink },
		"経路の脱出":     func(f *updateFixture) { f.extraPath = "../outside" },
		"版の不一致":     func(f *updateFixture) { f.files["shikii"] = []byte("#!/bin/sh\nprintf 'shikii 0.0.0\\n'\n") },
		"外部のURL":    func(f *updateFixture) { f.unsafeURL = true },
		"下書き":       func(f *updateFixture) { f.draft = true },
		"先行版":       func(f *updateFixture) { f.prerelease = true },
		"不正なタグ":     func(f *updateFixture) { f.tag = "v0.4.2/other" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := fixtureUpdate(t)
			change(f)
			if err := runUpdate(context.Background(), f.opts); err == nil {
				t.Fatal("不正な更新を受け入れた")
			}
			assertUpdateUnchanged(t, f)
		})
	}
}

func TestUpdateRollsBackThePairOnReplacementFailure(t *testing.T) {
	f := fixtureUpdate(t)
	f.opts.replace = func(from, to string) error {
		if to == "shikii" {
			return errors.New("合成した書き込み失敗")
		}
		return os.Rename(filepath.Join(filepath.Dir(f.opts.executable), from), filepath.Join(filepath.Dir(f.opts.executable), to))
	}
	if err := runUpdate(context.Background(), f.opts); err == nil {
		t.Fatal("書き込み失敗が成功になった")
	}
	assertUpdateUnchanged(t, f)
}

func TestUpdateRefusesCompanionLinksAndSupportsExecutableAlias(t *testing.T) {
	t.Run("リンク先へ書かない", func(t *testing.T) {
		f := fixtureUpdate(t)
		path := filepath.Join(filepath.Dir(f.opts.executable), "shikii-readout")
		outside := filepath.Join(t.TempDir(), "outside")
		os.WriteFile(outside, []byte("keep"), 0o644)
		os.Remove(path)
		os.Symlink(outside, path)
		if err := runUpdate(context.Background(), f.opts); err == nil {
			t.Fatal("readoutのリンクを受け入れた")
		}
		if b, _ := os.ReadFile(outside); string(b) != "keep" {
			t.Fatal("リンク先を変更した")
		}
		if b, _ := os.ReadFile(f.opts.executable); string(b) != "old-shikii" {
			t.Fatal("本体を変更した")
		}
	})
	t.Run("実行ファイルの実体を更新", func(t *testing.T) {
		f := fixtureUpdate(t)
		alias := filepath.Join(t.TempDir(), "shikii")
		os.Symlink(f.opts.executable, alias)
		f.opts.executable = alias
		if err := runUpdate(context.Background(), f.opts); err != nil {
			t.Fatal(err)
		}
		if info, _ := os.Lstat(alias); info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("呼び出し側のリンクを置き換えた")
		}
	})
}

func TestUpdateRejectsArgumentsAndUnsupportedPlatforms(t *testing.T) {
	if err := cmdUpdate([]string{"--yes"}); err == nil {
		t.Fatal("不要なオプションを受け入れた")
	}
	f := fixtureUpdate(t)
	f.opts.goos = "windows"
	if err := runUpdate(context.Background(), f.opts); err == nil {
		t.Fatal("未対応の配布物を探した")
	}
	if f.requests.Load() != 0 {
		t.Fatal("未対応環境で通信した")
	}
}

func TestUpdateSerializesConcurrentInstallations(t *testing.T) {
	f := fixtureUpdate(t)
	root, err := os.OpenRoot(filepath.Dir(f.opts.executable))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := updateLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := runUpdate(context.Background(), f.opts); err == nil || !strings.Contains(err.Error(), "実行中") {
		t.Fatalf("同時更新を拒否する: %v", err)
	}
	assertUpdateUnchanged(t, f)
	if f.downloads.Load() != 0 {
		t.Fatal("同時更新中に配布物を取得した")
	}
}

func TestUpdateReportsPostInstallFailureWithoutLosingBinaries(t *testing.T) {
	f := fixtureUpdate(t)
	f.opts.finish = func(context.Context, string) error { return errors.New("合成したhook設定の不備") }
	err := runUpdate(context.Background(), f.opts)
	if err == nil || !strings.Contains(err.Error(), "更新済み") {
		t.Fatalf("本体の更新と反映の失敗を区別: %v", err)
	}
	for _, name := range []string{"shikii", "shikii-readout"} {
		b, _ := os.ReadFile(filepath.Join(filepath.Dir(f.opts.executable), name))
		if !bytes.Equal(b, f.files[name]) {
			t.Fatalf("更新済みの%sを失った", name)
		}
	}
}

func TestUpdateBoundsResponsesAndHandlesOffline(t *testing.T) {
	f := fixtureUpdate(t)
	if _, err := updateGet(context.Background(), f.opts, f.opts.latestURL, 8); err == nil {
		t.Fatal("応答の上限を無視した")
	}
	f.opts.latestURL = "http://127.0.0.1:1/latest"
	if err := runUpdate(context.Background(), f.opts); err == nil {
		t.Fatal("通信失敗が成功になった")
	}
	assertUpdateUnchanged(t, f)
}

func TestUpdateCLIEntry(t *testing.T) {
	if os.Getenv("SHIKII_TEST_UPDATE_ENTRY") == "1" {
		os.Args = []string{"shikii", "update", "--yes"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestUpdateCLIEntry$")
	cmd.Env = append(os.Environ(), "SHIKII_TEST_UPDATE_ENTRY=1")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), "shikii update に引数やオプションは不要です") {
		t.Fatalf("CLIの入口を登録する: %s %v", out, err)
	}
}
