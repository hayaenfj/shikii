package main

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// gitLayout is where the files of a working tree's repository are, told by the files alone (no process): gitDir holds the HEAD of this
// working tree and the files that belong to it alone; commonDir holds what it shares (the object folder, the refs, the configuration and
// info/exclude). They are one folder for a plain `.git` folder, a submodule and a working tree whose git folder is kept elsewhere; a
// linked worktree has a folder of its own under `<repository>/worktrees/` and shares the repository it was made from. Both are absolute
// with the links resolved, as git resolves them.
type gitLayout struct {
	gitDir, commonDir string
}

func (l gitLayout) objects() string { return filepath.Join(l.commonDir, "objects") }
func (l gitLayout) exclude() string { return filepath.Join(l.commonDir, "info", "exclude") }

// gitFileMax is the most a `.git` file or a commondir file is read for: they hold one path.
const gitFileMax = 4096

// layoutAt tells where the files of the working tree whose top folder is top (the folder that holds its `.git`) are, in the three forms
// git writes: a `.git` folder, a `.git` file that names the git folder (a linked worktree, a submodule, a working tree made with
// --separate-git-dir), and, for a linked worktree, the commondir file of that git folder. ok is false for everything else (a link, a
// `.git` folder that is shared, a file git reads another way, a folder that is not a git folder): git says then. A path in these files
// is read as git reads it, from the folder that holds the file and not cleaned before the links are resolved (`..` after a link names
// the folder above what the link points to).
func layoutAt(top string) (gitLayout, bool) {
	dotGit := filepath.Join(top, ".git")
	entry, err := os.Lstat(dotGit)
	if err != nil {
		return gitLayout{}, false
	}
	var l gitLayout
	switch {
	case entry.IsDir():
		if _, err := os.Lstat(filepath.Join(dotGit, "commondir")); !errors.Is(err, fs.ErrNotExist) {
			return gitLayout{}, false // a `.git` folder that says it is shared with another: git says
		}
		l = gitLayout{gitDir: dotGit, commonDir: dotGit}
	case entry.Mode().IsRegular():
		named, ok := oneLineOf(dotGit)
		if !ok {
			return gitLayout{}, false
		}
		path, ok := strings.CutPrefix(named, "gitdir: ")
		if !ok || path == "" || path != strings.TrimLeft(path, " \t") {
			return gitLayout{}, false
		}
		gitDir, ok := resolvedFrom(top, path)
		if !ok {
			return gitLayout{}, false
		}
		l = gitLayout{gitDir: gitDir, commonDir: gitDir}
		switch shared, err := os.Lstat(filepath.Join(gitDir, "commondir")); {
		case err == nil && shared.Mode().IsRegular():
			named, ok := oneLineOf(filepath.Join(gitDir, "commondir"))
			if !ok {
				return gitLayout{}, false
			}
			if l.commonDir, ok = resolvedFrom(gitDir, named); !ok {
				return gitLayout{}, false
			}
		case !errors.Is(err, fs.ErrNotExist):
			return gitLayout{}, false
		}
	default:
		return gitLayout{}, false
	}
	// what git asks of a git folder: a HEAD that is a plain file and names a ref or a commit, and an object folder and a refs folder that
	// are folders (not links) it can enter (a link or a damaged one is left to git)
	if info, err := os.Lstat(filepath.Join(l.gitDir, "HEAD")); err != nil || !info.Mode().IsRegular() {
		return gitLayout{}, false
	}
	if line, ok := oneLineOf(filepath.Join(l.gitDir, "HEAD")); !ok || !isObjectID(line) && !namesARef(line) {
		return gitLayout{}, false
	}
	// git follows a link to the refs folder; the object folder is read as a folder of its own, as before
	if info, err := os.Lstat(filepath.Join(l.commonDir, "objects")); err != nil || !info.IsDir() {
		return gitLayout{}, false
	}
	if info, err := os.Stat(filepath.Join(l.commonDir, "refs")); err != nil || !info.IsDir() {
		return gitLayout{}, false
	}
	for _, name := range []string{"objects", "refs"} {
		if syscall.Access(filepath.Join(l.commonDir, name), 1) != nil { // 1: may be entered
			return gitLayout{}, false
		}
	}
	return l, true
}

// namesARef reports whether the content of a HEAD file is `ref:` and a name under refs/, the spaces after the colon being as many as
// git accepts.
func namesARef(line string) bool {
	rest, ok := strings.CutPrefix(line, "ref:")
	rest = strings.TrimLeft(rest, " \t")
	return ok && strings.HasPrefix(rest, "refs/") && len(rest) > len("refs/")
}

// oneLineOf is the content of a small file that holds one line, without its line end. ok is false for a file that is not a plain one,
// is longer than a path, or has spaces or a carriage return around the line (git trims what is around a line, and those forms are left
// to it). A second line is part of the path then, and a path like that is not a folder.
func oneLineOf(file string) (string, bool) {
	if info, err := os.Lstat(file); err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	f, err := os.Open(file)
	if err != nil {
		return "", false
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, gitFileMax+1))
	if err != nil || len(raw) > gitFileMax {
		return "", false
	}
	line := strings.TrimSuffix(string(raw), "\n")
	if line == "" || line != strings.TrimSpace(line) {
		return "", false
	}
	return line, true
}

// resolvedFrom is the folder a path in a git file names, as git resolves it: from the folder that holds the file when it is relative,
// with its links resolved. ok is false when it is not a folder.
func resolvedFrom(holder, path string) (string, bool) {
	if !filepath.IsAbs(path) {
		path = holder + string(filepath.Separator) + path // not filepath.Join: that folds `..` away before the links are resolved
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	if info, err := os.Lstat(real); err != nil || !info.IsDir() {
		return "", false
	}
	return real, true
}

// fileID says which file a path is and what state it is in: the same file with the same inode, size and both times is the same content
// for what is asked here. The change time (ctime) is the one that cannot be set back, so a file put in place with an old modification time
// (mv, cp -p, a restore) is another file here; and a configuration written in the same second as the answer, or after it, is another one
// too. Exists is false for a file that is not there (a zero id): its being made later is a change.
type fileID struct {
	Exists bool   `json:"e,omitempty"`
	Ino    uint64 `json:"i,omitempty"`
	Size   int64  `json:"s,omitempty"`
	Mtime  int64  `json:"m,omitempty"`
	Ctime  int64  `json:"c,omitempty"`
}

// statID is the id of the file a path names (links followed: git writes a configuration through one). ok is false when it could not be
// told; a file that is not there is a zero id with ok.
func statID(path string) (fileID, bool) {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fileID{}, true
	case err != nil:
		return fileID{}, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}, false
	}
	ctime, ok := changeTimeOf(st)
	if !ok {
		return fileID{}, false // a system whose stat has no change time: the files cannot be told apart, git is asked
	}
	return fileID{Exists: true, Ino: uint64(st.Ino), Size: info.Size(), Mtime: info.ModTime().UnixNano(), Ctime: ctime}, true
}

// changeTimeOf is the time the system last changed the file's inode (its content, name or timestamps), in nanoseconds: it cannot be set
// back by touch, cp -p or a restore, as the modification time can. The field is Ctimespec on macOS and Ctim on Linux (read by name, so
// that one file builds for both: the checks that read the package's code type-check every file of it, with no build tags).
func changeTimeOf(st *syscall.Stat_t) (int64, bool) {
	v := reflect.ValueOf(st).Elem()
	for _, name := range []string{"Ctimespec", "Ctim"} {
		if f := v.FieldByName(name); f.IsValid() && f.Kind() == reflect.Struct {
			sec, nsec := f.FieldByName("Sec"), f.FieldByName("Nsec")
			if sec.IsValid() && nsec.IsValid() {
				return sec.Int()*1_000_000_000 + nsec.Int(), true
			}
		}
	}
	return 0, false
}

// placeSnapshot is what the files say about a working tree at one moment: where its git folders are, and the state of the configurations
// that can say where the working tree is (the shared one, which every repository has, and the one of a worktree alone, which most do not).
type placeSnapshot struct {
	layout           gitLayout
	config, wtConfig fileID
}

// snapshotAt takes the snapshot of the working tree whose top folder is top. ok is false where the layout cannot be told, or the
// shared configuration is not there.
func snapshotAt(top string) (placeSnapshot, bool) {
	l, ok := layoutAt(top)
	if !ok {
		return placeSnapshot{}, false
	}
	config, ok1 := statID(filepath.Join(l.commonDir, "config"))
	wtConfig, ok2 := statID(filepath.Join(l.gitDir, "config.worktree"))
	if !ok1 || !ok2 || !config.Exists || !workTreeIsHere(l, top) {
		return placeSnapshot{}, false
	}
	return placeSnapshot{layout: l, config: config, wtConfig: wtConfig}, true
}

var (
	configSection = regexp.MustCompile(`^\[([A-Za-z0-9-]+)( "(?:[^"\\]|\\.)*")?\]$`)
	configKey     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
)

// configFileMax is the most of a configuration read for the folder it names as the working tree.
const configFileMax = 1 << 20

// coreWorktreeOf reads the value of core.worktree as written in the text of a git configuration. present is false where there is none.
// ok is false where the text is in a form that is not read here with certainty (an include, a quote, a comment after the value, a
// line that goes on, a carriage return, a second value): the folder it names is then not known, and git is asked.
func coreWorktreeOf(text string) (value string, present, ok bool) {
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "worktree") && !strings.Contains(lower, "[include") {
		return "", false, true // no key of that name, and nothing that could include one
	}
	if strings.ContainsAny(text, "\r\x00") {
		return "", false, false
	}
	inCore := false
	for _, raw := range strings.Split(text, "\n") {
		if strings.HasSuffix(raw, "\\") {
			return "", false, false // a line that goes on
		}
		line := strings.TrimLeft(raw, " \t")
		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
		case line[0] == '[':
			m := configSection.FindStringSubmatch(strings.TrimRight(line, " \t"))
			if m == nil || strings.EqualFold(m[1], "include") || strings.EqualFold(m[1], "includeIf") {
				return "", false, false
			}
			inCore = strings.EqualFold(m[1], "core") && m[2] == ""
		default:
			key, val, hasValue := strings.Cut(line, "=")
			if key = strings.TrimSpace(key); !configKey.MatchString(key) {
				return "", false, false
			}
			if !inCore || !strings.EqualFold(key, "worktree") {
				continue
			}
			if val = strings.TrimSpace(val); !hasValue || val == "" || strings.ContainsAny(val, "\"\\#;") || present {
				return "", false, false
			}
			value, present = val, true
		}
	}
	return value, present, true
}

// workTreeIsHere reports whether the configuration of the repository puts the working tree where the `.git` was found, if it names one
// at all (core.worktree, which a submodule always has, is read from the git folder through the links as git reads it). The folder it
// names is read each time, so a link on the way pointed elsewhere changes the answer; where it cannot be read with certainty, it is no.
func workTreeIsHere(l gitLayout, top string) bool {
	for _, file := range []string{filepath.Join(l.commonDir, "config"), filepath.Join(l.gitDir, "config.worktree")} {
		raw, err := readSmallFile(file, configFileMax)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return false
		}
		value, present, ok := coreWorktreeOf(string(raw))
		if !ok {
			return false
		}
		if !present {
			continue
		}
		if folder, ok := resolvedFrom(l.gitDir, value); !ok || !sameFolder(folder, top) {
			return false
		}
	}
	return true
}

// readSmallFile reads a file (links followed, as git does for a configuration) of at most max bytes.
func readSmallFile(file string, max int64) ([]byte, error) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, errors.New("not a small plain file")
	}
	return os.ReadFile(file)
}

// layoutAgrees reports whether what the files say is what git said about the same working tree: the object folder, the exclude file
// and, where both are known, HEAD. A kept place and the reading of HEAD from files are for the layouts where it holds; the rest of the
// time git is asked, as before.
func layoutAgrees(l gitLayout, p gitPlace) bool {
	if !samePath(l.objects(), p.objects) || !samePath(l.exclude(), p.exclude) {
		return false
	}
	if p.headKnown {
		if head, known := headFromFiles(l.gitDir, l.commonDir); known && head != p.head {
			return false
		}
	}
	return true
}

// samePath reports whether two paths name the same file: the same text, or the same once the links are resolved.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// isPerWorktreeRef reports whether a ref belongs to one worktree (git keeps it in the git folder of that worktree, not in the shared
// one).
func isPerWorktreeRef(name string) bool {
	for _, prefix := range []string{"refs/worktree/", "refs/bisect/", "refs/rewritten/"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// keptEntryAgrees reports whether a kept answer is still for what the files say now: the answer is fresh, it is for this top folder, and
// the git folders and the configurations are the ones it was given for. A kept answer of 0.37.3 and before names none of them and is
// not used (the one git is asked then keeps a new one).
func keptEntryAgrees(e placeEntry, top string, now placeSnapshot) bool {
	if age := time.Since(time.Unix(e.At, 0)); age > placeCacheTTL || age < -time.Minute || !sameFolder(e.Top, top) {
		return false
	}
	return e.Git != "" && e.Git == now.layout.gitDir && e.Common == now.layout.commonDir && e.Config == now.config && e.WtConfig == now.wtConfig
}
