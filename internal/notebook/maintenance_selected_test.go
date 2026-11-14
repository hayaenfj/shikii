package notebook

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaintenanceSelectedPageRequiresReadableSources(t *testing.T) {
	f := newOutcomeFixture(t)
	p := f.page("howto", "Selected procedure", "Keep the approval condition.", "Synthetic archive", "Owner approval is required.")
	restore := f.lose(f.source)
	if tasks, err := f.b.MaintenanceForPages([]string{p.ID}); err == nil {
		t.Fatal("an unreadable selected article must not look like a completed review", tasks)
	}
	restore()
	tasks, err := f.b.MaintenanceForPages([]string{p.ID})
	if err != nil || len(tasks) != 1 || tasks[0].PageID != p.ID {
		t.Fatal("restoring the source must recover the pending review", tasks, err)
	}
}

func TestMaintenanceSelectedArticleMustExist(t *testing.T) {
	for _, state := range []string{"missing", "malformed", "unreadable", "deleted"} {
		t.Run(state, func(t *testing.T) {
			f := newOutcomeFixture(t)
			p := f.page("howto", "Selected procedure", "Keep approval.", "Synthetic archive", "Owner approval is required.")
			dir, err := f.b.directory(false, "pages")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, p.ID+".md")
			switch state {
			case "missing", "unreadable":
				err = os.Remove(path)
				if err == nil && state == "unreadable" {
					err = os.Mkdir(path, 0o700)
				}
			case "malformed":
				err = os.WriteFile(path, []byte("damaged synthetic article"), 0o600)
			case "deleted":
				_, err = f.b.Manage(Change{ID: p.ID, Revision: p.Revision, Action: "delete"})
			}
			if err != nil {
				t.Fatal(err)
			}
			tasks, err := f.b.MaintenanceForPages([]string{p.ID})
			if state == "deleted" {
				if err != nil || len(tasks) != 0 {
					t.Fatal("an explicit deletion needs no current-article review", tasks, err)
				}
			} else if err == nil {
				t.Fatal("a missing or unreadable selected article is not a completed review", tasks)
			}
		})
	}
}

func TestMaintenanceSelectedRelatedArticleMustBeReadable(t *testing.T) {
	for _, relation := range []string{"outgoing", "incoming"} {
		for _, state := range []string{"missing", "malformed", "source-missing", "deleted"} {
			if relation == "incoming" && state == "missing" {
				continue // A removed incoming link leaves no current article that names the relationship.
			}
			t.Run(relation+"/"+state, func(t *testing.T) {
				f := newOutcomeFixture(t)
				d := draft(f.source, "Related procedure")
				first, err := f.b.Ingest(d)
				if err != nil {
					t.Fatal(err)
				}
				d.Title = "Selected procedure"
				d.Related = []string{first.Page.ID}
				second, err := f.b.Ingest(d)
				if err != nil {
					t.Fatal(err)
				}
				selected, related := *second.Page, *first.Page
				if relation == "incoming" {
					selected, related = related, selected
				}
				tasks, err := f.b.MaintenanceForPages([]string{selected.ID})
				if err != nil || len(tasks) != 1 {
					t.Fatal(tasks, err)
				}
				reviewRelatedTasks(t, f.b, tasks)
				dir, err := f.b.directory(false, "pages")
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, related.ID+".md")
				switch state {
				case "missing":
					err = os.Remove(path)
				case "malformed":
					err = os.WriteFile(path, []byte("damaged synthetic related article"), 0o600)
				case "source-missing":
					// Only the related article loses its evidence.
					source := f.capture("test", "Related condition", "Keep the related condition.")
					_, err = f.b.Patch(Patch{ID: related.ID, Revision: related.Revision, Sources: []string{source.ID}, ChangeReason: "Add related evidence.", Edits: []TextEdit{{Field: "lesson", Before: related.Lesson, After: related.Lesson + " Retain related evidence."}}})
					if err == nil {
						f.lose(source.ID)
					}
				case "deleted":
					_, err = f.b.Manage(Change{ID: related.ID, Revision: related.Revision, Action: "delete"})
				}
				if err != nil {
					t.Fatal(err)
				}
				tasks, err = f.b.MaintenanceForPages([]string{selected.ID})
				if state == "deleted" {
					if err != nil || len(tasks) != 1 {
						t.Fatal("an explicit related deletion is a reviewable premise change", tasks, err)
					}
				} else if err == nil {
					t.Fatal("unreadable related evidence must not be dropped from the selected review", tasks)
				}
			})
		}
	}
}
