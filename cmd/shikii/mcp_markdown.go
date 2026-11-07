package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type knowledgeText struct {
	value string
	paths []string
}

type knowledgeTextView struct {
	texts  []knowledgeText
	index  map[string]int
	paths  int
	counts map[string]int
}

// This is a presentation of the complete result, not a summary. Repeated strings
// share a verbatim text section; JSON Pointers retain every original location.
// The original object remains untouched for structuredContent. Deep, large and
// control-bearing values keep their ordinary JSON representation.
func knowledgeMarkdown(raw []byte, original map[string]any) string {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return strings.TrimSpace(string(raw))
	}
	baseline := compact.String()
	if len(raw) > 1<<20 {
		return baseline
	}
	v := knowledgeTextView{index: map[string]int{}, counts: map[string]int{}}
	countKnowledgeText(original, v.counts, 0)
	repeated := false
	for _, count := range v.counts {
		repeated = repeated || count > 1
	}
	if !repeated {
		return baseline
	}
	metadata, _ := v.extract(original, "", 0)
	if len(v.texts) == 0 {
		return baseline
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return baseline
	}
	var out strings.Builder
	out.WriteString("# Knowledge\n\nMetadata is JSON. Text sections supply original string values at their JSON Pointers; null array slots at these paths are placeholders. Original field meanings and trust boundaries still apply. structuredContent retains the complete JSON result.\n\n```json\n")
	out.Write(encoded)
	out.WriteString("\n```\n")
	for i, text := range v.texts {
		paths, _ := json.Marshal(text.paths)
		fmt.Fprintf(&out, "\n## Text %d\nFields: %s\nUTF-8 bytes: %d\n", i+1, paths, len(text.value))
		fence := knowledgeTextFence(text.value)
		out.WriteString(fence + "text\n")
		out.WriteString(text.value)
		out.WriteString("\n" + fence + "\n")
	}
	// A byte threshold is only a conservative presentation gate, not a claim
	// about model tokens or accuracy. Compare actual tokenizers before shipping.
	if out.Len()+512 > len(baseline) {
		return baseline
	}
	return out.String()
}

func countKnowledgeText(value any, counts map[string]int, depth int) {
	if depth > 64 {
		return
	}
	switch value := value.(type) {
	case string:
		if len(value) >= 256 {
			counts[value]++
		}
	case map[string]any:
		for _, child := range value {
			countKnowledgeText(child, counts, depth+1)
		}
	case []any:
		for _, child := range value {
			countKnowledgeText(child, counts, depth+1)
		}
	}
}

func (v *knowledgeTextView) extract(value any, path string, depth int) (any, bool) {
	if depth > 64 {
		return value, false
	}
	switch value := value.(type) {
	case string:
		if v.counts[value] < 2 || v.paths >= 1024 || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 && r != '\n' && r != '\t' }) >= 0 {
			return value, false
		}
		i, exists := v.index[value]
		if !exists {
			if len(v.texts) >= 128 {
				return value, false
			}
			i = len(v.texts)
			v.index[value] = i
			v.texts = append(v.texts, knowledgeText{value: value})
		}
		v.texts[i].paths = append(v.texts[i].paths, path)
		v.paths++
		return nil, true
	case map[string]any:
		result := make(map[string]any, len(value))
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			part := strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
			child, extracted := v.extract(value[key], path+"/"+part, depth+1)
			if !extracted {
				result[key] = child
			}
		}
		return result, false
	case []any:
		result := make([]any, len(value))
		for i, child := range value {
			result[i], _ = v.extract(child, path+"/"+strconv.Itoa(i), depth+1)
		}
		return result, false
	}
	return value, false
}

func knowledgeTextFence(text string) string {
	longest, run := 2, 0
	for i := 0; i < len(text); i++ {
		if text[i] == '~' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("~", longest+1)
}
