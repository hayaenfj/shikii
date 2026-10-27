package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Properties C1 to C4 of .shikii/handoff/audit-0.9.7/invariants.md over random sequences of what a person does with project folders on
// one machine (0.9.7 quality bar): move (mv), copy (cp -R), bring in a .shikii from elsewhere, make a request (the hook), and shikii --init.
// A store becomes active only if this machine initialized it (and it was moved here) or the person ran shikii --init in its folder; a
// legitimate store moved and then asked for is active; the shared index never names a store whose id or folder differs.
func TestRelinkPropertiesOverRandomFolderHistories(t *testing.T) {
	runs := 40
	if testing.Short() {
		runs = 10
	}
	for run := 0; run < runs; run++ {
		t.Run(fmt.Sprint(run), func(t *testing.T) {
			r := rand.New(rand.NewSource(int64(970 + run)))
			repo := t.TempDir() // half of the runs outside Git: the project is the folder itself (0.9.7 full audit 1-1)
			if run%2 == 0 {
				repo = gitRepo(t)
			}
			hookEnv(t, nil, repo)
			common, _ := agentHome()
			base := t.TempDir()
			// identity: the store a folder holds ("orig", "copy-N", "foreign-N"); legit: the stores this machine initialized
			identity := map[string]string{feedbackScope(repo): "orig"}
			legit := map[string]bool{"orig": true}
			askedSinceArrival := map[string]bool{}
			n := 0
			fresh := func(name string) string {
				n++
				p := filepath.Join(base, fmt.Sprintf("%s-%d", name, n))
				return p
			}
			pick := func() string {
				var all []string
				for f := range identity {
					all = append(all, f)
				}
				if len(all) == 0 {
					return ""
				}
				// map order is random; sort-free pick by rand over a stable copy
				return all[r.Intn(len(all))]
			}
			var steps []string
			for step := 0; step < 8; step++ {
				switch r.Intn(5) {
				case 0: // move
					f := pick()
					to := fresh("moved")
					if err := os.Rename(f, to); err != nil {
						t.Fatal(err)
					}
					to = feedbackScope(to)
					identity[to] = identity[f]
					delete(identity, f)
					delete(askedSinceArrival, f)
					steps = append(steps, "mv "+filepath.Base(f)+" "+filepath.Base(to))
				case 1: // copy
					f := pick()
					to := fresh("copy")
					if out, err := exec.Command("cp", "-R", f, to).CombinedOutput(); err != nil {
						t.Fatal(err, string(out))
					}
					to = feedbackScope(to)
					identity[to] = fmt.Sprintf("copy-%d", n)
					steps = append(steps, "cp "+filepath.Base(f)+" "+filepath.Base(to))
				case 2: // bring in a .shikii from elsewhere that names one of these folders (another machine, an archive)
					f := pick()
					to := fresh("foreign")
					if out, err := exec.Command("cp", "-R", f, to).CombinedOutput(); err != nil {
						t.Fatal(err, string(out))
					}
					to = feedbackScope(to)
					os.WriteFile(filepath.Join(to, ".shikii", "records", "store-id"), []byte(fmt.Sprintf("%032x\n", r.Int63())), 0o600)
					identity[to] = fmt.Sprintf("foreign-%d", n)
					steps = append(steps, "bring "+filepath.Base(to)+" naming "+filepath.Base(f))
				case 3: // a request in a folder
					f := pick()
					relinkMovedProject(f, false)
					askedSinceArrival[f] = true
					steps = append(steps, "ask "+filepath.Base(f))
				default: // the person runs shikii --init in a folder
					f := pick()
					t.Chdir(f)
					capture(t, func() error { return cmdInit(nil) })
					legit[identity[f]] = true
					askedSinceArrival[f] = true
					steps = append(steps, "init "+filepath.Base(f))
				}
				for f, id := range identity {
					active, _ := projectActive(filepath.Join(f, ".shikii"), f)
					if active && !legit[id] {
						t.Fatalf("C2/C3: %s (%s) became active without this machine's init or the person's --init\nsteps: %v", filepath.Base(f), id, steps)
					}
					if !active && legit[id] && askedSinceArrival[f] && !copiedOver(identity, f) {
						t.Fatalf("C1: %s (%s), initialized here and asked for after it arrived, is not active\nsteps: %v", filepath.Base(f), id, steps)
					}
				}
				files, _ := filepath.Glob(filepath.Join(common, "projects", "*.json"))
				for _, file := range files {
					b, _ := os.ReadFile(file)
					var p projectEntry
					if json.Unmarshal(b, &p) != nil {
						continue
					}
					if _, gone := os.Stat(p.Path); gone != nil {
						continue // a path no longer there is not listed (registeredProjects skips it)
					}
					root := filepath.Join(p.Path, ".shikii")
					if active, _ := projectActive(root, p.Path); active && (p.StoreID != storeID(root, false) || p.StoreDir != storeDir(root)) {
						t.Fatalf("C4: the index entry for %s names another store\nsteps: %v", filepath.Base(p.Path), steps)
					}
				}
			}
		})
	}
}

// copiedOver reports whether f holds a copy whose original is still where this machine's index has it: relinking it would take over (only
// the person's --init does). Used only to skip the liveness check.
func copiedOver(identity map[string]string, f string) bool {
	_, copied := originalElsewhere(filepath.Join(f, ".shikii"))
	return copied
}
