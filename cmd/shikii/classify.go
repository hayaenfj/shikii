package main

// Filling in pages from before the version bump (0.9.0): since 0.9.0 every page needs a kind and a scene. Pages made by earlier versions have
// neither, so the request hook finds them and has the agent fill classifyBatch pages at a time; if they are still empty when the turn ends,
// the ritual check (wiki-kinds) blocks once. The agent fills them with shikii wiki classify (on MCP, classify in shikii_wiki)
// and does not rewrite the lesson text.

import (
	"fmt"
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

// classifyBatch is the number of pages asked for in one request.
const classifyBatch = 5

// classifyContext returns, when there are pages without a kind or scene, their count, classifyBatch of them (id, title, lesson) and how to fill them. Empty when there are none.
func classifyContext(agent, scope string) (string, []string, error) {
	if scope == "" {
		return "", nil, nil
	}
	book := notebook.Book{Root: agent, Scope: scope}
	un, err := book.Unclassified()
	if err != nil || len(un) == 0 {
		return "", nil, err
	}
	// If the wiki has an unreadable page or a page edited by hand, every save is refused and classify always fails. Do not ask (or block); only add one line suggesting lint (audit 0.9.0 B2)
	// Stop only when saving is refused (a problem in the page itself). A source problem in another page still lets saves through, so do not stop (re-audit 0.9.0 new-2)
	if issues, err := book.PageIssues(); err == nil && len(issues) > 0 {
		return fmt.Sprintf("shikii: pages without a kind or scene: %d. They cannot be filled now because the wiki has an unreadable page or a page edited by hand. Check and fix the problem with %s wiki lint, and then they can be filled.", len(un), hookExe()), nil, nil
	}
	live, err := book.Live()
	if err != nil {
		return "", nil, err
	}
	var scenes []string
	for _, sc := range liveScenes(live) {
		scenes = append(scenes, fmt.Sprintf("%s(%d)", sc.Scene, sc.Count))
		if len(scenes) == 12 {
			break
		}
	}
	batch := un
	if len(batch) > classifyBatch {
		batch = batch[:classifyBatch]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "shikii: pages without a kind or scene (made before the version bump): %d. After the current request, read the lessons of the next %d listed below and give each a kind and a scene: %s wiki classify <page ID> <kind> <scene> (on MCP, operation classify in shikii_wiki with id, kind and scene). Kinds are rule=a rule decided in this project / howto=how to do something / criteria=a criterion for judging / fact=an assumption or fact / history=how things came to be; the scene is \"when ...\" in the work's own words (up to 24 characters).", len(un), len(batch), hookExe())
	if len(scenes) > 0 {
		b.WriteString("Existing scenes: " + strings.Join(scenes, ", ") + ". If one is close, use the same wording.")
	}
	ids := make([]string, 0, len(batch))
	for _, p := range batch {
		fmt.Fprintf(&b, "\n- %s: %s — %s (applies when: %s)", p.ID, p.Title, cut(p.Lesson, 160), cut(p.AppliesTo, 80))
		ids = append(ids, p.ID)
	}
	return b.String(), ids, nil
}

// classifyLeft returns the ids of the asked pages that still have no kind or scene.
func classifyLeft(root, scope string, asked []string) ([]string, error) {
	un, err := (notebook.Book{Root: root, Scope: scope}).Unclassified()
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range asked {
		want[id] = true
	}
	var left []string
	for _, p := range un {
		if want[p.ID] {
			left = append(left, p.ID)
		}
	}
	return left, nil
}
