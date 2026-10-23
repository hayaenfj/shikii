package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 共通側はプロジェクトへの入口だけを持つ。会話や採点、線は各 .shikii/ に置く。
type projectEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	URL  string `json:"url"`
}

var errProjectInactive = errors.New("このプロジェクトでは shikii --init を実行してください")

// 設定ではなく、明示的な初期化が最後まで済んだ印。別の場所へコピーしても有効にしない。
type projectActivation struct {
	Version int    `json:"version"`
	Scope   string `json:"scope"`
}

func projectActive(root, scope string) (bool, error) {
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("初期化の保存先は通常のフォルダにしてください")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return false, err
	}
	defer dir.Close()
	opened, err := dir.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		return false, errors.New("初期化の保存先が変わりました")
	}
	markerInfo, err := dir.Lstat("project.json")
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !markerInfo.Mode().IsRegular() {
		return false, errors.New("初期化の印は通常のファイルにしてください")
	}
	f, err := dir.OpenFile("project.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || !os.SameFile(markerInfo, stat) || stat.Size() > 4096 {
		return false, errors.New("プロジェクトの初期化の印を確認できません")
	}
	var activation projectActivation
	decoder := json.NewDecoder(io.LimitReader(f, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&activation); err != nil {
		return false, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return false, errors.New("プロジェクトの初期化の印が不正です")
	}
	return activation.Version == 1 && activation.Scope == scope, nil
}

func finishProjectInit(root, scope string) error {
	// 索引が先にできても、最後の印が無ければ画面にもhookにも現れない。
	if _, err := os.UserHomeDir(); err == nil {
		if err := registerProject(scope); err != nil {
			return err
		}
	}
	marker := filepath.Join(root, "project.json")
	if info, err := os.Lstat(marker); err == nil && !info.Mode().IsRegular() {
		return errors.New("初期化の印にリンクは使えません")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	data, _ := json.Marshal(projectActivation{Version: 1, Scope: scope})
	f, err := os.CreateTemp(root, ".project-init-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), marker)
}

func projectScopeAt(cwd string) string {
	if cwd == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	if top, err := cmd.Output(); err == nil {
		return feedbackScope(strings.TrimSpace(string(top)))
	}
	return feedbackScope(cwd)
}

func projectID(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(sum[:])
}

func projectStoreAt(cwd string) (string, string, error) {
	scope := projectScopeAt(cwd)
	if scope == "" {
		return "", "", errors.New("プロジェクトのフォルダを確かめられません")
	}
	root := filepath.Join(scope, ".shikii")
	if err := validateStoreDirectories(root); err != nil {
		return "", "", err
	}

	return root, scope, nil
}

func validateStoreDirectories(root string) error {
	for _, part := range []string{"", "decisions", "wiki", "records", "labels", "sessions", "guards"} {
		info, err := os.Lstat(filepath.Join(root, part))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("プロジェクトの保存先に共有先へのリンクは使えません")
		}
	}
	return nil
}

func currentProjectStore() (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	return projectStoreAt(cwd)
}

func defaultProjectStore() (string, error) {
	root, _, err := currentProjectStore()
	if err != nil {
		return "", err
	}
	cwd, _ := os.Getwd()
	if filepath.Dir(root) == feedbackScope(cwd) {
		return ".shikii", nil
	}
	return root, nil
}

// フラグを読み切ってから既定を解く。拒否した保存先へ相対パスで戻らない。
func resolveStoreFlag(fs *flag.FlagSet) error {
	f := fs.Lookup("store")
	if f == nil {
		return nil
	}
	path := f.Value.String()
	if path == ".shikii" {
		var err error
		path, err = defaultProjectStore()
		if err != nil {
			return err
		}
	}
	if err := validateStoreDirectories(path); err != nil {
		return err
	}
	return f.Value.Set(path)
}

// symlink の共有先へ個別の会話や較正を書かない。
func checkProjectStore(root string, create bool) error {
	for _, part := range []string{"", "decisions", "wiki", "records", "labels", "sessions", "guards"} {
		path := filepath.Join(root, part)
		if create {
			if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
				return err
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) && part != "" && !create {
				continue // 古い版が作った置き場(0.6.0 〜 0.7.2 には guards/ が無い)も登録済みとして扱う。無い置き場は使う時に作る
			}
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("プロジェクトの .shikii と保存先は通常のフォルダにしてください（共有先へのリンクは使いません）")
		}
	}
	return nil
}

// 個人の会話を git add -A に巻き込まない。追跡済みのファイルの扱いは変えない。
func excludeProjectStore(scope string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-path", "info/exclude")
	cmd.Dir = scope
	raw, err := cmd.Output()
	if err != nil {
		return nil
	} // git の外
	path := strings.TrimSpace(string(raw))
	if !filepath.IsAbs(path) {
		path = filepath.Join(scope, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return err
	}
	if bytes.Contains(append([]byte("\n"), data...), []byte("\n/.shikii/\n")) {
		return nil
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	_, err = f.WriteString("\n/.shikii/\n")
	return err
}

func prepareHookProject(cwd string) (string, string, error) {
	root, scope, err := projectStoreAt(cwd)
	if err != nil {
		return "", "", err
	}
	active, err := projectActive(root, scope)
	if err != nil {
		return "", "", err
	}
	if !active {
		return "", "", errProjectInactive
	}
	if err := excludeProjectStore(scope); err != nil {
		return "", "", err
	}
	if err := checkProjectStore(root, true); err != nil {
		return "", "", err
	}
	if _, err := seedAgentSpecs(filepath.Join(root, "decisions")); err != nil {
		return "", "", err
	}
	if _, err := seedWiki(filepath.Join(root, "wiki")); err != nil {
		return "", "", err
	}
	if err := registerProject(scope); err != nil {
		return "", "", err
	}
	return root, scope, nil
}

func registerProject(scope string) error {
	common, err := agentHome()
	if err != nil {
		return err
	}
	entry := projectEntry{ID: projectID(scope), Name: filepath.Base(scope), Path: scope, URL: "/p/" + projectID(scope) + "/"}
	data, _ := json.Marshal(entry)
	dir := filepath.Join(common, "projects")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("プロジェクトの索引を開けません")
	}
	tmp, err := os.CreateTemp(dir, ".project-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, entry.ID+".json")); err != nil {
		return err
	}
	return nil
}

func registeredProjects(common string) []projectEntry {
	projects := []projectEntry{}
	files, _ := filepath.Glob(filepath.Join(common, "projects", "*.json"))
	for _, path := range files {
		f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			continue
		}
		var p projectEntry
		err = json.NewDecoder(io.LimitReader(f, 8192)).Decode(&p)
		f.Close()
		if err != nil || !filepath.IsAbs(p.Path) || feedbackScope(p.Path) != p.Path || projectID(p.Path) != p.ID || filepath.Base(path) != p.ID+".json" {
			continue
		}
		if checkProjectStore(filepath.Join(p.Path, ".shikii"), false) != nil {
			continue
		}
		if active, err := projectActive(filepath.Join(p.Path, ".shikii"), p.Path); err != nil || !active {
			continue
		}
		p.Name, p.URL = filepath.Base(p.Path), "/p/"+p.ID+"/"
		projects = append(projects, p)
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].Name == projects[j].Name {
			return projects[i].Path < projects[j].Path
		}
		return projects[i].Name < projects[j].Name
	})
	return projects
}

func projectPageURL(root string) string {
	scope := filepath.Dir(root)
	return hookPageURL + "/p/" + projectID(scope) + "/"
}

// URL に任意の保存先を受け付けない。hook が登録したプロジェクトだけへ振り分ける。
func newProjectHub(common string, options serveOpts) http.Handler {
	var mu sync.Mutex
	handlers := map[string]http.Handler{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" && r.Method == http.MethodGet {
			writeJSON(w, 200, map[string]any{"projects": registeredProjects(common), "specs": []any{}})
			return
		}
		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/assets/") {
			(&server{}).handleUI(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/p/") {
			parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/p/"), "/", 2)
			for _, p := range registeredProjects(common) {
				if p.ID != parts[0] {
					continue
				}
				if len(parts) == 1 {
					target := p.URL
					if r.URL.RawQuery != "" {
						target += "?" + r.URL.RawQuery
					}
					http.Redirect(w, r, target, http.StatusTemporaryRedirect)
					return
				}
				mu.Lock()
				h := handlers[p.ID]
				if h == nil {
					o := options
					o.storeDir, o.specsDir, o.wikiDir, o.projectScope = filepath.Join(p.Path, ".shikii"), "", "", p.Path
					h = http.StripPrefix(strings.TrimSuffix(p.URL, "/"), newServer(o))
					handlers[p.ID] = h
				}
				mu.Unlock()
				h.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	})
}
