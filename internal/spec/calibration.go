package spec

// CalibrationFor returns the calibration matching the current hash and model, or nil.
func (s *Spec) CalibrationFor(model string) *Calibration {
	h := s.Hash()
	for i := range s.Calibration {
		c := &s.Calibration[i]
		if c.Model == model && c.SpecHash == h {
			return c
		}
	}
	return nil
}

// PutCalibration replaces the calibration with the same (hash, model), or appends it.
func (s *Spec) PutCalibration(c Calibration) {
	// The same model keeps one line per step (the minimal question and, if any, the context step). Lines for old hashes (from before the wording changed) are replaced. Lines for other models are kept.
	kept := make([]Calibration, 0, len(s.Calibration)+1) // the copy (ContextSpec) shares the slice, so do not rewrite it in place
	for _, cur := range s.Calibration {
		if cur.Model != c.Model || (cur.SpecHash != c.SpecHash && s.LiveHash(cur.SpecHash)) {
			kept = append(kept, cur)
		}
	}
	s.Calibration = append(kept, c)
}

// HasContext reports whether there is a step that "adds context only when unsure".
func (s *Spec) HasContext() bool { return s.State.Context != "" }

// ContextSpec is the context question itself: a copy with template replaced by context and the added inputs made required.
// Its hash (Hash) differs, so its calibration is looked up separately. The calibration list and the file location are shared. nil when there is no context.
func (s *Spec) ContextSpec() *Spec {
	if !s.HasContext() {
		return nil
	}
	c := *s
	c.State = State{Template: s.State.Context, Required: append(append([]string(nil), s.State.Required...), s.State.ContextInputs()...)}
	c.Variants = nil
	c.Steps = copySteps(s.Steps)
	return &c
}

// ContextHash is the hash of the context step. Empty when there is none.
func (s *Spec) ContextHash() string {
	if c := s.ContextSpec(); c != nil {
		return c.Hash()
	}
	return ""
}

// LiveHash reports whether h is the hash of either step of the current wording (anything else is an old hash from before the wording changed).
func (s *Spec) LiveHash(h string) bool {
	return h == s.Hash() || (s.HasContext() && h == s.ContextHash())
}

// WithoutContext returns a copy that asks only the minimal question (same hash). Use it to measure without moving up a step, as in tune scoring or re-asking.
func (s *Spec) WithoutContext() *Spec {
	c := *s
	c.State.Context = ""
	return &c
}

// StateInputs returns only the inputs this spec's template uses (required and the template placeholders).
// The input hash is taken over these: even if the context inputs differ, the minimal question sees the same input.
func (s *Spec) StateInputs(in map[string]string) map[string]string {
	used := map[string]bool{}
	for _, r := range s.State.Required {
		used[r] = true
	}
	for _, p := range placeholders(s.State.Template) {
		used[p] = true
	}
	out := make(map[string]string, len(used))
	for k, v := range in {
		if used[k] {
			out[k] = v
		}
	}
	return out
}
