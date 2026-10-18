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

// wiki のパターンのページは「主張 + 条件 + 証拠 + 再検証の手順」。
// 前書き(--- で囲んだ YAML)を機械が読み書きし、本文は人のもの。状態(tentative / confirmed / rejected)を変えるのは人だけ。
// `shikii wiki verify` は再検証の手順(どの spec で、どの候補がどれを上回るか)を今のモデルで回し、証拠を前書きに足す。
type claimPage struct {
	Claim     string            `yaml:"claim"`
	Status    string            `yaml:"status,omitempty"`
	AppliesTo map[string]string `yaml:"applies_to,omitempty"` // type / models / lang など、効く範囲(自由な文)
	Evidence  []claimEvidence   `yaml:"evidence,omitempty"`
	Retest    *claimRetest      `yaml:"retest,omitempty"`
}

type claimEvidence struct {
	Date    string `yaml:"date"`
	Model   string `yaml:"model"`
	Result  string `yaml:"result"`
	Verdict string `yaml:"verdict"` // supports | contradicts | inconclusive
	Run     string `yaml:"run,omitempty"`
}

// claimRetest は再検証の手順: spec の候補 better が候補 than を top-1 で上回れば主張は支持される。
// 候補の名前は spec の variants の name か "current"。
type claimRetest struct {
	Spec   string `yaml:"spec"`
	Better string `yaml:"better"`
	Than   string `yaml:"than"`
}

// readClaimPage は前書きと本文を分けて読む。前書きが無ければ空の主張(retest 無し)と本文全体。
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
	return pg, body, nil
}

// writeClaimPage は前書きを書き直し、本文はそのまま戻す。
func writeClaimPage(path string, pg claimPage, body string) error {
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&pg); err != nil {
		return err
	}
	_ = enc.Close()
	buf.WriteString("---\n")
	buf.WriteString(body)
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func cmdWiki(args []string) error {
	if len(args) == 0 || args[0] != "verify" {
		return errors.New("usage: shikii wiki verify [pattern.md ...] [--local URL] [--hosted URL] [--labeled file] [--store DIR]\n  再検証の手順を持つ主張(wiki/patterns/*.md の前書き)を今のモデルで回し、証拠を書き足す。状態は人が変える")
	}
	fs := flag.NewFlagSet("wiki verify", flag.ContinueOnError)
	var ef endpointFlags
	ef.bind(fs)
	storeDir := bindStore(fs)
	labeled := fs.String("labeled", "", "正解の貯金のファイル(省略時は spec の eval.labeled)")
	paths, err := parseMixed(fs, args[1:])
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		paths, _ = filepath.Glob(filepath.Join(wikiRoot, "patterns", "*.md"))
		sort.Strings(paths)
	}
	if len(paths) == 0 {
		return fmt.Errorf("no pattern pages under %s/patterns", wikiRoot)
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

// runRetest は 2 つの候補を貯金で採点し、対の勝敗と符号検定で判定する。
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
		Result:  fmt.Sprintf("%s %.3f vs %s %.3f (top-1 on %d truths; %d better / %d worse, p=%.3f)", rt.Better, sa.acc, rt.Than, sb.acc, len(bank), wins, losses, p),
		Verdict: verdict,
	}, nil
}
