package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/hayaenfj/shikii/internal/spec"
	"gopkg.in/yaml.v3"
)

// shikii new: answer a few questions and you get a spec. Anything passed as a flag is not asked; only what is missing is asked on stdin.
// --from starts from a sample in examples/. Validate runs before writing; if it fails, nothing is written.

var stdin io.Reader = os.Stdin

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func cmdNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	dir := fs.String("dir", filepath.Join(".shikii", "decisions"), "folder to write to (default .shikii/decisions)")
	from := fs.String("from", "", "spec to use as a sample (e.g. examples/*.yaml). Only the id and what you pass as flags change")
	typ := fs.String("type", "", "choice | noul | score")
	question := fs.String("question", "", "question text (shown to the model; English or Japanese)")
	var options, levels, inputs multiFlag
	fs.Var(&options, "option", "a choice option id=description (repeatable)")
	fs.Var(&levels, "level", "a score level (lowest first, repeatable)")
	fs.Var(&inputs, "input", "name of an input filled into the text (repeatable; the template lists them by name)")
	pii := fs.String("pii", "", "true: the text may contain personal information (never sent to an outside AI) | false")
	sendTo := fs.String("send-to", "", "local | hosted | local,hosted (where it may be sent; the first one wins)")
	act := fs.Float64("act", 0.75, "at or above this it acts automatically (only once calibrated)")
	ask := fs.Float64("ask", 0.60, "at or above this it asks a person")
	minN := fs.Int("min-n", 50, "number of truths needed to draw the line")
	titleJa := fs.String("title-ja", "", "Japanese title shown on the page (optional)")
	force := fs.Bool("force", false, "overwrite a file with the same name")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: shikii new <id> [--type choice|noul|score] [--question ...] [--option id=desc ...] [--input name ...] [--pii true|false] [--send-to local|hosted] [--from spec.yaml]")
	}
	explicitDir := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "dir" {
			explicitDir = true
		}
	})
	if !explicitDir {
		root, _, err := currentProjectStore()
		if err != nil {
			return err
		}
		*dir = specsDirOf(root)
	}
	id := pos[0]
	path := filepath.Join(*dir, id+".yaml")
	if _, err := os.Stat(path); err == nil && !*force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}

	sp := &spec.Spec{Version: 1, Eval: spec.Eval{MinN: *minN}}
	if *from != "" {
		base, err := spec.Load(*from)
		if err != nil {
			return fmt.Errorf("--from %s: %w", *from, err)
		}
		sp = base
		sp.Calibration = nil // do not carry over the sample's line (different wording, different use)
		sp.Version = 1
	}
	sp.ID = id
	in := bufio.NewReader(stdin)
	askLine := func(prompt string) (string, error) {
		fmt.Fprint(stdout, prompt+" ")
		line, err := in.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			if err != nil {
				return "", io.EOF
			}
			return "", nil
		}
		return line, nil
	}
	need := func(have string, prompt, what string) (string, error) {
		if have != "" {
			return have, nil
		}
		v, err := askLine(prompt)
		if err != nil || v == "" {
			return "", fmt.Errorf("missing %s (pass --%s or answer the prompt)", what, what)
		}
		return v, nil
	}

	// type
	if *typ == "" && sp.Type == "" {
		v, err := need("", "Type? (choice = pick one option / noul = yes-no / score = ordered levels):", "type")
		if err != nil {
			return err
		}
		*typ = v
	}
	if *typ != "" {
		sp.Type = spec.Type(*typ)
	}
	// question
	if *question == "" && sp.Question == "" {
		v, err := need("", "Question the model should answer (one sentence; say what counts and what does not):", "question")
		if err != nil {
			return err
		}
		*question = v
	}
	if *question != "" {
		sp.Question = *question
	}
	// options / levels
	switch sp.Type {
	case spec.Choice:
		if len(options) > 0 {
			sp.Options = nil
			for _, o := range options {
				idDesc := strings.SplitN(o, "=", 2)
				if len(idDesc) != 2 {
					return fmt.Errorf("--option must be id=description, got %q", o)
				}
				sp.Options = append(sp.Options, spec.Option{ID: strings.TrimSpace(idDesc[0]), Description: strings.TrimSpace(idDesc[1])})
			}
		}
		for len(sp.Options) < 2 && *from == "" {
			v, err := askLine(fmt.Sprintf("Option %d as id=description (empty line to finish; at least 2):", len(sp.Options)+1))
			if err != nil || v == "" {
				break
			}
			idDesc := strings.SplitN(v, "=", 2)
			if len(idDesc) != 2 {
				return fmt.Errorf("option must be id=description, got %q", v)
			}
			sp.Options = append(sp.Options, spec.Option{ID: strings.TrimSpace(idDesc[0]), Description: strings.TrimSpace(idDesc[1])})
		}
	case spec.Score:
		if len(levels) > 0 {
			sp.Levels = append([]string(nil), levels...)
		}
	}
	// inputs and template
	if len(inputs) == 0 && len(sp.State.Required) == 0 {
		v, err := need("", "Input name(s) the text is built from, comma-separated (e.g. message, or report1,report2):", "input")
		if err != nil {
			return err
		}
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				inputs = append(inputs, p)
			}
		}
	}
	if len(inputs) > 0 {
		sp.State.Required = append([]string(nil), inputs...)
		if len(inputs) == 1 {
			sp.State.Template = "{{ " + inputs[0] + " }}\n"
		} else {
			var b strings.Builder
			for i, name := range inputs {
				if i > 0 {
					b.WriteString("\n")
				}
				fmt.Fprintf(&b, "%s: {{ %s }}\n", strings.ToUpper(name[:1])+name[1:], name)
			}
			sp.State.Template = b.String()
		}
	}
	// destinations and pii
	if *sendTo == "" && len(sp.Policy.SendTo) == 0 {
		v, err := need("", "Where may the text be sent? (local = a model on this machine / hosted = a provider's API / local,hosted):", "send-to")
		if err != nil {
			return err
		}
		*sendTo = v
	}
	if *sendTo != "" {
		sp.Policy.SendTo = nil
		for _, d := range strings.Split(*sendTo, ",") {
			if d = strings.TrimSpace(d); d != "" {
				sp.Policy.SendTo = append(sp.Policy.SendTo, d)
			}
		}
	}
	if *pii == "" && sp.Policy.PII == nil {
		v, err := need("", "Can the text contain personal information? (yes = never sent to a hosted endpoint / no):", "pii")
		if err != nil {
			return err
		}
		*pii = v
	}
	if *pii != "" {
		switch strings.ToLower(*pii) {
		case "true", "yes", "y":
			t := true
			sp.Policy.PII = &t
		case "false", "no", "n":
			f := false
			sp.Policy.PII = &f
		default:
			return fmt.Errorf("--pii must be true or false, got %q", *pii)
		}
	}
	// thresholds (without --from, the flag values; with it, the sample's values, overridden when given explicitly)
	if *from == "" || isSet(fs, "act") {
		sp.Thresholds.Act = *act
	}
	if *from == "" || isSet(fs, "ask") {
		sp.Thresholds.Ask = *ask
	}
	if *from == "" || isSet(fs, "min-n") {
		sp.Eval.MinN = *minN
	}
	if *titleJa != "" {
		if sp.Display == nil {
			sp.Display = map[string]spec.Display{}
		}
		d := sp.Display["ja"]
		d.Title = *titleJa
		sp.Display["ja"] = d
	}
	if err := sp.Validate(); err != nil {
		return fmt.Errorf("the spec is not complete: %w", err)
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(sp)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("# %s: made by `shikii new`. The calibration block is written by `shikii calibrate --write`; never by hand.\n", id)
	if err := os.WriteFile(path, []byte(header+string(b)), 0o644); err != nil {
		return err
	}
	// what to do next, in a form you can type as is
	var example []string
	for _, name := range sp.State.Required {
		example = append(example, "-i "+name+"='…'")
	}
	fmt.Fprintf(stdout, "wrote %s (hash=%s)\n", path, sp.Hash())
	fmt.Fprintf(stdout, "try it:   shikii decide %s --local http://127.0.0.1:8080 %s\n", path, strings.Join(example, " "))
	fmt.Fprintf(stdout, "then:     shikii serve --specs %s   # answer what was true on http://127.0.0.1:8095/ ; the line draws itself at %d answers\n", *dir, sp.Eval.MinN)
	return nil
}

func isSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}
