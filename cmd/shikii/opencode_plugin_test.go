package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// wantPluginDir is where OpenCode reads shikii's plugin under home, written out here and not taken from the product code.
func wantPluginDir(home string) string {
	return filepath.Join(home, ".config", "opencode", "plugins", "shikii")
}

// writePluginT puts the two files shikii would write for the binary exe into dir.
func writePluginT(t *testing.T, dir, exe string) {
	t.Helper()
	for name, body := range opencodePluginFiles(exe) {
		writeFileT(t, filepath.Join(dir, name), body)
	}
}

// symlinkT makes link point at target, with the folders above link in place.
func symlinkT(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func installedPlugin(t *testing.T, home string) (index, pkg string) {
	t.Helper()
	dir := wantPluginDir(home)
	i, err := os.ReadFile(filepath.Join(dir, "index.ts"))
	if err != nil {
		t.Fatalf("hook install must write the OpenCode plugin: %v", err)
	}
	p, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		t.Fatalf("hook install must write the OpenCode plugin: %v", err)
	}
	return string(i), string(p)
}

func TestHookInstallPutsOpenCodePluginWithTheAbsolutePath(t *testing.T) {
	home := freshMachine(t)
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	index, pkg := installedPlugin(t, home)
	lit, _ := json.Marshal(shikiiBinaryPath())
	if !filepath.IsAbs(shikiiBinaryPath()) || !strings.Contains(index, `const SHIKII: string = `+string(lit)) || strings.Contains(index, "__SHIKII_BIN__") {
		t.Errorf("the plugin runs this binary by its absolute path (%s):\n%s", lit, index)
	}
	var p struct{ Name, Main string }
	if err := json.Unmarshal([]byte(pkg), &p); err != nil || p.Name != "shikii-opencode-plugin" || p.Main != "index.ts" {
		t.Errorf("package.json names the plugin and its entry: %v %s", err, pkg)
	}
	if !strings.Contains(out, "~/.config/opencode/plugins/shikii") {
		t.Errorf("install says where the OpenCode plugin went:\n%s", out)
	}
	// a second install changes nothing and does not double anything
	first := snapshotTree(t, filepath.Join(home, ".config", "opencode"))
	out2 := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if d := diffTrees(first, snapshotTree(t, filepath.Join(home, ".config", "opencode"))); len(d) != 0 {
		t.Errorf("a second install changed the plugin: %v", d)
	}
	if n := strings.Count(installedIndex(t, home), "const SHIKII"); n != 1 {
		t.Errorf("the plugin must hold the binary once, got %d", n)
	}
	if !strings.Contains(out2, "OpenCode") || !strings.Contains(out2, "already") {
		t.Errorf("a second install says the plugin was already there:\n%s", out2)
	}
}

func installedIndex(t *testing.T, home string) string {
	i, _ := installedPlugin(t, home)
	return i
}

func TestHookInstallPutsOpenCodePluginUnderXDGConfigHome(t *testing.T) {
	home := freshMachine(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if !fileExists(filepath.Join(xdg, "opencode", "plugins", "shikii", "index.ts")) {
		t.Errorf("XDG_CONFIG_HOME decides where OpenCode reads plugins")
	}
	if fileExists(filepath.Join(home, ".config", "opencode")) {
		t.Errorf("nothing goes under ~/.config when XDG_CONFIG_HOME is set")
	}
	// a relative XDG_CONFIG_HOME is not a path the XDG rules allow: the default place is used
	t.Setenv("XDG_CONFIG_HOME", "relative/dir")
	if got := opencodePluginDir(home); got != wantPluginDir(home) {
		t.Errorf("a relative XDG_CONFIG_HOME is ignored: %s", got)
	}
}

func TestHookInstallPreviewWritesNoOpenCodePlugin(t *testing.T) {
	home := freshMachine(t)
	out := capture(t, func() error { return cmdHook([]string{"install"}) })
	if !strings.Contains(out, "OpenCode") || !strings.Contains(out, "plugins/shikii") {
		t.Errorf("the preview names the OpenCode plugin:\n%s", out)
	}
	if fileExists(filepath.Join(home, ".config")) {
		t.Errorf("without --yes nothing is written")
	}
}

func TestHookInstallRewritesShikiisOwnPluginForAnotherBinary(t *testing.T) {
	home := freshMachine(t)
	dir := wantPluginDir(home)
	writePluginT(t, dir, "/old/place/shikii")
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if index := installedIndex(t, home); strings.Contains(index, "/old/place") || !strings.Contains(index, shikiiBinaryPath()) {
		t.Errorf("shikii's own plugin is pointed at the running binary:\n%s", index)
	}
	if !strings.Contains(out, "updated") {
		t.Errorf("install says it updated the plugin:\n%s", out)
	}
}

func TestHookInstallLeavesAPersonsPluginFolderAndLinks(t *testing.T) {
	cases := map[string]func(t *testing.T, home, dir string){
		"a folder with the person's index.ts": func(t *testing.T, home, dir string) {
			writeFileT(t, filepath.Join(dir, "index.ts"), "export default {} // mine\n")
		},
		"a folder with the person's package.json": func(t *testing.T, home, dir string) {
			writeFileT(t, filepath.Join(dir, "package.json"), `{"name":"mine"}`)
		},
		"a folder with other files only": func(t *testing.T, home, dir string) {
			writeFileT(t, filepath.Join(dir, "notes.txt"), "mine")
		},
		"a file named shikii": func(t *testing.T, home, dir string) {
			writeFileT(t, dir, "mine")
		},
		"a link to a folder": func(t *testing.T, home, dir string) {
			target := filepath.Join(home, "dotfiles", "shikii-plugin")
			writeFileT(t, filepath.Join(target, "index.ts"), "export default {} // mine\n")
			symlinkT(t, target, dir)
		},
		"index.ts is a link to a file with shikii's content": func(t *testing.T, home, dir string) {
			linkedPluginFile(t, home, dir, opencodeIndexFile, opencodePluginFiles("/old/shikii")[opencodeIndexFile])
		},
		"package.json is a link to a file with shikii's content": func(t *testing.T, home, dir string) {
			linkedPluginFile(t, home, dir, opencodeJSONFile, `{"name":"shikii-opencode-plugin","main":"index.ts","kept":"by the person"}`)
		},
		"a link to shikii's own plugin elsewhere": func(t *testing.T, home, dir string) {
			target := filepath.Join(home, "dotfiles", "shikii-plugin")
			writePluginT(t, target, "/old/shikii")
			symlinkT(t, target, dir)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			home := freshMachine(t)
			dir := wantPluginDir(home)
			setup(t, home, dir)
			before := snapshotTree(t, home)
			delete(before, filepath.Join(home, ".config")) // directories made by the install of the hooks themselves are not what is compared
			out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
			after := snapshotTree(t, filepath.Join(home, ".config"), filepath.Join(home, "dotfiles"))
			for p, v := range after {
				b, ok := before[p]
				if ok && b != v {
					t.Errorf("%s changed: %q -> %q", p, b, v)
				}
				if !ok && strings.HasPrefix(p, dir) {
					t.Errorf("install added %s inside what is the person's", p)
				}
			}
			if !strings.Contains(out, "plugins/shikii") || !strings.Contains(out, "left as it is") {
				t.Errorf("install says the plugin folder was left as it is:\n%s", out)
			}
			// the rest of the install went on
			if !fileExists(filepath.Join(home, ".claude", "settings.json")) {
				t.Errorf("the hooks of the other agents are still installed")
			}
		})
	}
}

func TestOpenCodePluginWritesOnlyItsTwoFiles(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".config", "opencode")
	for _, outside := range []string{
		filepath.Join(cfg, "opencode.json"),
		filepath.Join(cfg, "plugin", "herdr-agent-state.js"),
		filepath.Join(cfg, "plugins", "mine", "index.ts"),
		filepath.Join(cfg, "plugins", "shikii", "other.ts"),
		filepath.Join(cfg, "plugins", "shikii", "sub", "index.ts"),
		filepath.Join(home, ".claude", "settings.json"),
	} {
		if err := writeOpencodePluginFile(home, outside, []byte("x")); err == nil || fileExists(outside) {
			t.Errorf("a write outside the plugin's two files must be refused: %s (err %v)", outside, err)
		}
	}
	if err := writeOpencodePluginFile(home, filepath.Join(cfg, "plugins", "shikii", "index.ts"), []byte("x")); err != nil {
		t.Errorf("the plugin's own file is allowed: %v", err)
	}
}

func TestUninstallRemovesOnlyShikiisOpenCodePlugin(t *testing.T) {
	b := newUninstallBox(t)
	cfg := filepath.Join(b.home, ".config", "opencode")
	installedPlugin(t, b.home) // precondition: install put it there
	mine := map[string]string{
		filepath.Join(cfg, "plugin", "herdr-agent-state.js"):  "export default {}\n",
		filepath.Join(cfg, "plugins", "mine", "index.ts"):     "export default {}\n",
		filepath.Join(cfg, "plugins", "mine", "package.json"): `{"name":"mine"}`,
		filepath.Join(cfg, "opencode.json"):                   `{"plugin":["x"]}`,
	}
	for p, body := range mine {
		writeFileT(t, p, body)
	}
	out := capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	if fileExists(filepath.Join(cfg, "plugins", "shikii")) {
		t.Errorf("shikii's plugin folder is removed:\n%s", out)
	}
	if !strings.Contains(out, "plugins/shikii") {
		t.Errorf("uninstall lists the plugin:\n%s", out)
	}
	for p, body := range mine {
		if got, err := os.ReadFile(p); err != nil || string(got) != body {
			t.Errorf("the person's %s stays as it was: %v %q", p, err, got)
		}
	}
	for p, body := range b.person { // the person's own files, including opencode.json in the project, stay
		if strings.HasSuffix(p, "opencode.json") {
			got, _ := os.ReadFile(p)
			if !strings.Contains(string(got), `"other"`) {
				t.Errorf("the person's entry in %s stays: %s (was %s)", p, got, body)
			}
		}
	}
}

// linkedPluginFile makes the plugin folder a real folder in which one of the two files is a link to a file kept elsewhere (a dotfiles
// folder) whose content looks like shikii's. The other file, when it is the one that is not the link, is shikii's own.
func linkedPluginFile(t *testing.T, home, dir, linked, body string) {
	t.Helper()
	target := filepath.Join(home, "dotfiles", linked)
	writeFileT(t, target, body)
	symlinkT(t, target, filepath.Join(dir, linked))
	for name, content := range opencodePluginFiles("/old/shikii") {
		if name != linked {
			writeFileT(t, filepath.Join(dir, name), content)
		}
	}
}

// A link inside the folder is the person's even when what it points at looks like shikii's: uninstall removes neither the link nor
// what it points at, and leaves the folder.
func TestUninstallLeavesALinkedFileInsideThePluginFolder(t *testing.T) {
	for _, linked := range []string{opencodeIndexFile, opencodeJSONFile} {
		t.Run(linked, func(t *testing.T) {
			home := freshMachine(t)
			dir := wantPluginDir(home)
			body := opencodePluginFiles("/old/shikii")[linked]
			linkedPluginFile(t, home, dir, linked, body)
			before := snapshotTree(t, filepath.Join(home, ".config"), filepath.Join(home, "dotfiles"))
			out := captureNoFail(func() error { return cmdUninstall([]string{"--yes"}) })
			if d := diffTrees(before, snapshotTree(t, filepath.Join(home, ".config"), filepath.Join(home, "dotfiles"))); len(d) != 0 {
				t.Errorf("uninstall changed the folder with a linked file or its target: %v\n%s", d, out)
			}
			if !strings.Contains(out, "left alone") {
				t.Errorf("uninstall says it left the folder:\n%s", out)
			}
		})
	}
}

func TestUninstallKeepsAPersonsFileInsideShikiisPluginFolder(t *testing.T) {
	b := newUninstallBox(t)
	dir := wantPluginDir(b.home)
	writeFileT(t, filepath.Join(dir, "notes.txt"), "mine")
	capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	if fileExists(filepath.Join(dir, "index.ts")) || fileExists(filepath.Join(dir, "package.json")) {
		t.Errorf("shikii's two files are removed")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(got) != "mine" {
		t.Errorf("a file of the person's in the folder stays")
	}
}

func TestUninstallLeavesAPersonsPluginFolderNamedShikii(t *testing.T) {
	home := freshMachine(t)
	dir := wantPluginDir(home)
	writeFileT(t, filepath.Join(dir, "index.ts"), "export default {} // mine\n")
	writeFileT(t, filepath.Join(dir, "package.json"), `{"name":"mine"}`)
	out := captureNoFail(func() error { return cmdUninstall([]string{"--yes"}) })
	if got, _ := os.ReadFile(filepath.Join(dir, "index.ts")); !strings.Contains(string(got), "mine") || !fileExists(filepath.Join(dir, "package.json")) {
		t.Errorf("a folder that is not shikii's stays:\n%s", out)
	}
	if !strings.Contains(out, "left alone") || !strings.Contains(out, "plugins/shikii") {
		t.Errorf("uninstall says it left the folder:\n%s", out)
	}
}

func TestUninstallWithoutYesListsTheOpenCodePluginAndRemovesNothing(t *testing.T) {
	b := newUninstallBox(t)
	dir := wantPluginDir(b.home)
	before := snapshotTree(t, dir)
	out := capture(t, func() error { return cmdUninstall(nil) })
	if !strings.Contains(out, "plugins/shikii") || len(diffTrees(before, snapshotTree(t, dir))) != 0 {
		t.Errorf("the preview lists the plugin and changes nothing:\n%s", out)
	}
}

func TestUninstallFollowsXDGConfigHome(t *testing.T) {
	home := freshMachine(t)
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if !fileExists(filepath.Join(xdg, "opencode", "plugins", "shikii", "index.ts")) {
		t.Fatal("precondition: the plugin is under XDG_CONFIG_HOME")
	}
	capture(t, func() error { return cmdUninstall([]string{"--yes"}) })
	if fileExists(filepath.Join(xdg, "opencode", "plugins", "shikii")) {
		t.Errorf("uninstall removes the plugin from XDG_CONFIG_HOME (home %s)", home)
	}
}

func TestHookUninstallRemovesTheOpenCodePlugin(t *testing.T) {
	home := freshMachine(t)
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	capture(t, func() error { return cmdHook([]string{"uninstall", "--yes"}) })
	if fileExists(wantPluginDir(home)) {
		t.Errorf("hook uninstall removes what hook install put in OpenCode")
	}
}

func TestAgentsWithShikiiHookCountsOpenCodeWhenItsPluginIsThere(t *testing.T) {
	home := freshMachine(t)
	if got := agentsWithShikiiHook(home); len(got) != 0 {
		t.Fatalf("precondition: %v", got)
	}
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	got := agentsWithShikiiHook(home)
	if !strings.Contains(strings.Join(got, ","), "OpenCode") {
		t.Errorf("OpenCode has a shikii hook once the plugin is there: %v", got)
	}
	// only OpenCode: no hook file of the other two
	home2 := freshMachine(t)
	writePluginT(t, wantPluginDir(home2), "/x/shikii")
	if got := agentsWithShikiiHook(home2); strings.Join(got, ",") != "OpenCode" {
		t.Errorf("a machine with only the plugin has OpenCode's hook: %v", got)
	}
	// a person's folder of that name is not a shikii hook
	home3 := freshMachine(t)
	writeFileT(t, filepath.Join(wantPluginDir(home3), "index.ts"), "mine")
	if got := agentsWithShikiiHook(home3); len(got) != 0 {
		t.Errorf("a person's folder is not shikii's hook: %v", got)
	}
}

func TestInitInstallsTheOpenCodePluginOnAFirstProject(t *testing.T) {
	home := freshMachine(t)
	repo := gitRepo(t)
	t.Chdir(repo)
	out := capture(t, func() error { return cmdInit(nil) })
	installedPlugin(t, home)
	if !strings.Contains(out, "installs them now") {
		t.Errorf("init installs the hooks, the OpenCode plugin among them, for the first project:\n%s", out)
	}
	if got := agentsWithShikiiHook(home); !strings.Contains(strings.Join(got, ","), "OpenCode") {
		t.Errorf("init counts OpenCode among the agents with hooks: %v", got)
	}
}

func TestDoctorChecksTheBinaryTheOpenCodePluginRuns(t *testing.T) {
	home := t.TempDir()
	exe := filepath.Join(t.TempDir(), "shikii")
	writeFileT(t, exe, "binary")
	if err := os.Chmod(exe, 0o755); err != nil {
		t.Fatal(err)
	}
	dir := wantPluginDir(home)
	put := func(bin string) { writePluginT(t, dir, bin) }
	put(exe)
	joined := strings.Join(registeredBinaryLines(home, exe), "\n")
	if !strings.Contains(joined, "ok   OpenCode plugin") || !strings.Contains(joined, exe) {
		t.Errorf("doctor reports the plugin's binary as ok:\n%s", joined)
	}
	put(filepath.Join(t.TempDir(), "gone", "shikii"))
	joined = strings.Join(registeredBinaryLines(home, exe), "\n")
	if !strings.Contains(joined, "MISSING OpenCode plugin") || !strings.Contains(joined, "hook install --yes") {
		t.Errorf("doctor says the plugin's binary is gone and how to fix it:\n%s", joined)
	}
	// a person's folder is named, not checked
	other := t.TempDir()
	writeFileT(t, filepath.Join(wantPluginDir(other), "index.ts"), "mine")
	joined = strings.Join(registeredBinaryLines(other, exe), "\n")
	if strings.Contains(joined, "MISSING") || !strings.Contains(joined, "not shikii's") {
		t.Errorf("doctor does not check a folder that is not shikii's:\n%s", joined)
	}
}

// the plugin text: every hook the plugin gives OpenCode is wrapped in try/catch (a throw in the prompt or context hook makes OpenCode
// refuse the request), the turn it starts for a stop block carries the metadata the prompt hook turns into stop_hook_active, and
// nothing of the spike (a marker word, SPIKE_ variables) is left in.
func TestOpenCodePluginBodyWrapsEveryHookAndMarksItsOwnTurns(t *testing.T) {
	body := opencodePluginFiles("/x/shikii")[opencodeIndexFile]
	for _, hook := range []string{`ctx.session.hook("prompt", async (e: any) => {`, `ctx.session.hook("context", async (e: any) => {`, `const stop = async (sessionID: string) => {`} {
		i := strings.Index(body, hook)
		if i < 0 {
			t.Fatalf("the plugin must have %s", hook)
		}
		if rest := body[i+len(hook):]; !regexp.MustCompile(`^\s*try \{`).MatchString(rest) {
			t.Errorf("%s must start with try { (a throw here refuses the request):\n%.120s", hook, rest)
		}
	}
	if n := strings.Count(body, "ctx.session.hook("); n != 2 {
		t.Errorf("exactly the prompt and context hooks: %d", n)
	}
	if !regexp.MustCompile(`const onEvent = async \(ev: any\) => \{\s*try \{`).MatchString(body) || !strings.Contains(body, "await stop(") {
		t.Errorf("each event of the subscription is handled inside try")
	}
	if !strings.Contains(body, "void onEvent(ev)") || strings.Contains(body, "await onEvent(") {
		t.Errorf("the events are not handled one by one: a slow stop would hold back the other sessions")
	}
	for _, want := range []string{
		`metadata: { shikii: CONTINUE_KEY }`,    // the continuation is marked
		`e.metadata?.shikii === CONTINUE_KEY`,   // and the prompt hook recognises it
		`stop_hook_active: active`,              // so the next stop is stop_hook_active
		`continued.set(sessionID, true)`,        //
		`"session.execution.succeeded"`,         // the end of a turn
		`CONTINUE_MAX`,                          // a safety net against endless continuation
		`parentID`,                              // a subagent's session is not the person's request
		`hookSpecificOutput?.additionalContext`, // read as hookOut.json writes it
		`out?.decision === "block"`,             //
		`stdio: ["pipe", "pipe", "ignore"]`,     // no shell
		`session_id: sessionID, cwd: cwd()`,     // the keys hookInput reads
		`{ prompt: e.prompt?.text ?? "" }`,
		`last_assistant_message: reply`,
		`"session.inbox.enqueued"`, // a subagent's report wakes the parent
		`"session.compaction.ended"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the plugin must contain %q", want)
		}
	}
	for _, event := range regexp.MustCompile(`(?:^|[^.\w])hook\("([a-z-]+)"`).FindAllStringSubmatch(body, -1) {
		if !shikiiHookEvent(event[1]) {
			t.Errorf("the plugin runs an event shikii does not have: %s", event[1])
		}
	}
	for _, event := range []string{"user-prompt-submit", "session-start", "stop"} {
		if !strings.Contains(body, `hook("`+event+`"`) {
			t.Errorf("the plugin must run %s", event)
		}
	}
	for _, gone := range []string{"PINEAPPLE", "SPIKE_", "appendFileSync", "spawnSync"} {
		if strings.Contains(body, gone) {
			t.Errorf("nothing of the trial is left in the plugin: %s", gone)
		}
	}
	if !strings.HasPrefix(body, opencodePluginMark) {
		t.Errorf("the plugin starts with the mark shikii recognises its own file by")
	}
}

// The preview says what the install will do, as the install itself decides it (it asks installOpencodePlugin without writing): a
// second install changes nothing, and an older plugin is updated, not "added".
func TestHookInstallPreviewSaysWhatTheInstallWillDo(t *testing.T) {
	home := freshMachine(t)
	capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	same := capture(t, func() error { return cmdHook([]string{"install"}) })
	if !strings.Contains(same, "already there as this install would write it, so nothing changes") || strings.Contains(same, "add the OpenCode plugin") {
		t.Errorf("with shikii's plugin in place the preview says nothing changes:\n%s", same)
	}
	writePluginT(t, wantPluginDir(home), "/old/place/shikii")
	older := capture(t, func() error { return cmdHook([]string{"install"}) })
	if !strings.Contains(older, "update shikii's OpenCode plugin") || strings.Contains(older, "add the OpenCode plugin") {
		t.Errorf("with an older plugin in place the preview says it updates it:\n%s", older)
	}
}

// A folder under the home folder whose name starts with two dots (~/..x) is under the home folder, and shown with ~.
func TestOpenCodePluginFolderIsShownWithTildeUnderTheHomeFolder(t *testing.T) {
	home := freshMachine(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "..x"))
	out := capture(t, func() error { return cmdHook([]string{"install"}) })
	if !strings.Contains(out, "~/..x/opencode/plugins/shikii") {
		t.Errorf("the plugin folder under the home folder is shown with ~:\n%s", out)
	}
}

// The binary the plugin runs is read back as the JSON string it was written as, whatever the path holds (a space and slashes included).
func TestOpenCodePluginBinaryIsReadBackWhateverItsPathHolds(t *testing.T) {
	for _, exe := range []string{"/Applications/my tools // shikii/shikii", `/x/with "quote"/shikii`, "/plain/shikii"} {
		home := t.TempDir()
		writePluginT(t, wantPluginDir(home), exe)
		if got, ok := opencodePluginBinary(home); !ok || got != exe {
			t.Errorf("the plugin's binary %q is read back as %q (ok %v)", exe, got, ok)
		}
	}
}

// An install that stopped halfway (shikii's package.json and no index.ts) is mended by the next one; a folder in which one of the two
// files is a person's is left, whichever file it is.
func TestHookInstallMendsAnInstallThatStoppedHalfway(t *testing.T) {
	home := freshMachine(t)
	dir := wantPluginDir(home)
	writeFileT(t, filepath.Join(dir, "package.json"), opencodePluginPackage)
	out := capture(t, func() error { return cmdHook([]string{"install", "--yes"}) })
	if !fileExists(filepath.Join(dir, "index.ts")) || !strings.Contains(out, "updated") {
		t.Errorf("the missing index.ts is written next to shikii's package.json:\n%s", out)
	}
	mine := freshMachine(t)
	writeFileT(t, filepath.Join(wantPluginDir(mine), "package.json"), opencodePluginPackage)
	writeFileT(t, filepath.Join(wantPluginDir(mine), "index.ts"), "export default {} // mine\n")
	if got, _ := installOpencodePlugin(mine, "/x/shikii", true); got != "left" {
		t.Errorf("an index.ts of the person's keeps the folder as it is: %s", got)
	}
}

// What shikii wrote is written to a new file renamed into place: a link at the place is replaced and what it pointed at is not written.
func TestOpenCodePluginWriteNeverWritesThroughALink(t *testing.T) {
	home := t.TempDir()
	dir := wantPluginDir(home)
	target := filepath.Join(home, "dotfiles", "index.ts")
	writeFileT(t, target, "mine")
	symlinkT(t, target, filepath.Join(dir, "index.ts"))
	if err := writeOpencodePluginFile(home, filepath.Join(dir, "index.ts"), []byte("shikii's")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "mine" {
		t.Errorf("the file the link pointed at is untouched: %q", got)
	}
	if info, err := os.Lstat(filepath.Join(dir, "index.ts")); err != nil || !info.Mode().IsRegular() {
		t.Errorf("the link was replaced by the file: %v %v", info, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".write-*")); len(left) != 0 {
		t.Errorf("no temporary file is left: %v", left)
	}
}

// A file far larger than shikii's two small files is not read; it is not shikii's.

// removeOpencodePlugin says what it found, and removes only shikii's own.

// A file far larger than shikii's two small files is not read; it is not shikii's.
func TestOpenCodePluginFileTooLargeToBeShikiisIsLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.ts")
	writeFileT(t, path, opencodePluginMark+strings.Repeat("x", opencodeMaxFile+1))
	if state, _ := opencodeFileState(path, opencodeIndexFile); state != opencodePluginForeign {
		t.Errorf("a large file is not shikii's: %v", state)
	}
}

// removeOpencodePlugin says what it found, and removes only shikii's own.
func TestRemoveOpencodePluginSaysWhatItFound(t *testing.T) {
	home := t.TempDir()
	if state, err := removeOpencodePlugin(home, true); err != nil || state != opencodePluginAbsent {
		t.Errorf("nothing there: %v %v", state, err)
	}
	writePluginT(t, wantPluginDir(home), "/x/shikii")
	if state, _ := removeOpencodePlugin(home, false); state != opencodePluginShikii || !fileExists(filepath.Join(wantPluginDir(home), "index.ts")) {
		t.Errorf("a look removes nothing: %v", state)
	}
	if state, err := removeOpencodePlugin(home, true); err != nil || state != opencodePluginShikii || fileExists(wantPluginDir(home)) {
		t.Errorf("shikii's own is removed: %v %v", state, err)
	}
	writeFileT(t, filepath.Join(wantPluginDir(home), "index.ts"), "mine")
	if state, _ := removeOpencodePlugin(home, true); state != opencodePluginForeign || !fileExists(filepath.Join(wantPluginDir(home), "index.ts")) {
		t.Errorf("a person's folder stays: %v", state)
	}
}
