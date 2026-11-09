package spec

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash is the hash that keys calibration. It is built from the question, type, state template, options and levels.
// Thresholds and policy are left out (changing them keeps the calibration valid).
func (s *Spec) Hash() string {
	h := sha256.New()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	// thresholds.level decides the band and is not part of the calibration key (same as the thresholds)
	w(string(s.Type), s.Question, s.State.Template)
	for _, o := range s.Options {
		w(o.ID, o.Description)
	}
	for _, l := range s.Levels {
		w(l)
	}
	for _, st := range s.Steps { // the breakdown is part of the wording. Specs without one keep their key
		w("step", st.Ask, st.Yes, st.No)
	}
	if s.Layout != "" { // the arrangement is part of the wording too. Specs without one keep their key
		w("layout", s.Layout)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
