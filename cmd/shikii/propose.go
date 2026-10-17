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

// 提案役と wiki。tune --propose N が使う。
//
//   - 提案役はチャット用の LLM(OpenAI 互換の /v1/chat/completions。本家 llama-server でも可)。
//     渡すのは、今の聞き方、混同表、外した実例、wiki の該当ページ(型・モデルの系統・確認済みのパターン・この spec の決まりと学び)。
//     返すのは候補の JSON。候補は spec の variants に残す(次回は提案無しでも採点できる。人も読める)。
//   - 本文(外した実例)は、spec が pii: true なら手元(loopback)の提案役にしか渡さない。
//   - wiki は markdown のフォルダ。tune は runs/ に証拠のページを書き、specs/<id>.md に案内を足す。
//     パターン(patterns/)と型(types/)とモデル(models/)のページは人が育てる。機械は読むだけ。

type proposal struct {
	Name      string            `json:"name"`
	Question  string            `json:"question,omitempty"`
	Options   map[string]string `json:"options,omitempty"`
	Levels    []string          `json:"levels,omitempty"`
	Template  string            `json:"template,omitempty"`
	Steps     []spec.Step       `json:"steps,omitempty"` // choice を はい/いいえ の列に分解する提案
	Rationale string            `json:"rationale,omitempty"`
}

// isLoopback は提案役が手元か(pii: true の本文を渡してよいか)。
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
	return ip.IsLoopback() || ip.IsUnspecified() // 0.0.0.0 / :: は「この機械の全ての口」= 手元
}

// proposerAuth は提案役に付ける鍵。hosted(TypeSafe / OpenRouter)の鍵は提案役には渡さない: 別の相手なので。
// 提案役に鍵が要るなら SHIKII_PROPOSER_KEY。手元の提案役には何も付けない。
func proposerAuth(base string) string {
	if isLoopback(base) {
		return ""
	}
	return os.Getenv("SHIKII_PROPOSER_KEY")
}

// wikiContext は提案役に渡す wiki の抜粋: 型・モデルの系統・確認済みパターン・この spec のページ。
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
	// 口の文脈は 4096 トークン程度のことが多いので、全体で 6000 文字ほどに抑える(型とモデルと spec を優先、パターンは残りで)
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

// modelSlug はモデル名から wiki のページ名を作る(量子化と配置の接尾辞は落とす)。
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

// buildProposerPrompt は提案役への依頼文。
// localProposer: 提案役が手元(loopback)か。遠い提案役には pii: true の本文も tainted な本文も見せない。
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
				continue // tainted な本文(本番のデータに触った作業で記録した)は手元の提案役にしか見せない
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

// askProposer はチャット用の口に投げて、JSON の候補を取り出す。
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
		// Qwen 系の「考える」モードは答えの前に長い思考を吐き、JSON が切れる。llama.cpp はこの kwargs で止められる(知らない口は無視する)
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
		content = out.Choices[0].Message.Reasoning // 考えだけで終わった口。JSON が中にあれば拾う
	}
	props, err := parseProposals(content)
	if err != nil && out.Choices[0].FinishReason == "length" {
		err = fmt.Errorf("%w (the proposer ran out of tokens; the prompt may be too long for its context, try a bigger -c or fewer wiki pages)", err)
	}
	return props, content, err
}

// parseProposals は返事から JSON 配列を取り出す(``` で囲まれていても、前後に文があっても)。
func parseProposals(content string) ([]proposal, error) {
	var props []proposal
	var lastErr error
	parsed := false
	// 前置きに [ があっても良い: 配列として読める最初の [ … 最後の ] を探す
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

// adoptProposals は提案を検証して spec の variants に足す(名前が重なれば番号を付ける)。通らない提案は数えて飛ばす。
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
		v := spec.Variant{Name: name, Question: strings.TrimSpace(p.Question), Options: p.Options, Levels: p.Levels, Template: p.Template, Steps: p.Steps}
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

// writeRunPage は tune の結果を wiki/runs/ に書き、wiki/specs/<id>.md に案内と学びを足す。
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
	fmt.Fprintf(&b, "spec: [%s](../specs/%s.md) · model page: [%s](../models/%s.md) · type: [%s](../types/%s.md) · truths: %d · hash before: %s\n\n", sp.ID, sp.ID, slug, slug, sp.Type, sp.Type, len(scores[0].p), sp.Hash())
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
	// spec ページ: 無ければ作る(Rules は人が書く)。Runs に 1 行足す
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
