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
	return fillTemplate(s.State.Template, inputs), nil
}

// fillTemplate fills each {{ name }} of the template in one pass, left to right. The filled text is never read
// again, so braces inside an input stay as the person wrote them and the result does not depend on map order.
// Any spacing inside the braces is accepted (the same placeholders the inputs hash counts); a placeholder with no
// input is left as written.
func fillTemplate(tpl string, inputs map[string]string) string {
	var b strings.Builder
	for {
		end := strings.Index(tpl, "}}")
		if end < 0 {
			break
		}
		open := strings.LastIndex(tpl[:end], "{{")
		if open >= 0 {
			if v, ok := inputs[strings.TrimSpace(tpl[open+2:end])]; ok {
				b.WriteString(tpl[:open])
				b.WriteString(v)
				tpl = tpl[end+2:]
				continue
			}
		}
		b.WriteString(tpl[:end+2])
		tpl = tpl[end+2:]
	}
	b.WriteString(tpl)
	return b.String()
}
