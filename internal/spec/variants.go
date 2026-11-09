package spec

// WithVariant returns a copy with the candidate applied (no path; the key changes).
func (s *Spec) WithVariant(v Variant) *Spec {
	c := *s
	c.Calibration = nil
	c.Variants = nil
	c.path = ""
	if v.Question != "" {
		c.Question = v.Question
	}
	if v.Template != "" {
		c.State = State{Template: v.Template, Required: append([]string(nil), s.State.Required...), Context: s.State.Context}
	}
	if len(v.Options) > 0 {
		c.Options = make([]Option, len(s.Options))
		for i, o := range s.Options {
			if d, ok := v.Options[o.ID]; ok {
				o.Description = d
			}
			c.Options[i] = o
		}
	}
	if len(v.Levels) > 0 {
		c.Levels = append([]string(nil), v.Levels...)
	}
	if len(v.Steps) > 0 {
		c.Steps = copySteps(v.Steps)
	} else {
		c.Steps = copySteps(s.Steps)
	}
	if v.Layout != "" {
		c.Layout = v.Layout
	}
	return &c
}

// Display is the screen translation for one language. Options maps a choice id (yes / no for noul) → display name; Levels holds the score levels (lowest first, same count as levels).
type Display struct {
	Title    string            `yaml:"title,omitempty"`
	Question string            `yaml:"question,omitempty"`
	Options  map[string]string `yaml:"options,omitempty"`
	Levels   []string          `yaml:"levels,omitempty"`
}
