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
	reset                             func(context.Context, string, [3]uint64, [3]uint64) error // re-initializes the projects' records when crossing a version (old version, new version)
}

// storeResetAt is the version at which an update crossing it deletes everything inside each project's .shikii/ and re-initializes it (the owner's decision, 2026-10-23).
var storeResetAt = [3]uint64{0, 7, 0}

func versionString(v [3]uint64) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// currentVersion is this binary's version (shikii.Version).
func currentVersion() [3]uint64 {
	v, err := updateVersion(shikii.Version)
	if err != nil {
		return [3]uint64{}
	}
	return v
}

// crossesStoreReset reports whether from < storeResetAt <= to.
func crossesStoreReset(from, to [3]uint64) bool {
	return updateNewer(storeResetAt, from) && !updateNewer(storeResetAt, to)
}

// resetProjectStores, on an update crossing 0.7.0, deletes everything inside each registered project's .shikii/ and re-initializes it with the new binary.
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
	fmt.Fprintf(stdout, "Updating to %s: deleting everything inside .shikii/ of %d initialized projects and re-initializing them.\n", versionString(to), len(projects))
	// Note: only the old binary running update (before 0.7.0) crosses 0.7.0 in this function. In a new binary, resetStoresOnFirstRun makes the same decision.
	var targets []projectEntry
	var refused []string
	for _, p := range planWipe(projects) {
		switch p.Action {
		case wipeDo:
			targets = append(targets, p.Project)
		case wipeRefuse:
			refused = append(refused, p.Project.Path+": "+p.Reason)
		}
	}
	if err := wipeProjectStores(targets, reinitWith(ctx, exe)); err != nil {
		return err
	}
	if len(refused) > 0 { // if any were refused, do not write the mark (the next hook install --yes tries again; same as resetStoresOnFirstRun)
		return errors.New(strings.Join(refused, "\n"))
	}
	// Also set the mark to the new version (so the new binary's hook install, called next, does not delete again).
	return writeInstalledVersion(common, to)
}

// installedVersionPath is the "last installed version" mark kept on the shared side. Written from 0.7.0 on (0.6.0 and earlier never wrote it, so a missing mark means 0.0.0).
func installedVersionPath(common string) string { return filepath.Join(common, "version") }

// installedVersion reads the shared mark. Missing means 0.0.0 (0.6.0 or earlier); unreadable or corrupt gives broken=true (audit R2: must not treat it as 0.0.0 and delete everything).
func installedVersion(common string) (v [3]uint64, broken bool) {
	b, err := os.ReadFile(installedVersionPath(common))
	if err != nil {
		if os.IsNotExist(err) {
			return [3]uint64{}, false
		}
		return [3]uint64{}, true
	}
	v, err = updateVersion(strings.TrimSpace(string(b)))
	if err != nil {
		return [3]uint64{}, true
	}
	return v, false
}

func writeInstalledVersion(common string, v [3]uint64) error {
	if err := os.MkdirAll(common, 0o755); err != nil {
		return err
	}
	return os.WriteFile(installedVersionPath(common), []byte(versionString(v)+"\n"), 0o644)
}

// resetStoresOnFirstRun runs when a new binary first runs `hook install --yes` (an update from 0.6.0 or earlier calls the new binary's
// hook install --yes at the end). If the previously installed version (the shared mark) crosses storeResetAt, it deletes everything inside each project's .shikii/,
// re-initializes it, and writes the current version to the mark. update's reset runs inside the old binary running update, so
// for an update from 0.6.0 this function does the deleting (noticed on 2026-10-23). Returns true when it deleted.
func resetStoresOnFirstRun(common string, to [3]uint64, reinit func(path string) error) (bool, error) {
	from, broken := installedVersion(common)
	if broken {
		return false, fmt.Errorf("%s: the version mark cannot be read, so no project's .shikii/ will be deleted. Check its contents and either rewrite it with the current version (%s) or delete it, then run shikii hook install --yes", installedVersionPath(common), versionString(to))
	}
	wiped := false
	if crossesStoreReset(from, to) {
		plan := planWipe(registeredProjects(common))
		var targets []projectEntry
		for _, p := range plan {
			if p.Action == wipeDo {
				targets = append(targets, p.Project)
			}
		}
		if len(targets) > 0 {
			fmt.Fprintf(stdout, "Updating to %s: deleting everything inside .shikii/ of %d initialized projects and re-initializing them.\n", versionString(to), len(targets))
			if err := wipeProjectStores(targets, reinit); err != nil {
				return false, err
			}
			wiped = true
		}
		var refused []string
		for _, p := range plan {
			if p.Action == wipeRefuse {
				refused = append(refused, filepath.Join(p.Project.Path, ".shikii")+": "+p.Reason)
			}
		}
		if len(refused) > 0 { // do not write the mark while refused ones remain (after cleaning up and running again, finished ones are skipped by each project's mark and only the rest are deleted)
			return wiped, errors.New(strings.Join(refused, "\n"))
		}
	}
	if from != to {
		if err := writeInstalledVersion(common, to); err != nil {
			return wiped, err
		}
	}
	return wiped, nil
}

// wipeAction is how each project is handled in the deletion when crossing 0.7.0. Display (hook install) and execution use the same decision (audit R2: display and reality used to disagree).
type wipeAction int

const (
	wipeDo     wipeAction = iota // delete and re-initialize
	wipeDone                     // already done: created (or re-initialized) by --init of 0.7.5 or later
	wipeRefuse                   // do not delete (shared store, not a regular folder, or has unknown entries)
)

type wipePlan struct {
	Project projectEntry
	Action  wipeAction
	Reason  string
}

func planWipe(projects []projectEntry) []wipePlan {
	var out []wipePlan
	for _, p := range projects {
		store := filepath.Join(p.Path, ".shikii")
		item := wipePlan{Project: p, Action: wipeDo}
		switch {
		case p.InitVersion >= projectInitVersionNow:
			item.Action, item.Reason = wipeDone, "already initialized by this version"
		case isSharedPlaceScope(p.Path):
			item.Action, item.Reason = wipeRefuse, "not deleted because it is the shared store (home)"
		default:
			info, err := os.Lstat(store)
			if err != nil {
				item.Action, item.Reason = wipeRefuse, err.Error()
				break
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				item.Action, item.Reason = wipeRefuse, "not deleted because it is not a regular folder"
				break
			}
			entries, err := os.ReadDir(store)
			if err != nil {
				item.Action, item.Reason = wipeRefuse, err.Error()
				break
			}
			var unknown []string
			for _, e := range entries {
				if !storeEntries[e.Name()] && e.Name() != "project.json" && !disposableEntry(e.Name()) {
					unknown = append(unknown, e.Name())
				}
			}
			if len(unknown) > 0 {
				item.Action, item.Reason = wipeRefuse, "not deleted because it has entries shikii did not create ("+strings.Join(unknown, ", ")+"). Clean them up by hand, then run shikii hook install --yes"
			}
		}
		out = append(out, item)
	}
	return out
}

// reinitWith re-initializes by running `exe --init` in path.
func reinitWith(ctx context.Context, exe string) func(path string) error {
	return func(path string) error {
		cmd := exec.CommandContext(ctx, exe, "--init")
		cmd.Dir = path
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: cannot re-initialize: %v %s", path, err, strings.TrimSpace(string(out)))
		}
		fmt.Fprintf(stdout, "  %s: deleted and re-initialized\n", path)
		return nil
	}
}

// storeEntries are the entries shikii creates in each project's .shikii/ (allow list). Only these are deleted; a project with unknown entries is not deleted.
var storeEntries = map[string]bool{"decisions": true, "wiki": true, "records": true, "labels": true, "sessions": true, "guards": true, "handoff": true}

// disposableEntry is a stray that shikii did not create but that may be deleted: Finder's .DS_Store and temp files --init left behind midway (audit R3).
func disposableEntry(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, ".project-init-")
}

// isSharedPlaceScope reports whether scope is the parent of the shared store (~/.shikii/agent), i.e. home. Deleting it would remove the hook, index and marks too, so it is neither initialized nor deleted (audit 2026-10-23).
// Compares the real files (SameFile): a path spelled differently on a case-insensitive FS, or reached through a link, does not slip through (audit R2).
func isSharedPlaceScope(scope string) bool {
	agent, err := agentHome()
	if err != nil {
		return false
	}
	shared := filepath.Dir(filepath.Dir(agent))
	if scope == shared || feedbackScope(scope) == feedbackScope(shared) {
		return true
	}
	a, err1 := os.Stat(scope)
	b, err2 := os.Stat(shared)
	return err1 == nil && err2 == nil && os.SameFile(a, b)
}

// wipeProjectStores deletes, inside each project's .shikii/ (a regular folder), the entries shikii created (storeEntries) and re-initializes with reinit.
// Nothing outside .shikii/ is touched. project.json is kept (so the project stays registered even if re-initializing fails). A project with unknown entries is not deleted.
// One failure does not stop it: it tries all of them and returns every failed path together.
func wipeProjectStores(projects []projectEntry, reinit func(path string) error) error {
	var failed []string
	for _, p := range projects {
		store := filepath.Join(p.Path, ".shikii")
		if isSharedPlaceScope(p.Path) {
			failed = append(failed, store+": not deleted because it is the shared store (home)")
			continue
		}
		info, err := os.Lstat(store)
		if err != nil {
			failed = append(failed, store+": "+err.Error())
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			failed = append(failed, store+": not deleted because it is not a regular folder")
			continue
		}
		entries, err := os.ReadDir(store)
		if err != nil {
			failed = append(failed, store+": "+err.Error())
			continue
		}
		unknown := []string{}
		for _, e := range entries {
			if !storeEntries[e.Name()] && e.Name() != "project.json" && !disposableEntry(e.Name()) {
				unknown = append(unknown, e.Name())
			}
		}
		if len(unknown) > 0 {
			failed = append(failed, fmt.Sprintf("%s: not deleted because it has entries shikii did not create (%s). Clean them up by hand, then run shikii --init", store, strings.Join(unknown, ", ")))
			continue
		}
		removed := true
		for _, e := range entries {
			if e.Name() == "project.json" {
				continue
			}
			if err := os.RemoveAll(filepath.Join(store, e.Name())); err != nil {
				failed = append(failed, filepath.Join(store, e.Name())+": "+err.Error())
				removed = false
				break
			}
		}
		if !removed {
			continue
		}
		if err := reinit(p.Path); err != nil {
			failed = append(failed, fmt.Sprintf("%s: deleted but could not re-initialize (%v). Run shikii --init in that folder", p.Path, err))
		}
	}
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "\n"))
	}
	return nil
}

func cmdUpdate(args []string) error {
	if len(args) != 0 {
		return errors.New("shikii update takes no arguments or options")
	}
	exe, err := os.Executable()
	if err != nil {
		return errors.New("cannot determine where the executable is")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return runUpdate(ctx, defaultUpdateOptions(exe))
}

func defaultUpdateOptions(exe string) updateOptions {
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || req.URL.Scheme != "https" {
			return errors.New("cannot verify the redirect to the release source")
		}
		switch req.URL.Hostname() {
		case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
			return nil
		default:
			return errors.New("cannot verify the redirect to the release source")
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
		return fmt.Errorf("no release is provided for this platform (%s/%s)", o.goos, o.goarch)
	}
	current, err := updateVersion(o.version)
	if err != nil {
		return errors.New("cannot determine the current version")
	}
	exe, err := filepath.EvalSymlinks(o.executable)
	if err != nil {
		return errors.New("cannot resolve the real executable to update")
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(exe))
	if err != nil {
		return errors.New("cannot open the executable's folder")
	}
	defer root.Close()
	fmt.Fprintln(stdout, "Checking for the latest version…")
	data, err := updateGet(ctx, o, o.latestURL, 1<<20)
	if err != nil {
		return err
	}
	var release updateRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return errors.New("cannot read the latest release information")
	}
	next, err := updateVersion(release.Tag)
	if err != nil || !strings.HasPrefix(release.Tag, "v") || release.Draft || release.Prerelease {
		return errors.New("cannot find stable release information")
	}
	if next == current || !updateNewer(next, current) {
		if next == current {
			fmt.Fprintf(stdout, "shikii %s is the latest version. Nothing changed.\n", o.version)
		} else {
			fmt.Fprintf(stdout, "Local shikii %s is newer than the published stable release %s, so nothing is changed.\n", o.version, strings.TrimPrefix(release.Tag, "v"))
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
			return errors.New("cannot verify the official release URL")
		}
		assets[asset.Name], digests[asset.Name] = asset.URL, asset.Digest
	}
	if assets[filename] == "" || assets["SHA256SUMS"] == "" {
		return errors.New("the release for this platform and SHA256SUMS are not both available")
	}
	sums, err := updateGet(ctx, o, assets["SHA256SUMS"], 512<<10)
	if err != nil {
		return err
	}
	if !updateDigest(sums, digests["SHA256SUMS"]) {
		return errors.New("the SHA256SUMS hash does not match")
	}
	expectedHash, err := updateChecksum(sums, filename)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Downloading shikii %s…\n", version)
	archive, err := updateGet(ctx, o, assets[filename], 128<<20)
	if err != nil {
		return err
	}
	actual := sha256.Sum256(archive)
	if hex.EncodeToString(actual[:]) != expectedHash || !updateDigest(archive, digests[filename]) {
		return errors.New("the release hash does not match. The existing executable was kept")
	}
	files, err := updateArchive(archive)
	if err != nil {
		return err
	}
	if err := installUpdate(ctx, root, exe, version, files, o.replace); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Updated shikii %s → %s.\n", o.version, version)
	if o.reset != nil {
		if err := o.reset(ctx, exe, current, next); err != nil {
			return fmt.Errorf("shikii and readout were updated, but re-initializing the projects could not be completed: %w", err)
		}
	}
	if o.finish != nil {
		if err := o.finish(ctx, exe); err != nil {
			return fmt.Errorf("shikii and readout were updated, but applying the update could not be completed: %w", err)
		}
	}
	return nil
}

func updateVersion(value string) ([3]uint64, error) {
	var result [3]uint64
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != 3 {
		return result, errors.New("invalid version")
	}
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return result, errors.New("invalid version")
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return result, errors.New("invalid version")
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
		return nil, errors.New("cannot parse the release source URL")
	}
	req.Header.Set("User-Agent", "shikii/"+o.version)
	req.Header.Set("Accept", "application/vnd.github+json")
	response, err := o.client.Do(req)
	if err != nil {
		return nil, errors.New("communication with the release source failed. The existing executable is kept")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cannot download from the release source (HTTP %d). Try again later", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, errors.New("cannot download the complete release")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("the release source's response exceeds the size limit")
	}
	return data, nil
}

func updateDigest(data []byte, digest string) bool {
	if digest == "" {
		return true
	} // older GitHub assets have no digest. Checking SHA256SUMS is always required.
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
			return "", errors.New("cannot recognize the SHA256SUMS format")
		}
		if strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size || result != "" {
			return "", errors.New("cannot find a unique hash for the release")
		}
		result = strings.ToLower(fields[0])
	}
	if result == "" {
		return "", errors.New("the release hash is not in SHA256SUMS")
	}
	return result, nil
}

var updateFiles = []string{"LICENSE", "LICENSE-APACHE", "LICENSE-MIT", "THIRD-PARTY-NOTICES", "shikii-readout", "shikii"}

func updateArchive(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("cannot extract the release")
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
			return nil, errors.New("cannot read the release contents")
		}
		if count >= 128 || h.Size < 0 || h.Size > 64<<20 {
			return nil, errors.New("the extracted release exceeds the size limit")
		}
		total += h.Size + 512
		if total > 256<<20 {
			return nil, errors.New("the extracted release exceeds the size limit")
		}
		if filepath.Base(h.Name) != h.Name || h.Name == "." || h.Name == ".." || strings.ContainsAny(h.Name, "/\\") || seen[h.Name] || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA) {
			return nil, errors.New("the release contains invalid paths, duplicates or links")
		}
		seen[h.Name] = true
		if !containsString(updateFiles, h.Name) {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil || len(body) == 0 {
			return nil, errors.New("cannot read the required files in the release")
		}
		result[h.Name] = body
	}
	for _, name := range updateFiles {
		if len(result[name]) == 0 {
			return nil, fmt.Errorf("the release has no %s", name)
		}
	}
	return result, nil
}

func updateLock(root *os.Root) (*os.File, error) {
	const name = ".shikii-update.lock"
	if info, err := root.Lstat(name); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("the update lock cannot be a link")
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, errors.New("cannot write to the install location. Put shikii in a place you have write permission for")
	}
	info, err := f.Stat()
	named, nerr := root.Lstat(name)
	if err != nil || nerr != nil || !named.Mode().IsRegular() || !os.SameFile(info, named) {
		f.Close()
		return nil, errors.New("cannot check the update lock")
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("another shikii update is running. Try again after it finishes")
	}
	return f, nil
}

// installUpdate prepares everything on the same file system and replaces the binary last.
// If it fails midway, the files already replaced are restored from the backup.
func installUpdate(ctx context.Context, root *os.Root, exe, version string, files map[string][]byte, replace func(string, string) error) error {
	stage, err := os.MkdirTemp(filepath.Dir(exe), ".shikii-update-")
	if err != nil {
		return errors.New("cannot write to the install location. The existing executable was kept")
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
			return errors.New("cannot prepare the update files")
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, filepath.Join(stage, "new-shikii"), "--version").Output()
	if err != nil || strings.TrimSpace(string(out)) != "shikii "+version {
		return errors.New("cannot confirm that the downloaded shikii starts and has the right version. The existing executable was kept")
	}
	licenseCreated := false
	if info, err := root.Lstat("shikii-licenses"); os.IsNotExist(err) {
		if err := root.Mkdir("shikii-licenses", 0o755); err != nil {
			return errors.New("cannot prepare the license destination")
		}
		licenseCreated = true
	} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("the license destination cannot be a link")
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
				return fmt.Errorf("update target %s is not a regular file", target)
			}
			p.info = info
			old, err := root.Open(target)
			if err != nil {
				return errors.New("cannot move the existing executable aside")
			}
			opened, serr := old.Stat()
			if serr != nil || !os.SameFile(info, opened) {
				old.Close()
				return errors.New("the update target changed midway")
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
				return errors.New("cannot finish moving the existing files aside")
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
				return errors.New("the update target was created midway")
			}
		} else if err != nil || !os.SameFile(p.info, info) {
			return errors.New("the update target changed midway")
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
			return fmt.Errorf("the update and the restore both failed. The moved-aside files are kept at: %s", stage)
		}
		return errors.New("could not write the update target. Restored the existing executable")
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
			return errors.New("cannot check the hook settings")
		}
		var config struct {
			Hooks map[string][]struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if json.Unmarshal(body, &config) != nil {
			return errors.New("cannot read the hook settings. Check them with shikii hook install --yes")
		}
		for _, groups := range config.Hooks {
			for _, group := range groups {
				for _, hook := range group.Hooks {
					if isShikiiHook(hook.Command) {
						bin, _ := hookBinary(hook.Command)
						actual, _ := filepath.EvalSymlinks(bin)
						if actual == exe {
							installed = true
						}
					}
				}
			}
		}
	}
	if installed {
		cmd := exec.CommandContext(ctx, exe, "hook", "install", "--yes")
		cmd.Stdout, cmd.Stderr = stdout, stdout // pass the new binary's deletion targets and failure reasons through to the update output (audit R2: they used to be discarded)
		if err := cmd.Run(); err != nil {
			return errors.New("cannot update the hook and the working rules. Retry with shikii hook install --yes")
		}
		fmt.Fprintln(stdout, "Applied to the hook and the agents' working rules.")
	}
	fmt.Fprintln(stdout, "A running page or MCP switches to the new version when restarted.")
	return nil
}
