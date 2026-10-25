package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
)

// The proposer and the wiki. Used by tune --propose N.
//
//   - The proposer is a chat LLM (OpenAI-compatible /v1/chat/completions; upstream llama-server works too).
//     It is given the current wording, the confusion table, examples it got wrong, and the relevant wiki pages (types, model families, confirmed patterns, this spec's rules and learnings).
//     It returns candidates as JSON. Candidates are kept in the spec's variants (so the next run can score them without proposing; people can read them too).
//   - The text (the examples it got wrong) is given only to a local (loopback) proposer when the spec has pii: true.
//   - The wiki is a folder of markdown. tune writes evidence pages to runs/ and adds a pointer to specs/<id>.md.
//     Pattern (patterns/), type (types/) and model (models/) pages are grown by people. The machine only reads them.

type proposal struct {
	Name      string            `json:"name"`
	Question  string            `json:"question,omitempty"`
	Options   map[string]string `json:"options,omitempty"`
	Levels    []string          `json:"levels,omitempty"`
	Template  string            `json:"template,omitempty"`
	Steps     []spec.Step       `json:"steps,omitempty"`  // a proposal to split a choice into a row of yes/no steps
	Layout    string            `json:"layout,omitempty"` // a proposal for the prompt layout (state-last | state-first | plain)
	Rationale string            `json:"rationale,omitempty"`
}

// isLoopback reports whether the proposer is local (whether text from a pii: true spec may be sent to it).
func isLoopback(raw string) bool {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsUnspecified() // 0.0.0.0 / :: means "every port on this machine" = local
}

// proposerAuth is the key sent to the proposer. The hosted (TypeSafe / OpenRouter) key is never sent to the proposer, because it is a different party.
// If the proposer needs a key, use SHIKII_PROPOSER_KEY. Nothing is sent to a local proposer.
func proposerAuth(base string) string {
	if isLoopback(base) {
		return ""
	}
	return os.Getenv("SHIKII_PROPOSER_KEY")
}

// wikiContext is the wiki excerpt given to the proposer: types, model families, confirmed patterns and this spec's page.
func wikiContext(wikiDir string, sp *spec.Spec, model string) string {
	var b strings.Builder
	add := func(title, path string, max int) {
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		s := strings.TrimSpace(string(data))
		if len([]rune(s)) > max {
			s = string([]rune(s)[:max]) + "\n…"
		}
		fmt.Fprintf(&b, "\n### %s (%s)\n%s\n", title, filepath.Base(path), s)
	}
	// Endpoints often have a context of about 4096 tokens, so keep the whole thing to about 6000 characters (types, models and the spec first; patterns get what is left)
	add("How to ask this type of question", filepath.Join(wikiDir, "types", string(sp.Type)+".md"), 1500)
	add("This model family", filepath.Join(wikiDir, "models", modelSlug(model)+".md"), 1200)
	add("This decision's own rules and past lessons", filepath.Join(wikiDir, "specs", sp.ID+".md"), 1800)
	if pats, _ := filepath.Glob(filepath.Join(wikiDir, "patterns", "*.md")); len(pats) > 0 {
		sort.Strings(pats)
		for _, p := range pats {
			if len([]rune(b.String())) > 5000 {
				break
			}
			data, err := os.ReadFile(p)
			if err != nil || !strings.Contains(string(data), "status: confirmed") {
				continue
			}
			add("Pattern", p, 700)
		}
	}
	return b.String()
}

// modelSlug makes a wiki page name from a model name (dropping quantization and layout suffixes).
// wikiLink returns a Markdown link from runs/ if the wiki page rel (relative to wiki/) exists, or plain text if not.
func wikiLink(wikiDir, rel, text string) string {
	if fileExists(filepath.Join(wikiDir, filepath.FromSlash(rel))) {
		return fmt.Sprintf("[%s](../%s)", text, rel)
	}
	return text
}

func modelSlug(model string) string {
	s := strings.ToLower(model)
	if i := strings.Index(s, "+"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(s, "-gguf")
	s = regexp.MustCompile(`[^a-z0-9.]+`).ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// buildProposerPrompt is the request text for the proposer.
// localProposer: whether the proposer is local (loopback). A remote proposer is never shown text from a pii: true spec or tainted text.
func buildProposerPrompt(sp *spec.Spec, cur candScore, bank []bankRow, wikiText string, n int, localProposer bool) (system, user string) {
	withExamples := localProposer || !sp.PIISensitive()
	system = "You improve how a typed question is asked to a decision model (a model that answers with probabilities, not text). " +
		"You will propose alternative wordings. Keep the meaning of the question and the identity of every option; change only how it is phrased, " +
		"how options are described, or how the input text is laid out. Prefer changes that address the errors shown. " +
		"Answer with a JSON array only, no prose around it."
	if sp.Type == spec.Choice {
		system += " You may also propose a decomposition instead of a rewording: \"steps\", a list of yes/no questions asked all at once, " +
			"[{\"ask\": \"…?\", \"no\": \"<option settled when the answer is no>\"}, …, {\"ask\": \"…?\", \"yes\": \"<option>\", \"no\": \"<option>\"}]. " +
			"A middle step settles exactly one option (yes or no) and passes the rest on; the last step settles both; every option appears exactly once. " +
			"Branch probabilities are multiplied into the original options, so this suits models that answer yes/no better than a multi-way choice."
	}
	system += " A proposal may also set \"layout\" (one of state-last, state-first, plain) to change how the prompt is arranged for a plain chat model behind shikii-readout: " +
		"plain drops the system message, lists options as \"- id: description\" and reads the option id itself instead of a letter. Decision models ignore it."
	var b strings.Builder
	fmt.Fprintf(&b, "## Current spec (id %s, type %s)\nquestion: %s\n", sp.ID, sp.Type, strings.TrimSpace(sp.Question))
	for _, o := range sp.Options {
		fmt.Fprintf(&b, "option %s: %s\n", o.ID, o.Description)
	}
	for i, l := range sp.Levels {
		fmt.Fprintf(&b, "level %d: %s\n", i, l)
	}
	fmt.Fprintf(&b, "template:\n%s\n", sp.State.Template)
	fmt.Fprintf(&b, "\n## How the current wording performs on %d truths\ntop-1 accuracy %.3f, brier %.4f, precision@act %.3f, recall@act %.3f\n", len(bank), cur.acc, cur.brier, cur.prec, cur.rec)
	b.WriteString("confusion (truth → predicted: weight):\n")
	truths := make([]string, 0, len(cur.confusion))
	for t := range cur.confusion {
		truths = append(truths, t)
	}
	sort.Strings(truths)
	for _, t := range truths {
		preds := make([]string, 0)
		for p := range cur.confusion[t] {
			preds = append(preds, p)
		}
		sort.Strings(preds)
		for _, p := range preds {
			if p != t {
				fmt.Fprintf(&b, "  %s → %s: %.0f\n", t, p, cur.confusion[t][p])
			}
		}
	}
	if withExamples {
		b.WriteString("\n## Examples the current wording gets wrong (input, truth)\n")
		shown := 0
		for i, r := range bank {
			if cur.correct[i] || shown >= 6 || (r.tainted && !localProposer) {
				continue // tainted text (recorded in work that touched production data) is shown only to a local proposer
			}
			state, _ := sp.Render(r.inputs)
			fmt.Fprintf(&b, "- %q → truth %s\n", trim(state, 200), r.truth)
			shown++
		}
		if shown == 0 {
			b.WriteString("(none; all correct — propose wordings that keep accuracy and sharpen the probabilities)\n")
		}
	} else {
		b.WriteString("\n(example texts withheld: this decision may contain personal information and you are not a local model)\n")
	}
	if strings.TrimSpace(wikiText) != "" {
		b.WriteString("\n## What is already known about asking this kind of question (from the wiki)\n" + wikiText + "\n")
	}
	fmt.Fprintf(&b, "\n## Task\nPropose %d alternative wordings as a JSON array. Each item: {\"name\": short-slug, \"question\": optional full replacement, "+
		"\"options\": optional {option_id: new description}, \"levels\": optional full list of %d level names, \"template\": optional state template that still mentions %v, \"rationale\": one sentence}. "+
		"Only include fields you change. Names must be unique slugs.\n", n, len(sp.Levels), sp.State.Required)
	return system, b.String()
}

// askProposer sends the request to a chat endpoint and extracts the JSON candidates.
func askProposer(ctx context.Context, base, model, system, user string) ([]proposal, string, error) {
	endpoint := strings.TrimRight(base, "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
		endpoint += "/chat/completions"
	}
	body := map[string]any{
		"messages":    []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
		"temperature": 0.8,
		"max_tokens":  2000,
		// Qwen's "thinking" mode prints long thoughts before the answer and the JSON gets cut off. llama.cpp can stop it with these kwargs (endpoints that do not know them ignore them)
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	}
	if model != "" {
		body["model"] = model
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := proposerAuth(base); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("proposer %s: HTTP %d: %s", endpoint, resp.StatusCode, trim(string(data), 200))
	}
	var out struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil || len(out.Choices) == 0 {
		return nil, "", fmt.Errorf("proposer: unexpected response: %s", trim(string(data), 200))
	}
	content := out.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		content = out.Choices[0].Message.Reasoning // an endpoint that ended with only thoughts. Pick up the JSON if it is in there
	}
	props, err := parseProposals(content)
	if err != nil && out.Choices[0].FinishReason == "length" {
		err = fmt.Errorf("%w (the proposer ran out of tokens; the prompt may be too long for its context, try a bigger -c or fewer wiki pages)", err)
	}
	return props, content, err
}

// parseProposals extracts a JSON array from the reply (even when wrapped in ``` or surrounded by text).
func parseProposals(content string) ([]proposal, error) {
	var props []proposal
	var lastErr error
	parsed := false
	// A [ in the preamble is fine: look for the first [ ... last ] that reads as an array
	for i := strings.Index(content, "["); i >= 0 && i < len(content); {
		j := strings.LastIndex(content, "]")
		if j <= i {
			break
		}
		if err := json.Unmarshal([]byte(content[i:j+1]), &props); err == nil {
			parsed = true
			break
		} else {
			lastErr = err
		}
		next := strings.Index(content[i+1:], "[")
		if next < 0 {
			break
		}
		i += 1 + next
	}
	if !parsed {
		if lastErr == nil {
			lastErr = errors.New("no JSON array found")
		}
		return nil, fmt.Errorf("proposer did not return a JSON array of proposals: %w\n%s", lastErr, trim(content, 300))
	}
	if len(props) == 0 {
		return nil, errors.New("proposer returned no proposals")
	}
	return props, nil
}

// adoptProposals checks the proposals and adds them to the spec's variants (numbering names that collide). Proposals that fail are counted and skipped.
func adoptProposals(sp *spec.Spec, props []proposal) (added, skipped int, notes []string) {
	names := map[string]bool{"current": true}
	for _, v := range sp.Variants {
		names[v.Name] = true
	}
	for _, p := range props {
		name := regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(strings.TrimSpace(p.Name)), "-")
		name = strings.Trim(name, "-")
		if name == "" {
			name = "proposed"
		}
		base := name
		for i := 2; names[name]; i++ {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		v := spec.Variant{Name: name, Question: strings.TrimSpace(p.Question), Options: p.Options, Levels: p.Levels, Template: p.Template, Steps: p.Steps, Layout: p.Layout}
		trial := *sp
		trial.Variants = append(append([]spec.Variant(nil), sp.Variants...), v)
		if err := trial.Validate(); err != nil {
			skipped++
			notes = append(notes, fmt.Sprintf("skipped %q: %v", p.Name, err))
			continue
		}
		if err := sp.WithVariant(v).Validate(); err != nil {
			skipped++
			notes = append(notes, fmt.Sprintf("skipped %q: %v", p.Name, err))
			continue
		}
		sp.Variants = append(sp.Variants, v)
		names[name] = true
		added++
		if p.Rationale != "" {
			notes = append(notes, fmt.Sprintf("%s: %s", name, trim(p.Rationale, 120)))
		}
	}
	return added, skipped, notes
}

// writeRunPage writes tune's result to wiki/runs/ and adds a pointer and learnings to wiki/specs/<id>.md.
func writeRunPage(wikiDir string, sp *spec.Spec, model string, cands []candidate, scores []candScore, holdout float64, haveHoldout bool, decision, lesson string, rationales []string) (string, error) {
	if err := os.MkdirAll(filepath.Join(wikiDir, "runs"), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(wikiDir, "specs"), 0o755); err != nil {
		return "", err
	}
	day := time.Now().Format("2006-01-02")
	slug := modelSlug(model)
	name := fmt.Sprintf("%s-%s-%s.md", day, sp.ID, slug)
	path := filepath.Join(wikiDir, "runs", name)
	for i := 2; ; i++ {
		if _, err := os.Stat(path); err != nil {
			break
		}
		name = fmt.Sprintf("%s-%s-%s-%d.md", day, sp.ID, slug, i)
		path = filepath.Join(wikiDir, "runs", name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# tune %s on %s (%s)\n\n", sp.ID, model, day)
	// Link only the model and type pages that exist (linking to pages not in a new project's wiki would leave broken links in the user's wiki). The spec page is always created below
	fmt.Fprintf(&b, "spec: [%s](../specs/%s.md) · model page: %s · type: %s · truths: %d · hash before: %s\n\n", sp.ID, sp.ID,
		wikiLink(wikiDir, "models/"+slug+".md", slug), wikiLink(wikiDir, "types/"+string(sp.Type)+".md", string(sp.Type)), len(scores[0].p), sp.Hash())
	b.WriteString("| candidate | top-1 | brier | prec@act | rec@act | what changes |\n|---|---|---|---|---|---|\n")
	for i, s := range scores {
		fmt.Fprintf(&b, "| %s | %.3f | %.4f | %.3f | %.3f | %s |\n", s.name, s.acc, s.brier, s.prec, s.rec, strings.ReplaceAll(describeVariant(sp, cands[i].sp), "|", "\\|"))
	}
	if haveHoldout {
		fmt.Fprintf(&b, "\nholdout (pick on k-1, measure on 1): %.3f vs keeping current %.3f\n", holdout, scores[0].acc)
	}
	fmt.Fprintf(&b, "\n**Decision:** %s\n\n**Lesson:** %s\n", decision, lesson)
	if len(rationales) > 0 {
		b.WriteString("\nProposer's rationales:\n")
		for _, r := range rationales {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	b.WriteString("\n<!-- written by shikii tune. Edit freely; the Lesson line is what later proposals read. -->\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	// spec page: create it if missing (people write the Rules). Add one line to Runs
	specPage := filepath.Join(wikiDir, "specs", sp.ID+".md")
	data, err := os.ReadFile(specPage)
	if err != nil {
		data = []byte(fmt.Sprintf("# %s\n\n## Rules\n<!-- things every wording of this decision must keep; written by people -->\n\n## Runs\n", sp.ID))
	}
	page := string(data)
	if !strings.Contains(page, "## Runs") {
		page = strings.TrimRight(page, "\n") + "\n\n## Runs\n"
	}
	page = strings.TrimRight(page, "\n") + fmt.Sprintf("\n- [%s](../runs/%s) — %s. Lesson: %s\n", day+" "+slug, name, decision, lesson)
	if err := os.WriteFile(specPage, []byte(page), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
