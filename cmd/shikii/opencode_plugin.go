package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// OpenCode 2.x has no JSON hook settings (Claude Code's settings.json, Codex's hooks.json). It loads plugins from
// <config>/opencode/plugins/<folder>/ instead, so shikii puts one plugin folder there: package.json and index.ts. The plugin runs the
// same `<shikii> hook <event>` commands the other two agents run, with the same JSON on stdin (opencode_plugin/index.ts). The
// folder is per user (every project reads it); a project that was never initialized is skipped by the hooks themselves. OpenCode 1.x
// does not read the folder and keeps using only the MCP server.
//
// The folder is shikii's when package.json names it or index.ts starts with the mark below, and neither of the two files is anything
// else (so an install that stopped halfway is mended by the next one). A folder or a file a person made under the same name is never
// written over, and a link is never followed: install says so and goes on, and uninstall leaves it.

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

	// what the lines shikii prints call a plugin folder that is not shikii's (install, preview, uninstall, doctor)
	opencodeNotShikiis = "a folder, file or link of yours"
	opencodeMaxFile    = 1 << 20 // a file larger than this is not read: it is not shikii's two small files
)

// opencodePluginState says what is at the plugin folder.
type opencodePluginState int

const (
	opencodePluginAbsent  opencodePluginState = iota // nothing there (or an empty folder): shikii may write
	opencodePluginShikii                             // shikii's own plugin: shikii may rewrite and remove it
	opencodePluginForeign                            // a person's folder, file or link: shikii leaves it as it is
)

// opencodeConfigDir is OpenCode's config folder: $XDG_CONFIG_HOME/opencode, or ~/.config/opencode. A relative XDG_CONFIG_HOME is not a
// path the XDG rules allow, and OpenCode 2.3.0 reads it relative to the folder it was started in (`opencode debug paths` with
// XDG_CONFIG_HOME=relative/dir prints config relative/dir/opencode; an empty value is the default place). That folder differs per
// launch and shikii cannot know it, so shikii does not follow a relative value: it uses the default place, as for an empty one.
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

// opencodeOtherConfigDir is the folder OpenCode 2.x reads its config and its plugins from in place of opencodeConfigDir, when
// OPENCODE_CONFIG_DIR names another one in this environment; "" when OpenCode reads shikii's plugin where shikii puts it. OpenCode 2.3.0
// replaces its config folder with OPENCODE_CONFIG_DIR and reads the plugins under that one only (measured with env -i and a fake HOME
// and XDG: a serve started with it did not load a plugin under $XDG_CONFIG_HOME/opencode/plugins/, and loaded one under
// $OPENCODE_CONFIG_DIR/plugins/). shikii does not write there (doctor says so instead); a link there to shikii's folder is read.
func opencodeOtherConfigDir(home string) string {
	dir := os.Getenv("OPENCODE_CONFIG_DIR")
	// sameFolder (project_registry.go) also takes a link or another spelling of the same folder for it
	if dir == "" || sameFolder(dir, opencodeConfigDir(home)) || sameFolder(filepath.Join(dir, "plugins", opencodePluginFolder), opencodePluginDir(home)) {
		return ""
	}
	return dir
}

// opencodePluginFiles is what shikii writes, by file name, for the binary exe (an absolute path: the plugin runs it without a shell).
func opencodePluginFiles(exe string) map[string]string {
	lit, _ := json.Marshal(exe)
	return map[string]string{
		opencodeJSONFile:  opencodePluginPackage,
		opencodeIndexFile: strings.Replace(opencodePluginIndex, opencodeBinLiteral, string(lit), 1),
	}
}

// opencodeFileState looks at one of the two files without following a link: absent, shikii's, or not shikii's (a link, a folder, other
// content, a file too large to be one of shikii's). It is opened with O_NOFOLLOW and checked to be the file that was looked at, so a
// link put there in between is not followed either.
func opencodeFileState(path, name string) (opencodePluginState, []byte) {
	before, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return opencodePluginAbsent, nil
	}
	if err != nil || !before.Mode().IsRegular() || before.Size() > opencodeMaxFile {
		return opencodePluginForeign, nil
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return opencodePluginForeign, nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return opencodePluginForeign, nil
	}
	b, err := io.ReadAll(io.LimitReader(f, opencodeMaxFile+1))
	if err != nil || len(b) > opencodeMaxFile {
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
	entries, err := os.ReadDir(dir)
	if err != nil {
		return opencodePluginForeign
	}
	for _, e := range entries {
		if !opencodeOwnTempFile(e) {
			return opencodePluginForeign // a folder somebody filled: not shikii's
		}
	}
	return opencodePluginAbsent
}

// opencodeOwnTempFile says whether a directory entry is the temporary file writeAtomic makes next to a file it writes: a regular file
// named .write-<digits>.tmp (os.CreateTemp fills the * with digits). A folder or a link of that name is not: it is a person's. One that an install
// left behind when it stopped before the rename is not a person's file: the folder with only that in it counts as empty, so the next
// install writes the plugin, and install and uninstall remove it (removeOpencodeTempFiles): OpenCode reads every folder under plugins/
// as a plugin, and a folder left with only that in it would be one without an entry.
func opencodeOwnTempFile(e os.DirEntry) bool {
	return e.Type().IsRegular() && opencodeTempName.MatchString(e.Name())
}

var opencodeTempName = regexp.MustCompile(`^\.write-[0-9]+\.tmp$`)

// opencodePluginWritable is the allowed list of places shikii writes or removes for the plugin: the two files in the plugin folder,
// and nowhere else. Every write and removal goes through it.
func opencodePluginWritable(home, path string) bool {
	dir := opencodePluginDir(home)
	return path == filepath.Join(dir, opencodeJSONFile) || path == filepath.Join(dir, opencodeIndexFile)
}

// opencodePluginRemovable is the allowed list of what shikii removes from the plugin folder: the two files, and a temporary file of
// writeAtomic's name right in the folder. Nothing else, whatever its name. Every removal goes through it.
func opencodePluginRemovable(home, path string) bool {
	return opencodePluginWritable(home, path) || filepath.Dir(path) == opencodePluginDir(home) && opencodeTempName.MatchString(filepath.Base(path))
}

// opencodeTempFiles lists the temporary files an install left in the plugin folder (opencodeOwnTempFile), by path. None when the folder
// is not a real folder (a link to one is a person's).
func opencodeTempFiles(home string) []string {
	dir := opencodePluginDir(home)
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var paths []string
	for _, e := range entries {
		if opencodeOwnTempFile(e) {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}
	return paths
}

// removeOpencodeTempFiles removes the temporary files an install left in the plugin folder and says how many it removed. Each is looked
// at again right before it is removed, without following a link: only a regular file is removed, so a link or a folder that took the
// name in between stays (and what a link points at is never touched).
func removeOpencodeTempFiles(home string) (int, error) {
	n := 0
	for _, path := range opencodeTempFiles(home) {
		if !opencodePluginRemovable(home, path) {
			return n, errors.New(path + " is outside the OpenCode plugin folder")
		}
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return n, err
		}
		n++
	}
	return n, nil
}

// installOpencodePlugin puts the plugin in place for the binary exe, and removes the temporary files an earlier install left. With write
// false it only says what it would do. The result is "added", "updated" (shikii's own, with other content: an older version or another
// binary path), "same", or "left" (a person's folder is there: nothing is written or removed; the error is nil, install goes on without it).
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
	if write {
		if _, err := removeOpencodeTempFiles(home); err != nil {
			return "", err
		}
	}
	return result, nil
}

// writeOpencodePluginFile is the one way the plugin's files are written: only the two places opencodePluginWritable allows. The file
// is written to a new temporary file next to it and renamed over it, so a link that appeared at the place is replaced, never written
// through (writePersonFile is for files of the person's, where a link is followed on purpose).
func writeOpencodePluginFile(home, path string, content []byte) error {
	if !opencodePluginWritable(home, path) {
		return fmt.Errorf("%s is outside the OpenCode plugin folder, so shikii does not write it", path)
	}
	return writeAtomicMode(path, content, 0o644)
}

// removeOpencodePlugin removes shikii's plugin: its two files and the temporary files an install left, then the folder when nothing else
// is in it. It returns what is at the plugin folder; only shikii's own (opencodePluginShikii) is removed, a person's folder stays. A
// folder that holds only temporary files counts as absent, and they are removed too (opencodeTempFiles says whether there are any). With
// write false it only says what it would find.
func removeOpencodePlugin(home string, write bool) (opencodePluginState, error) {
	dir := opencodePluginDir(home)
	state := opencodePluginStateOf(dir)
	if state == opencodePluginForeign || !write {
		return state, nil
	}
	if state == opencodePluginAbsent {
		if n, err := removeOpencodeTempFiles(home); err != nil || n == 0 {
			return state, err
		}
		_ = os.Remove(dir) // only when nothing else is in it
		return state, nil
	}
	for _, name := range []string{opencodeIndexFile, opencodeJSONFile} {
		path := filepath.Join(dir, name)
		if s, _ := opencodeFileState(path, name); s != opencodePluginShikii { // checked again right before removing
			continue
		}
		if !opencodePluginRemovable(home, path) {
			return state, errors.New(path + " is outside the OpenCode plugin folder")
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return state, err
		}
	}
	if _, err := removeOpencodeTempFiles(home); err != nil {
		return state, err
	}
	_ = os.Remove(dir) // only when nothing else is in it
	return state, nil
}

// opencodePluginBinary is the binary shikii's plugin runs, read back from index.ts (for doctor). ok is false when there is no
// shikii plugin or its path cannot be read. The path is a JSON string literal and is read as one, so a path with any characters
// (a space and slashes, quotes) is read back as it was written.
func opencodePluginBinary(home string) (string, bool) {
	dir := opencodePluginDir(home)
	if opencodePluginStateOf(dir) != opencodePluginShikii {
		return "", false
	}
	_, b := opencodeFileState(filepath.Join(dir, opencodeIndexFile), opencodeIndexFile)
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, opencodeBinDeclStart); ok {
			var bin string
			if json.NewDecoder(strings.NewReader(rest)).Decode(&bin) == nil && bin != "" && filepath.IsAbs(bin) {
				return bin, true
			}
		}
	}
	return "", false
}
