package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenCode 2.x has no JSON hook settings (Claude Code's settings.json, Codex's hooks.json). It loads plugins from
// <config>/opencode/plugins/<folder>/ instead, so shikii puts one plugin folder there: package.json and index.ts. The plugin runs the
// same `<shikii> hook <event>` commands the other two agents run, with the same JSON on stdin (opencode_plugin/index.ts). The
// folder is per user (every project reads it); a project that was never initialized is skipped by the hooks themselves. OpenCode 1.x
// does not read the folder and keeps using only the MCP server.
//
// The folder is shikii's only when package.json names it and index.ts starts with the mark below. A folder or a file a person made
// under the same name is never written over, and a link is never followed: install says so and goes on, and uninstall leaves it.

//go:embed opencode_plugin/index.ts
var opencodePluginIndex string

//go:embed opencode_plugin/package.json
var opencodePluginPackage string

const (
	opencodePluginName   = "shikii-opencode-plugin"             // package.json "name": what tells shikii's folder from a person's
	opencodePluginMark   = "// shikii-managed OpenCode plugin." // the first line of index.ts
	opencodeBinLiteral   = `"__SHIKII_BIN__"`                   // in index.ts, replaced by the binary's absolute path (a JSON string)
	opencodeBinDeclStart = `const SHIKII: string = `            // the line that holds the path (doctor reads it back)
	opencodeIndexFile    = "index.ts"                           // the two files shikii writes in the folder, and nothing else
	opencodeJSONFile     = "package.json"
	opencodePluginFolder = "shikii"
)

// opencodePluginState says what is at the plugin folder.
type opencodePluginState int

const (
	opencodePluginAbsent  opencodePluginState = iota // nothing there (or an empty folder): shikii may write
	opencodePluginShikii                             // shikii's own plugin: shikii may rewrite and remove it
	opencodePluginForeign                            // a person's folder, file or link: shikii leaves it as it is
)

// opencodeConfigDir is OpenCode's config folder: $XDG_CONFIG_HOME/opencode, or ~/.config/opencode (a relative XDG_CONFIG_HOME is not
// a path the XDG rules allow, so it is not used).
func opencodeConfigDir(home string) string {
	if x := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "opencode")
	}
	return filepath.Join(home, ".config", "opencode")
}

// opencodePluginDir is the folder shikii's plugin lives in.
func opencodePluginDir(home string) string {
	return filepath.Join(opencodeConfigDir(home), "plugins", opencodePluginFolder)
}

// opencodePluginFiles is what shikii writes, by file name, for the binary exe (an absolute path: the plugin runs it without a shell).
func opencodePluginFiles(exe string) map[string]string {
	lit, _ := json.Marshal(exe)
	return map[string]string{
		opencodeJSONFile:  opencodePluginPackage,
		opencodeIndexFile: strings.Replace(opencodePluginIndex, opencodeBinLiteral, string(lit), 1),
	}
}

// opencodeFileState looks at one of the two files without following a link: absent, shikii's, or not shikii's (a link, a folder, other content).
func opencodeFileState(path, name string) (opencodePluginState, []byte) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return opencodePluginAbsent, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return opencodePluginForeign, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return opencodePluginForeign, nil
	}
	switch name {
	case opencodeJSONFile:
		var p struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(b, &p) == nil && p.Name == opencodePluginName {
			return opencodePluginShikii, b
		}
	case opencodeIndexFile:
		if strings.HasPrefix(string(b), opencodePluginMark) {
			return opencodePluginShikii, b
		}
	}
	return opencodePluginForeign, b
}

// opencodePluginStateOf says whether the plugin folder is absent, shikii's, or a person's. The folder itself must be a real folder
// (a link, even to a folder, is a person's); an empty one counts as absent; a folder with other files in it is shikii's when at
// least one of the two files is shikii's and neither is anything else.
func opencodePluginStateOf(dir string) opencodePluginState {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return opencodePluginAbsent
	}
	if err != nil || !info.IsDir() {
		return opencodePluginForeign
	}
	own := 0
	for _, name := range []string{opencodeJSONFile, opencodeIndexFile} {
		switch s, _ := opencodeFileState(filepath.Join(dir, name), name); s {
		case opencodePluginForeign:
			return opencodePluginForeign
		case opencodePluginShikii:
			own++
		}
	}
	if own > 0 {
		return opencodePluginShikii
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
		return opencodePluginForeign // a folder somebody filled: not shikii's
	}
	return opencodePluginAbsent
}

// opencodePluginWritable is the allowed list of places shikii writes or removes for the plugin: the two files in the plugin folder,
// and nowhere else. Every write and removal goes through it.
func opencodePluginWritable(home, path string) bool {
	dir := opencodePluginDir(home)
	return path == filepath.Join(dir, opencodeJSONFile) || path == filepath.Join(dir, opencodeIndexFile)
}

// installOpencodePlugin puts the plugin in place for the binary exe. With write false it only says what it would do. The result is
// "added", "updated" (shikii's own, with other content: an older version or another binary path), "same", or "left" (a person's
// folder is there: nothing is written; the error is nil, install goes on without it).
func installOpencodePlugin(home, exe string, write bool) (string, error) {
	dir := opencodePluginDir(home)
	state := opencodePluginStateOf(dir)
	if state == opencodePluginForeign {
		return "left", nil
	}
	files := opencodePluginFiles(exe)
	result := "same"
	if state == opencodePluginAbsent {
		result = "added"
	}
	for _, name := range []string{opencodeJSONFile, opencodeIndexFile} {
		path := filepath.Join(dir, name)
		_, have := opencodeFileState(path, name)
		if have != nil && string(have) == files[name] {
			continue
		}
		if state == opencodePluginShikii {
			result = "updated"
		}
		if !write {
			continue
		}
		if err := writeOpencodePluginFile(home, path, []byte(files[name])); err != nil {
			return "", err
		}
	}
	return result, nil
}

// writeOpencodePluginFile is the one way the plugin's files are written: only the two places opencodePluginWritable allows.
func writeOpencodePluginFile(home, path string, content []byte) error {
	if !opencodePluginWritable(home, path) {
		return fmt.Errorf("%s is outside the OpenCode plugin folder, so shikii does not write it", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writePersonFile(path, content, 0o644)
}

// removeOpencodePlugin removes shikii's plugin: its two files, then the folder when nothing else is in it. A person's folder is not
// shikii's and stays (found false). With write false it only says whether it would remove something.
func removeOpencodePlugin(home string, write bool) (found bool, err error) {
	dir := opencodePluginDir(home)
	if opencodePluginStateOf(dir) != opencodePluginShikii {
		return false, nil
	}
	if !write {
		return true, nil
	}
	for _, name := range []string{opencodeIndexFile, opencodeJSONFile} {
		path := filepath.Join(dir, name)
		if s, _ := opencodeFileState(path, name); s != opencodePluginShikii { // checked again right before removing
			continue
		}
		if !opencodePluginWritable(home, path) {
			return true, errors.New(path + " is outside the OpenCode plugin folder")
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return true, err
		}
	}
	_ = os.Remove(dir) // only when nothing else is in it
	return true, nil
}

// opencodePluginBinary is the binary shikii's plugin runs, read back from index.ts (for doctor). ok is false when there is no
// shikii plugin or its path cannot be read.
func opencodePluginBinary(home string) (string, bool) {
	dir := opencodePluginDir(home)
	if opencodePluginStateOf(dir) != opencodePluginShikii {
		return "", false
	}
	_, b := opencodeFileState(filepath.Join(dir, opencodeIndexFile), opencodeIndexFile)
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, opencodeBinDeclStart); ok {
			var bin string
			if json.Unmarshal([]byte(strings.TrimSpace(strings.SplitN(rest, " //", 2)[0])), &bin) == nil && bin != "" && filepath.IsAbs(bin) {
				return bin, true
			}
		}
	}
	return "", false
}

// opencodePluginShown is the plugin folder with the home folder as ~ (for the lines shikii prints).
func opencodePluginShown(home string) string {
	dir := opencodePluginDir(home)
	if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + filepath.ToSlash(rel)
	}
	return dir
}
