package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	for _, part := range []string{"", "decisions", "wiki", "records", "labels", "sessions"} {
		info, err := os.Lstat(filepath.Join(root, part))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", errors.New("プロジェクトの保存先に共有先へのリンクは使えません")
		}
	}
	return root, scope, nil
}

func currentProjectStore() (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	return projectStoreAt(cwd)
}

func defaultProjectStore() string {
	root, _, err := currentProjectStore()
	if err != nil {
		return ".shikii"
	}
	cwd, _ := os.Getwd()
	if filepath.Dir(root) == feedbackScope(cwd) {
		return ".shikii"
	}
	return root
}

// symlink の共有先へ個別の会話や較正を書かない。
func checkProjectStore(root string, create bool) error {
	for _, part := range []string{"", "decisions", "wiki", "records", "labels", "sessions"} {
		path := filepath.Join(root, part)
		if create {
			if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
				return err
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
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
	common, err := agentHome()
	if err != nil {
		return "", "", err
	}
	entry := projectEntry{ID: projectID(scope), Name: filepath.Base(scope), Path: scope, URL: "/p/" + projectID(scope) + "/"}
	data, _ := json.Marshal(entry)
	dir := filepath.Join(common, "projects")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("プロジェクトの索引を開けません")
	}
	tmp, err := os.CreateTemp(dir, ".project-*")
	if err != nil {
		return "", "", err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	closeErr := tmp.Close()
	if err != nil {
		return "", "", err
	}
	if closeErr != nil {
		return "", "", closeErr
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, entry.ID+".json")); err != nil {
		return "", "", err
	}
	return root, scope, nil
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
					http.Redirect(w, r, p.URL, http.StatusTemporaryRedirect)
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
