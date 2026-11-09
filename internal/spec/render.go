package spec

import (
	"sort"
	"strings"
)

// Render fills the template with the inputs. It returns the names of missing required inputs.
func (s *Spec) Render(inputs map[string]string) (state string, missing []string) {
	for _, r := range s.State.Required {
		if strings.TrimSpace(inputs[r]) == "" {
			missing = append(missing, r)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return "", missing
	}
	state = s.State.Template
	for k, v := range inputs {
		state = strings.ReplaceAll(state, "{{ "+k+" }}", v)
		state = strings.ReplaceAll(state, "{{"+k+"}}", v)
	}
	return state, nil
}
