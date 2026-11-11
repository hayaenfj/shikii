package main

import (
	"encoding/json"
	"strconv"
)

// limitReflectQuery makes the reply of reflect query fit what an agent's tool shows. It had no bound: with long lessons close to
// the oldest conversations it reached 86 KB in this project, the agent's MCP client refused it whole and its shell cut it in the
// middle. What the agent needs to submit always stays: every conversation with its targets and the keys of its delivered lessons,
// and how_to_submit. The rest goes in steps, the least useful first, until the reply fits:
//
//  1. the bodies and details of the close lessons (title, lesson, applies_to, kind, scene and the exact query stay)
//  2. the details of the maintenance tasks (page, revision, key, title and what to do stay)
//  3. the examples of earlier corrections
//  4. the close lessons at the end of the list
//  5. the raw text of the conversations after the first, which become heads like the ones after them
//
// output names each step taken and how to read what was left out. A reply that fits is returned as it is. size measures the whole
// reply the way the transport sends it (the MCP tool result, or the CLI's line).
func limitReflectQuery(view reflectView, budget int, size func(any) int) any {
	if size(view) <= budget {
		return view
	}
	build := func(v reflectView, level, lessons int) (map[string]any, []string) {
		data, err := json.Marshal(v)
		var reply map[string]any
		if err != nil || json.Unmarshal(data, &reply) != nil {
			return nil, nil
		}
		var omitted []string
		if list, ok := reply["lessons"].([]any); ok && len(list) > 0 {
			for i, raw := range list {
				page, _ := raw.(map[string]any)
				light := map[string]any{"body_included": false}
				for _, key := range []string{"id", "revision", "title", "lesson", "applies_to", "kind", "scene"} {
					if value, ok := page[key]; ok {
						light[key] = value
					}
				}
				id, _ := page["id"].(string)
				revision, _ := page["revision"].(string)
				light["query"] = map[string]any{"operation": "query", "query": "lesson:" + id + "@" + revision}
				list[i] = light
			}
			omitted = append(omitted, "lessons[].body and details (each query reads its lesson whole)")
			if level >= 4 && lessons < len(list) {
				reply["lessons"] = list[:lessons]
				omitted = append(omitted, "lessons after the first "+strconv.Itoa(lessons))
			}
		}
		if tasks, ok := reply["maintenance"].([]any); ok && len(tasks) > 0 && level >= 2 {
			for i, raw := range tasks {
				task, _ := raw.(map[string]any)
				light := map[string]any{}
				for _, key := range []string{"page_id", "revision", "key", "title", "next_action"} {
					if value, ok := task[key]; ok {
						light[key] = value
					}
				}
				tasks[i] = light
			}
			omitted = append(omitted, "maintenance[] details (wiki query without words lists the tasks whole)")
		}
		if _, ok := reply["human_corrections"]; ok && level >= 3 {
			delete(reply, "human_corrections")
			omitted = append(omitted, "human_corrections")
		}
		return reply, omitted
	}
	// try builds the reply with the steps up to level taken (lessons: how many close lessons stay at level 4; heads: how many
	// conversations the bound made heads), adds output and measures the whole.
	try := func(v reflectView, level, lessons, heads int, note ...string) (map[string]any, bool) {
		reply, omitted := build(v, level, lessons)
		if reply == nil {
			return nil, false
		}
		omitted = append(headsOmitted(omitted, view, heads), note...)
		reply["output"] = map[string]any{"limited": true, "max_bytes": budget, "omitted": omitted,
			"next_action": "Submit what is here: every conversation, target and delivered lesson is listed. Read a lesson left without its body with its query, and a conversation that came as a head with wiki query <its id>; query again after the submit for what is left."}
		return reply, size(reply) <= budget
	}
	for level := 1; level <= 3; level++ {
		reply, fits := try(view, level, 0, 0)
		if reply == nil {
			return view
		}
		if fits {
			return reply
		}
	}
	// From here nothing taken off comes back: the close lessons go from the end of the list, and when even none of them fits,
	// one more conversation becomes a head (the newest of those that came with their raw text; the first stays whole).
	current := view
	heads := 0
	for {
		low, high := -1, len(current.Lessons) // the most lessons that fit; -1: not even none
		for low < high {
			middle := (low + high + 1) / 2
			if _, fits := try(current, 4, middle, heads); fits {
				low = middle
			} else {
				high = middle - 1
			}
		}
		if low >= 0 {
			reply, _ := try(current, 4, low, heads)
			return reply
		}
		next := -1
		for i := len(current.Sources) - 1; i >= 1; i-- {
			if !current.Sources[i].Head {
				next = i
				break
			}
		}
		if next < 0 {
			break
		}
		sources := append([]reflectSource{}, current.Sources...)
		sources[next] = reflectHead(sources[next])
		current.Sources = sources
		heads++
	}
	// nothing more to take off: the reply is over the bound and says so, rather than leaving out what submit needs
	reply, _ := try(current, 4, 0, heads, "the reply is still over max_bytes: the conversations and their deliveries are not cut")
	if reply == nil {
		return view
	}
	return reply
}

// headsOmitted adds, to what was left out, the conversations whose raw text was cut to a head by the bound.
func headsOmitted(omitted []string, view reflectView, heads int) []string {
	if heads == 0 {
		return omitted
	}
	raw := 0
	for _, s := range view.Sources {
		if !s.Head {
			raw++
		}
	}
	return append(omitted, "the raw text of the last "+strconv.Itoa(heads)+" of the "+strconv.Itoa(raw)+" conversations that come whole (now heads)")
}
