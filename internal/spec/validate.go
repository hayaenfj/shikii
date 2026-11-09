package spec

import (
	"fmt"
	"strings"
)

// Validate checks that the spec is consistent.
func (s *Spec) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("id is required")
	}
	if strings.TrimSpace(s.Question) == "" {
		return fmt.Errorf("question is required")
	}
	switch s.Type {
	case Noul:
	case Choice:
		if len(s.Options) < 2 {
			return fmt.Errorf("choice needs at least 2 options")
		}
		for _, o := range s.Options {
			if o.ID == "" || strings.TrimSpace(o.Description) == "" {
				return fmt.Errorf("choice option %q needs id and description", o.ID)
			}
		}
	case Score:
		if len(s.Levels) < 2 || len(s.Levels) > 10 {
			return fmt.Errorf("score needs 2..10 levels, got %d", len(s.Levels))
		}
	default:
		return fmt.Errorf("unknown type %q", s.Type)
	}
	if err := validateSteps(s.Type, s.Options, s.Steps); err != nil {
		return err
	}
	if err := validateLayout(s.Layout); err != nil {
		return err
	}
	if strings.TrimSpace(s.State.Template) == "" {
		return fmt.Errorf("state.template is required")
	}
	if err := s.State.validateContext(); err != nil {
		return err
	}
	if len(s.Policy.SendTo) == 0 {
		return fmt.Errorf("policy.send_to is required")
	}
	for _, d := range s.Policy.SendTo {
		if d != "hosted" && d != "local" {
			return fmt.Errorf("policy.send_to: unknown destination %q", d)
		}
	}
	if s.Eval.MinN < 1 {
		return fmt.Errorf("eval.min_n must be at least 1 (how many truths before a calibration may be written; 50 is a sensible start)")
	}
	if s.Policy.PII == nil {
		return fmt.Errorf("policy.pii is required: true (the text can contain personal information: never sent to a hosted endpoint) or false (it cannot). shikii does not decide what counts as personal information; you do")
	}
	if !(0 < s.Thresholds.Ask && s.Thresholds.Ask <= s.Thresholds.Act && s.Thresholds.Act <= 1) {
		return fmt.Errorf("thresholds must satisfy 0 < ask <= act <= 1")
	}
	if s.Thresholds.Level != 0 {
		if s.Type != Score {
			return fmt.Errorf("thresholds.level is only for score")
		}
		if s.Thresholds.Level < 1 || s.Thresholds.Level > len(s.Levels)-1 {
			return fmt.Errorf("thresholds.level must be in 1..%d", len(s.Levels)-1)
		}
	}
	for lang, d := range s.Display {
		for id := range d.Options {
			if !s.hasTruthID(id) {
				return fmt.Errorf("display.%s.options: %q is not an option of this spec", lang, id)
			}
		}
		if len(d.Levels) > 0 && len(d.Levels) != len(s.Levels) {
			return fmt.Errorf("display.%s.levels: %d entries, spec has %d levels", lang, len(d.Levels), len(s.Levels))
		}
	}
	names := map[string]bool{}
	for i, v := range s.Variants {
		if strings.TrimSpace(v.Name) == "" || names[v.Name] || v.Name == "current" {
			return fmt.Errorf("variants[%d]: name must be unique, non-empty and not \"current\"", i)
		}
		names[v.Name] = true
		if v.Question == "" && len(v.Options) == 0 && len(v.Levels) == 0 && v.Template == "" && len(v.Steps) == 0 && v.Layout == "" {
			return fmt.Errorf("variants[%d] (%s): changes nothing", i, v.Name)
		}
		if err := validateSteps(s.Type, s.Options, v.Steps); err != nil {
			return fmt.Errorf("variants[%d] (%s).%w", i, v.Name, err)
		}
		if err := validateLayout(v.Layout); err != nil {
			return fmt.Errorf("variants[%d] (%s).%w", i, v.Name, err)
		}
		for id := range v.Options {
			if s.Type != Choice || !s.hasTruthID(id) {
				return fmt.Errorf("variants[%d] (%s).options: %q is not an option of this spec", i, v.Name, id)
			}
		}
		if len(v.Levels) > 0 && len(v.Levels) != len(s.Levels) {
			return fmt.Errorf("variants[%d] (%s).levels: %d entries, spec has %d levels", i, v.Name, len(v.Levels), len(s.Levels))
		}
		if v.Template != "" {
			for _, r := range s.State.Required {
				if !strings.Contains(v.Template, "{{ "+r+" }}") && !strings.Contains(v.Template, "{{"+r+"}}") {
					return fmt.Errorf("variants[%d] (%s).template: does not mention required input %q", i, v.Name, r)
				}
			}
		}
		// The name goes into wiki/runs file names and tables: separators, spaces and overly long names are rejected
		if v.Name == "" || len(v.Name) > 60 || strings.ContainsAny(v.Name, "/\\ \t\r\n") || strings.Contains(v.Name, "..") {
			return fmt.Errorf("variants[%d].name %q: 1 to 60 characters, no spaces, slashes or \"..\" (it becomes part of a file name)", i, v.Name)
		}
	}
	return nil
}

// hasTruthID reports whether an id a screen translation points to exists in the spec (option id for choice, yes / no for noul).
// PIISensitive reports whether the text is declared as possibly holding personal information (never sent to hosted).
func (s *Spec) PIISensitive() bool { return s.Policy.PII != nil && *s.Policy.PII }

func (s *Spec) hasTruthID(id string) bool {
	switch s.Type {
	case Noul:
		return id == "yes" || id == "no"
	case Choice:
		for _, o := range s.Options {
			if o.ID == id {
				return true
			}
		}
	}
	return false
}

// TruthKeys returns every raw key (yes/no for noul, option ids for choice, level numbers for score).
func (s *Spec) TruthKeys() []string {
	switch s.Type {
	case Noul:
		return []string{"yes", "no"}
	case Score:
		out := make([]string, len(s.Levels))
		for i := range s.Levels {
			out[i] = fmt.Sprint(i)
		}
		return out
	}
	out := make([]string, len(s.Options))
	for i, o := range s.Options {
		out[i] = o.ID
	}
	return out
}

// HasTruthKey reports whether id is a valid raw key: yes/no for noul, an option id for choice, a level number ("0".."n-1") for score.
func (s *Spec) HasTruthKey(id string) bool {
	if s.Type == Score {
		for i := range s.Levels {
			if id == fmt.Sprint(i) {
				return true
			}
		}
		return false
	}
	return s.hasTruthID(id)
}
