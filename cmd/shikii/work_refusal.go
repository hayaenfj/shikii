package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Refusals of work that name the cause and the next call. Before, each said only that something was wrong ("unknown prepared memory
// reference", "prepare reads context; send content with record"), so the agent's next call repeated the guess.

// workContentFields names the fields of a request that carry content for record. prepare takes none of them.
func workContentFields(req workRequest) []string {
	var sent []string
	for _, field := range []struct {
		name string
		sent bool
	}{
		{"result", req.Result != nil}, {"reading", req.Reading != nil}, {"trial", req.Trial != nil}, {"comparison", req.Comparison != nil},
		{"agreed", req.Agreed != nil}, {"open", req.Open != nil}, {"continues_previous", req.ContinuesPrevious != nil},
		{"observations", len(req.Observations) > 0}, {"notes", len(req.Notes) > 0}, {"reviews", len(req.Reviews) > 0}, {"memory", len(req.Memory) > 0},
		{"reviewed", len(req.Reviewed) > 0}, {"maintenance", len(req.Maintenance) > 0}, {"discoveries", len(req.Discoveries) > 0}, {"outcomes", len(req.Outcomes) > 0},
	} {
		if field.sent {
			sent = append(sent, field.name)
		}
	}
	return sent
}

// sortedRefs lists prepared references in reading order: by name, and by number within a name (memory2 before memory10).
func sortedRefs[T any](refs map[string]T) []string {
	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}
	sortRefs(out)
	return out
}

func sortRefs(refs []string) {
	split := func(ref string) (string, int) {
		end := len(ref)
		for end > 0 && ref[end-1] >= '0' && ref[end-1] <= '9' {
			end--
		}
		n, _ := strconv.Atoi(ref[end:])
		return ref[:end], n
	}
	sort.Slice(refs, func(i, j int) bool {
		nameI, numberI := split(refs[i])
		nameJ, numberJ := split(refs[j])
		if nameI != nameJ {
			return nameI < nameJ
		}
		if numberI != numberJ {
			return numberI < numberJ
		}
		return refs[i] < refs[j]
	})
}

// listRefs shows at most 12 references and says how many there are when it shows fewer.
func listRefs(refs []string) string {
	if len(refs) <= 12 {
		return strings.Join(refs, ", ")
	}
	return strings.Join(refs[:12], ", ") + fmt.Sprintf(", ... (%d in all)", len(refs))
}

// unknownPreparedRef refuses a memory or review reference the receipt did not prepare: it lists the ones it did, or says to leave
// the field out when it prepared none (an agent often tries to assess a lesson it read by its knowledge ref or lesson id).
func unknownPreparedRef(kind, ref string, prepared []string, where string) string {
	text := fmt.Sprintf("unknown prepared %s reference %q", kind, ref)
	if len(prepared) > 0 {
		return text + "; use the refs of " + where + " in the reply of prepare: " + listRefs(prepared)
	}
	if kind == "memory" {
		return text + ": this receipt prepared no memory to assess, so leave memory out"
	}
	return text + ": this receipt prepared no " + kind + ", so leave " + kind + "s out"
}

// unknownRelatedArticle refuses a related entry that is not an article the receipt read, with the refs it could use and how to
// read another page into the receipt.
func unknownRelatedArticle(ref string, read []string) string {
	text := fmt.Sprintf("unknown related article %q", ref)
	if len(read) > 0 {
		text += "; related takes the articles this receipt read: " + listRefs(read)
	} else {
		text += "; related takes the articles this receipt read, and it read none"
	}
	return text + fmt.Sprintf(" (to relate another page, read it first: prepare with this receipt and query lesson:%s; or leave it out)", ref)
}

// unknownSourceRef refuses a source reference of a note, result or judgment. newN names observation N of the same call: when that
// observation was refused, the note citing it is refused with it, and the fix is the observation's own error.
func unknownSourceRef(ref string, observations int, usable []string) string {
	text := fmt.Sprintf("unknown source reference %q", ref)
	if number, ok := strings.CutPrefix(ref, "new"); ok {
		if n, err := strconv.Atoi(number); err == nil && n >= 1 {
			if n <= observations {
				return text + fmt.Sprintf(": observation %d of this call was not saved (see its error); correct it and resend it with what cites it", n)
			}
			return text + fmt.Sprintf(": this call has %d observations, and newN names observation N of the same call", observations)
		}
	}
	if len(usable) == 0 {
		return text + "; add an observation to this call and cite it as new1"
	}
	usable = append([]string{}, usable...)
	sortRefs(usable)
	return text + "; usable here: " + listRefs(usable) + " (and newN for observation N of the same call)"
}
