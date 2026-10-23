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
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	shikii "github.com/hayaenfj/shikii"
)

type updateOptions struct {
	executable, version, goos, goarch string
	latestURL, downloadBase           string
	client                            *http.Client
	finish                            func(context.Context, string) error
	replace                           func(string, string) error
	reset                             func(context.Context, string, [3]uint64, [3]uint64) error // 版を跨ぐ時にプロジェクトの記録を初期化し直す(前の版、後の版)
}

// storeResetAt は、ここを跨ぐ更新で各プロジェクトの .shikii/ の中を全部消して初期化し直す版(本人の決定、2026-10-23)。
var storeResetAt = [3]uint64{0, 7, 0}

func versionString(v [3]uint64) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// currentVersion はこの本体の版(shikii.Version)。
func currentVersion() [3]uint64 {
	v, err := updateVersion(shikii.Version)
	if err != nil {
		return [3]uint64{}
	}
	return v
}

// crossesStoreReset は from < storeResetAt <= to か。
func crossesStoreReset(from, to [3]uint64) bool {
	return updateNewer(storeResetAt, from) && !updateNewer(storeResetAt, to)
}

// resetProjectStores は 0.7.0 を跨ぐ更新で、登録済みの各プロジェクトの .shikii/ の中を全部消し、新しい本体で初期化し直す。
func resetProjectStores(ctx context.Context, exe string, from, to [3]uint64) error {
	if !crossesStoreReset(from, to) {
		return nil
	}
	common, err := agentHome()
	if err != nil {
		return err
	}
	projects := registeredProjects(common)
	if len(projects) == 0 {
		return nil
	}
	fmt.Fprintf(stdout, "%s への更新: 初期化済みの %d プロジェクトの .shikii/ の中を全部消して初期化し直します。\n", versionString(to), len(projects))
	if err := wipeProjectStores(projects, reinitWith(ctx, exe)); err != nil {
		return err
	}
	// 印も新しい版にしておく(続いて呼ばれる新しい本体の hook install がもう一度消さないように)。
	return writeInstalledVersion(common, to)
}

// installedVersionPath は共通側に置く「最後に入れた版」の印。0.7.0 から書く(0.6.0 以前は書かないので、無ければ 0.0.0 とみなす)。
func installedVersionPath(common string) string { return filepath.Join(common, "version") }

func installedVersion(common string) [3]uint64 {
	b, err := os.ReadFile(installedVersionPath(common))
	if err != nil {
		return [3]uint64{}
	}
	v, err := updateVersion(strings.TrimSpace(string(b)))
	if err != nil {
		return [3]uint64{}
	}
	return v
}

func writeInstalledVersion(common string, v [3]uint64) error {
	if err := os.MkdirAll(common, 0o755); err != nil {
		return err
	}
	return os.WriteFile(installedVersionPath(common), []byte(versionString(v)+"\n"), 0o644)
}

// resetStoresOnFirstRun は、新しい本体が初めて `hook install --yes` を実行した時(0.6.0 以前の update は最後に新しい本体の
// hook install --yes を呼ぶ)に、前に入っていた版(共通側の印)から storeResetAt を跨いでいれば各プロジェクトの .shikii/ の中を
// 全部消して初期化し直し、今の版を印に書く。update の reset は update を実行している古い本体の中で動くので、
// 0.6.0 からの更新ではこちらが消す(2026-10-23 に気付いた)。消した時 true。
func resetStoresOnFirstRun(common string, to [3]uint64, reinit func(path string) error) (bool, error) {
	from := installedVersion(common)
	wiped := false
	if crossesStoreReset(from, to) {
		if projects := registeredProjects(common); len(projects) > 0 {
			fmt.Fprintf(stdout, "%s への更新: 初期化済みの %d プロジェクトの .shikii/ の中を全部消して初期化し直します。\n", versionString(to), len(projects))
			if err := wipeProjectStores(projects, reinit); err != nil {
				return false, err
			}
			wiped = true
		}
	}
	if from != to {
		if err := writeInstalledVersion(common, to); err != nil {
			return wiped, err
		}
	}
	return wiped, nil
}

// reinitWith は path で `exe --init` を実行して初期化し直す。
func reinitWith(ctx context.Context, exe string) func(path string) error {
	return func(path string) error {
		cmd := exec.CommandContext(ctx, exe, "--init")
		cmd.Dir = path
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: 初期化し直せません: %v %s", path, err, strings.TrimSpace(string(out)))
		}
		fmt.Fprintf(stdout, "  %s: 消して初期化し直しました\n", path)
		return nil
	}
}

// wipeProjectStores は各プロジェクトの .shikii/(通常のフォルダ)の中を全部消し、reinit で初期化し直す。.shikii/ の外は触らない。
func wipeProjectStores(projects []projectEntry, reinit func(path string) error) error {
	for _, p := range projects {
		store := filepath.Join(p.Path, ".shikii")
		info, err := os.Lstat(store)
		if err != nil {
			return fmt.Errorf("%s: %w", store, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: 通常のフォルダではないので消しません", store)
		}
		entries, err := os.ReadDir(store)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(store, e.Name())); err != nil {
				return fmt.Errorf("%s: %w", filepath.Join(store, e.Name()), err)
			}
		}
		if err := reinit(p.Path); err != nil {
			return err
		}
	}
	return nil
}

func cmdUpdate(args []string) error {
	if len(args) != 0 {
		return errors.New("shikii update に引数やオプションは不要です")
	}
	exe, err := os.Executable()
	if err != nil {
		return errors.New("実行ファイルの場所を確認できません")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return runUpdate(ctx, defaultUpdateOptions(exe))
}

func defaultUpdateOptions(exe string) updateOptions {
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || req.URL.Scheme != "https" {
			return errors.New("配布元への転送を確認できません")
		}
		switch req.URL.Hostname() {
		case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
			return nil
		default:
			return errors.New("配布元への転送を確認できません")
		}
	}}
	return updateOptions{
		executable: exe, version: shikii.Version, goos: runtime.GOOS, goarch: runtime.GOARCH,
		latestURL: "https://api.github.com/repos/hayaenfj/shikii/releases/latest", downloadBase: "https://github.com", client: client,
		finish: finishUpdate, reset: resetProjectStores,
	}
}

type updateRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func runUpdate(ctx context.Context, o updateOptions) error {
	if (o.goos != "darwin" && o.goos != "linux") || (o.goarch != "arm64" && o.goarch != "amd64") {
		return fmt.Errorf("この環境（%s/%s）の配布物には対応していません", o.goos, o.goarch)
	}
	current, err := updateVersion(o.version)
	if err != nil {
		return errors.New("現在の版を確認できません")
	}
	exe, err := filepath.EvalSymlinks(o.executable)
	if err != nil {
		return errors.New("更新する実行ファイルの実体を確認できません")
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(exe))
	if err != nil {
		return errors.New("実行ファイルのフォルダを開けません")
	}
	defer root.Close()
	fmt.Fprintln(stdout, "最新版を確認しています…")
	data, err := updateGet(ctx, o, o.latestURL, 1<<20)
	if err != nil {
		return err
	}
	var release updateRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return errors.New("最新版の案内を読み取れません")
	}
	next, err := updateVersion(release.Tag)
	if err != nil || !strings.HasPrefix(release.Tag, "v") || release.Draft || release.Prerelease {
		return errors.New("安定版の案内を確認できません")
	}
	if next == current || !updateNewer(next, current) {
		if next == current {
			fmt.Fprintf(stdout, "shikii %s は最新版です。変更はありません。\n", o.version)
		} else {
			fmt.Fprintf(stdout, "手元のshikii %sは公開中の安定版%sより新しいため、変更しません。\n", o.version, strings.TrimPrefix(release.Tag, "v"))
		}
		return nil
	}
	version := strings.TrimPrefix(release.Tag, "v")
	lock, err := updateLock(root)
	if err != nil {
		return err
	}
	defer lock.Close()
	filename := fmt.Sprintf("shikii_%s_%s_%s.tar.gz", version, o.goos, o.goarch)
	assets := map[string]string{}
	digests := map[string]string{}
	for _, asset := range release.Assets {
		if asset.Name != filename && asset.Name != "SHA256SUMS" {
			continue
		}
		expected := o.downloadBase + "/hayaenfj/shikii/releases/download/" + release.Tag + "/" + asset.Name
		if asset.URL != expected || assets[asset.Name] != "" {
			return errors.New("公式の配布物のURLを確認できません")
		}
		assets[asset.Name], digests[asset.Name] = asset.URL, asset.Digest
	}
	if assets[filename] == "" || assets["SHA256SUMS"] == "" {
		return errors.New("この環境の配布物とSHA256SUMSが揃っていません")
	}
	sums, err := updateGet(ctx, o, assets["SHA256SUMS"], 512<<10)
	if err != nil {
		return err
	}
	if !updateDigest(sums, digests["SHA256SUMS"]) {
		return errors.New("SHA256SUMSのハッシュが一致しません")
	}
	expectedHash, err := updateChecksum(sums, filename)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "shikii %s を取得しています…\n", version)
	archive, err := updateGet(ctx, o, assets[filename], 128<<20)
	if err != nil {
		return err
	}
	actual := sha256.Sum256(archive)
	if hex.EncodeToString(actual[:]) != expectedHash || !updateDigest(archive, digests[filename]) {
		return errors.New("配布物のハッシュが一致しません。既存の実行ファイルは保持しました")
	}
	files, err := updateArchive(archive)
	if err != nil {
		return err
	}
	if err := installUpdate(ctx, root, exe, version, files, o.replace); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "shikii %s → %s に更新しました。\n", o.version, version)
	if o.reset != nil {
		if err := o.reset(ctx, exe, current, next); err != nil {
			return fmt.Errorf("本体とreadoutは更新済みですが、プロジェクトの初期化し直しを完了できません: %w", err)
		}
	}
	if o.finish != nil {
		if err := o.finish(ctx, exe); err != nil {
			return fmt.Errorf("本体とreadoutは更新済みですが、反映を完了できません: %w", err)
		}
	}
	return nil
}

func updateVersion(value string) ([3]uint64, error) {
	var result [3]uint64
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != 3 {
		return result, errors.New("不正な版")
	}
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return result, errors.New("不正な版")
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return result, errors.New("不正な版")
			}
		}
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return result, err
		}
		result[i] = n
	}
	return result, nil
}

func updateNewer(a, b [3]uint64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func updateGet(ctx context.Context, o updateOptions, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("配布元のURLを読み取れません")
	}
	req.Header.Set("User-Agent", "shikii/"+o.version)
	req.Header.Set("Accept", "application/vnd.github+json")
	response, err := o.client.Do(req)
	if err != nil {
		return nil, errors.New("配布元との通信に失敗しました。既存の実行ファイルは保持しています")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("配布元から取得できません（HTTP %d）。時間を置いて再試行してください", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, errors.New("配布物を最後まで取得できません")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("配布元の応答が上限を超えています")
	}
	return data, nil
}

func updateDigest(data []byte, digest string) bool {
	if digest == "" {
		return true
	} // 古いGitHub添付にはdigestが無い。SHA256SUMSの照合は常に必須。
	sum := sha256.Sum256(data)
	return digest == "sha256:"+hex.EncodeToString(sum[:])
}

func updateChecksum(data []byte, name string) (string, error) {
	result := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return "", errors.New("SHA256SUMSの形式を確認できません")
		}
		if strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size || result != "" {
			return "", errors.New("配布物のハッシュを一意に確認できません")
		}
		result = strings.ToLower(fields[0])
	}
	if result == "" {
		return "", errors.New("配布物のハッシュがSHA256SUMSにありません")
	}
	return result, nil
}

var updateFiles = []string{"LICENSE", "LICENSE-APACHE", "LICENSE-MIT", "THIRD-PARTY-NOTICES", "shikii-readout", "shikii"}

func updateArchive(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("配布物を展開できません")
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, 256<<20))
	result := map[string][]byte{}
	seen := map[string]bool{}
	var total int64
	for count := 0; ; count++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("配布物の内容を読み取れません")
		}
		if count >= 128 || h.Size < 0 || h.Size > 64<<20 {
			return nil, errors.New("配布物の展開量が上限を超えています")
		}
		total += h.Size + 512
		if total > 256<<20 {
			return nil, errors.New("配布物の展開量が上限を超えています")
		}
		if filepath.Base(h.Name) != h.Name || h.Name == "." || h.Name == ".." || strings.ContainsAny(h.Name, "/\\") || seen[h.Name] || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA) {
			return nil, errors.New("配布物に不正な経路・重複・リンクがあります")
		}
		seen[h.Name] = true
		if !containsString(updateFiles, h.Name) {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil || len(body) == 0 {
			return nil, errors.New("配布物の必要なファイルを読み取れません")
		}
		result[h.Name] = body
	}
	for _, name := range updateFiles {
		if len(result[name]) == 0 {
			return nil, fmt.Errorf("配布物に%sがありません", name)
		}
	}
	return result, nil
}

func updateLock(root *os.Root) (*os.File, error) {
	const name = ".shikii-update.lock"
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("更新用のロックにリンクは使えません")
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, errors.New("インストール先へ書き込めません。書き込み権限のある場所へshikiiを配置してください")
	}
	info, err := f.Stat()
	named, nerr := root.Lstat(name)
	if err != nil || nerr != nil || !named.Mode().IsRegular() || !os.SameFile(info, named) {
		f.Close()
		return nil, errors.New("更新用のロックを確認できません")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("別のshikii updateが実行中です。完了後に再試行してください")
	}
	return f, nil
}

// installUpdate は同じファイルシステムへ全てを準備し、本体を最後に置き換える。
// 途中で失敗したら、既に置き換えたファイルをバックアップから戻す。
func installUpdate(ctx context.Context, root *os.Root, exe, version string, files map[string][]byte, replace func(string, string) error) error {
	stage, err := os.MkdirTemp(filepath.Dir(exe), ".shikii-update-")
	if err != nil {
		return errors.New("インストール先へ書き込めません。既存の実行ファイルは保持しました")
	}
	keep := false
	defer func() {
		if !keep {
			os.RemoveAll(stage)
		}
	}()
	for _, name := range updateFiles {
		mode := os.FileMode(0o644)
		if strings.HasPrefix(name, "shikii") {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(stage, "new-"+name), files[name], mode); err != nil {
			return errors.New("更新ファイルを準備できません")
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, filepath.Join(stage, "new-shikii"), "--version").Output()
	if err != nil || strings.TrimSpace(string(out)) != "shikii "+version {
		return errors.New("取得したshikiiの起動と版を確認できません。既存の実行ファイルは保持しました")
	}
	licenseCreated := false
	if info, err := root.Lstat("shikii-licenses"); os.IsNotExist(err) {
		if err := root.Mkdir("shikii-licenses", 0o755); err != nil {
			return errors.New("ライセンスの保存先を用意できません")
		}
		licenseCreated = true
	} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("ライセンスの保存先にリンクは使えません")
	}
	defer func() {
		if licenseCreated {
			_ = root.Remove("shikii-licenses")
		}
	}()
	type prior struct {
		name, target, backup string
		info                 os.FileInfo
	}
	var entries []prior
	for _, name := range updateFiles {
		target := name
		if name == "shikii" {
			target = filepath.Base(exe)
		} else if name != "shikii-readout" {
			target = filepath.Join("shikii-licenses", name)
		}
		p := prior{name: name, target: target, backup: filepath.Join(filepath.Base(stage), "old-"+name)}
		info, err := root.Lstat(target)
		if err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("更新先%sは通常のファイルではありません", target)
			}
			p.info = info
			old, err := root.Open(target)
			if err != nil {
				return errors.New("既存の実行ファイルを退避できません")
			}
			opened, serr := old.Stat()
			if serr != nil || !os.SameFile(info, opened) {
				old.Close()
				return errors.New("更新先が途中で変更されました")
			}
			backup, err := root.OpenFile(p.backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
			if err != nil {
				old.Close()
				return err
			}
			_, copyErr := io.Copy(backup, old)
			old.Close()
			syncErr := backup.Sync()
			closeErr := backup.Close()
			if copyErr != nil || syncErr != nil || closeErr != nil {
				return errors.New("既存ファイルの退避を完了できません")
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		entries = append(entries, p)
	}
	for _, p := range entries {
		info, err := root.Lstat(p.target)
		if p.info == nil {
			if !os.IsNotExist(err) {
				return errors.New("更新先が途中で作成されました")
			}
		} else if err != nil || !os.SameFile(p.info, info) {
			return errors.New("更新先が途中で変更されました")
		}
	}
	if replace == nil {
		replace = root.Rename
	}
	var installed []prior
	for _, p := range entries {
		if err := ctx.Err(); err == nil {
			err = replace(filepath.Join(filepath.Base(stage), "new-"+p.name), p.target)
			if err == nil {
				installed = append(installed, p)
				continue
			}
		}
		for i := len(installed) - 1; i >= 0; i-- {
			old := installed[i]
			var err error
			if old.info == nil {
				err = root.Remove(old.target)
			} else {
				err = root.Rename(old.backup, old.target)
			}
			if err != nil {
				keep = true
			}
		}
		if keep {
			return fmt.Errorf("更新と復元に失敗しました。退避先を保持しています: %s", stage)
		}
		return errors.New("更新先へ書き込めませんでした。既存の実行ファイルへ戻しました")
	}
	return nil
}

func finishUpdate(ctx context.Context, exe string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	installed := false
	for _, target := range hookTargets {
		body, err := os.ReadFile(filepath.Join(home, target.rel))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return errors.New("hookの設定を確認できません")
		}
		var config struct {
			Hooks map[string][]struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if json.Unmarshal(body, &config) != nil {
			return errors.New("hookの設定が読み取れません。shikii hook install --yesで確認してください")
		}
		for _, groups := range config.Hooks {
			for _, group := range groups {
				for _, hook := range group.Hooks {
					if isShikiiHook(hook.Command) {
						fields := strings.Fields(hook.Command)
						actual, _ := filepath.EvalSymlinks(fields[0])
						if actual == exe {
							installed = true
						}
					}
				}
			}
		}
	}
	if installed {
		if err := exec.CommandContext(ctx, exe, "hook", "install", "--yes").Run(); err != nil {
			return errors.New("hookと作法を更新できません。shikii hook install --yesで再試行してください")
		}
		fmt.Fprintln(stdout, "hookとエージェントの作法へ反映しました。")
	}
	fmt.Fprintln(stdout, "起動中の画面やMCPは、再起動すると新版へ切り替わります。")
	return nil
}
