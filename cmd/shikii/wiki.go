package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
	"gopkg.in/yaml.v3"
)

// A wiki pattern page is "claim + conditions + evidence + retest procedure".
// The machine reads and writes the front matter (YAML between ---); the body belongs to people. Only people change the status (tentative / confirmed / rejected).
// `shikii wiki verify` runs the retest procedure (on which spec, which candidate beats which) with the current model and appends evidence to the front matter.
type claimPage struct {
	Claim     string            `yaml:"claim"`
	Status    string            `yaml:"status,omitempty"`
	AppliesTo map[string]string `yaml:"applies_to,omitempty"` // where it holds: type / models / lang, etc. (free text)
	Evidence  []claimEvidence   `yaml:"evidence,omitempty"`
	Retest    *claimRetest      `yaml:"retest,omitempty"`

	extra map[string]any // front-matter fields the machine does not know (author: etc.). Written back as read (comments are not kept)
}

type claimEvidence struct {
	Date    string `yaml:"date" json:"date"`
	Model   string `yaml:"model" json:"model"`
	Result  string `yaml:"result" json:"result"`
	Verdict string `yaml:"verdict" json:"verdict"` // supports | contradicts | inconclusive
	Run     string `yaml:"run,omitempty" json:"run,omitempty"`
}

// claimRetest is the retest procedure: the claim is supported if spec candidate better beats candidate than at top-1.
// A candidate name is a variant name in the spec, or "current".
type claimRetest struct {
	Spec   string `yaml:"spec"`
	Better string `yaml:"better"`
	Than   string `yaml:"than"`
}

// readClaimPage reads the front matter and body separately. Without front matter, an empty claim (no retest) and the whole body.
func readClaimPage(path string) (claimPage, string, error) {
	var pg claimPage
	b, err := os.ReadFile(path)
	if err != nil {
		return pg, "", err
	}
	s := string(b)
	if !strings.HasPrefix(s, "---\n") {
		return pg, s, nil
	}
	end := strings.Index(s[4:], "\n---\n")
	if end < 0 {
		return pg, s, nil
	}
	front := s[4 : 4+end]
	body := s[4+end+5:]
	if err := yaml.Unmarshal([]byte(front), &pg); err != nil {
		return pg, "", fmt.Errorf("%s: front matter: %w", path, err)
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(front), &raw); err == nil {
		for _, k := range []string{"claim", "status", "applies_to", "evidence", "retest"} {
			delete(raw, k)
		}
		if len(raw) > 0 {
			pg.extra = raw
		}
	}
	return pg, body, nil
}

// writeClaimPage rewrites the front matter and puts the body back as is.
func writeClaimPage(path string, pg claimPage, body string) error {
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&pg); err != nil {
		return err
	}
	_ = enc.Close()
	if len(pg.extra) > 0 {
		b, err := yaml.Marshal(pg.extra)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	buf.WriteString("---\n")
	buf.WriteString(body)
	// Atomically: even if cut off midway, no half-written page with broken front matter is left
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func cmdWiki(args []string) error {
	if len(args) > 0 && args[0] != "verify" {
		return cmdNotebook(args)
	}
	if len(args) == 0 || args[0] != "verify" {
		return errors.New("usage: shikii wiki verify [pattern.md ...] [--local URL] [--hosted URL] [--labeled file] [--store DIR]\n  runs claims that have a retest procedure (front matter of wiki/patterns/*.md) with the current model and appends evidence. People change the status")
	}
	fs := flag.NewFlagSet("wiki verify", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	storeDir := bindStore(fs)
	labeled := fs.String("labeled", "", "file of saved truths (default: the spec's eval.labeled)")
	paths, err := parseMixed(fs, args[1:])
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		paths, _ = filepath.Glob(filepath.Join(wikiDirOf(*storeDir), "patterns", "*.md"))
		sort.Strings(paths)
	}
	if len(paths) == 0 {
		return fmt.Errorf("no pattern pages under %s/patterns", wikiDirOf(*storeDir))
	}
	eps := ef.endpoints()
	st := store.Store{Dir: *storeDir}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	for _, path := range paths {
		pg, body, err := readClaimPage(path)
		if err != nil {
			return err
		}
		if pg.Retest == nil {
			fmt.Fprintf(stdout, "%s: no retest (not a verifiable claim yet)\n", filepath.Base(path))
			continue
		}
		ev, err := runRetest(ctx, pg.Retest, *labeled, st, eps, ef.tainted)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		pg.Evidence = append(pg.Evidence, ev)
		if err := writeClaimPage(path, pg, body); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: %s\n  retest %s: %s better than %s on %s: %s → %s\n", filepath.Base(path), pg.Claim, pg.Retest.Spec, pg.Retest.Better, pg.Retest.Than, ev.Model, ev.Result, ev.Verdict)
		models := map[string]bool{}
		contra := 0
		for _, e := range pg.Evidence {
			switch e.Verdict {
			case "supports":
				models[e.Model] = true
			case "contradicts":
				contra++
			}
		}
		fmt.Fprintf(stdout, "  evidence: %d supporting model(s), %d contradicting run(s); status %s", len(models), contra, pg.Status)
		if len(models) >= 2 && pg.Status != "confirmed" && contra == 0 {
			fmt.Fprint(stdout, " → confirmable (2 independent models support it; a person changes the status)")
		}
		fmt.Fprintln(stdout)
	}
	return nil
}

// runRetest scores the 2 candidates on the saved truths and judges by pairwise wins and losses and a sign test.
func runRetest(ctx context.Context, rt *claimRetest, labeled string, st store.Store, eps []decide.Endpoint, tainted bool) (claimEvidence, error) {
	sp, err := spec.Load(rt.Spec)
	if err != nil {
		return claimEvidence{}, err
	}
	if labeled == "" {
		labeled = sp.Eval.Labeled
	}
	bank, _, err := truthBank(sp, st, labeled)
	if err != nil {
		return claimEvidence{}, err
	}
	if len(bank) == 0 {
		return claimEvidence{}, errors.New("no truths to retest on (pass --labeled)")
	}
	pick := func(name string) (candidate, error) {
		if name == "current" {
			return candidate{"current", sp.WithoutContext()}, nil
		}
		for _, v := range sp.Variants {
			if v.Name == name {
				return candidate{name, sp.WithVariant(v).WithoutContext()}, nil
			}
		}
		return candidate{}, fmt.Errorf("retest: %q is not a variant of %s (nor \"current\")", name, rt.Spec)
	}
	a, err := pick(rt.Better)
	if err != nil {
		return claimEvidence{}, err
	}
	b, err := pick(rt.Than)
	if err != nil {
		return claimEvidence{}, err
	}
	sa, err := scoreCandidate(ctx, a, bank, eps, tainted)
	if err != nil {
		return claimEvidence{}, err
	}
	sb, err := scoreCandidate(ctx, b, bank, eps, tainted)
	if err != nil {
		return claimEvidence{}, err
	}
	wins, losses := 0, 0
	for i := range bank {
		if sa.correct[i] && !sb.correct[i] {
			wins++
		} else if !sa.correct[i] && sb.correct[i] {
			losses++
		}
	}
	p := signTestP(wins, losses)
	verdict := "inconclusive"
	switch {
	case sa.acc > sb.acc && p <= signTestAlpha:
		verdict = "supports"
	case sa.acc < sb.acc:
		verdict = "contradicts"
	}
	return claimEvidence{
		Date:    time.Now().Format("2006-01-02"),
		Model:   sb.model,
		Result:  fmt.Sprintf("%s %.3f vs %s %.3f (top-1 on %d truths, %d human; %d better / %d worse, p=%.3f)", rt.Better, sa.acc, rt.Than, sb.acc, len(bank), humanTruths(bank), wins, losses, p),
		Verdict: verdict,
	}, nil
}

// updateClaims grows the wiki from the scoring of a tune run (the model is not called again):
//  1. drafts a candidate that significantly beat current (pairwise wins and losses, one-sided sign test p ≤ 0.05) as a claim draft (wiki/patterns/drafts/<spec>-<candidate>.md, status draft).
//     If the same draft exists, evidence is appended. A person rewrites the draft into a general claim, moves it to patterns/ and sets the status
//  2. for existing claims with this spec as retest (patterns/*.md and drafts/*.md), if better and than are among this run's candidates, this run's scoring is appended as evidence
func updateClaims(wikiDir string, sp *spec.Spec, cands []candidate, scores []candScore, run string, human int) (drafted, updated int, err error) {
	byName := map[string]candScore{}
	for i, c := range cands {
		byName[c.name] = scores[i]
	}
	cur, ok := byName["current"]
	if !ok {
		return 0, 0, nil
	}
	specAbs, _ := filepath.Abs(sp.Path())
	specRel := sp.Path() // the path written in the draft is relative to the project root (where it runs). In the basic layout, decisions/<id>.yaml
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, specAbs); err == nil && !strings.HasPrefix(rel, "..") {
			specRel = rel
		}
	}
	sameSpec := func(p string) bool {
		abs, err := filepath.Abs(p)
		return err == nil && abs == specAbs
	}
	verdictOf := func(a, b candScore) (claimEvidence, string) {
		wins, losses := 0, 0
		for i := range a.correct {
			if i >= len(b.correct) {
				break
			}
			if a.correct[i] && !b.correct[i] {
				wins++
			} else if !a.correct[i] && b.correct[i] {
				losses++
			}
		}
		p := signTestP(wins, losses)
		v := "inconclusive"
		switch {
		case a.acc > b.acc && p <= signTestAlpha:
			v = "supports"
		case a.acc < b.acc:
			v = "contradicts"
		}
		return claimEvidence{Date: time.Now().Format("2006-01-02"), Model: b.model, Run: run, Verdict: v,
			Result: fmt.Sprintf("%s %.3f vs %s %.3f (top-1 on %d truths, %d human; %d better / %d worse, p=%.3f)", a.name, a.acc, b.name, b.acc, len(a.correct), human, wins, losses, p)}, v
	}
	// 2. append evidence to existing claims
	pages, _ := filepath.Glob(filepath.Join(wikiDir, "patterns", "*.md"))
	drafts, _ := filepath.Glob(filepath.Join(wikiDir, "patterns", "drafts", "*.md"))
	seen := map[string]bool{}
	for _, path := range append(pages, drafts...) {
		pg, body, err := readClaimPage(path)
		if err != nil || pg.Retest == nil || !sameSpec(pg.Retest.Spec) {
			continue
		}
		a, okA := byName[pg.Retest.Better]
		b, okB := byName[pg.Retest.Than]
		if !okA || !okB {
			continue
		}
		ev, _ := verdictOf(a, b)
		pg.Evidence = append(pg.Evidence, ev)
		if err := writeClaimPage(path, pg, body); err != nil {
			return drafted, updated, err
		}
		updated++
		seen[pg.Retest.Better+"\x00"+pg.Retest.Than] = true
	}
	// 1. draft candidates that won significantly (except those that already have a claim)
	for _, c := range cands {
		if c.name == "current" || seen[c.name+"\x00current"] {
			continue
		}
		ev, v := verdictOf(byName[c.name], cur)
		if v != "supports" {
			continue
		}
		path := filepath.Join(wikiDir, "patterns", "drafts", sp.ID+"-"+c.name+".md")
		pg := claimPage{
			Claim:     fmt.Sprintf("in %s, candidate %s is right more often than current (%s)", sp.ID, c.name, describeVariant(sp, c.sp)),
			Status:    "draft",
			AppliesTo: map[string]string{"type": string(sp.Type), "models": cur.model},
			Evidence:  []claimEvidence{ev},
			Retest:    &claimRetest{Spec: specRel, Better: c.name, Than: "current"},
		}
		body := fmt.Sprintf("# Draft: %s\n\nA claim the machine drafted from tune run %s. What a person does: work out where it holds (type, model family), generalize the claim, move it to `wiki/patterns/` and set the status to tentative. Delete it if it is not needed.\n\nThe candidate: %s\n", pg.Claim, run, describeVariant(sp, c.sp))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return drafted, updated, err
		}
		if err := writeClaimPage(path, pg, body); err != nil {
			return drafted, updated, err
		}
		drafted++
	}
	return drafted, updated, nil
}
