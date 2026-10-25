package notebook

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/store"
)

// Split by the size of the JSON rather than the count. An observation whose body does not fit in one reply is returned in parts (observation_part);
// a conversation source and its long text are returned whole.
const replyBudget = 60 << 10

type continuation struct {
	Version  int    `json:"v"`
	Kind     string `json:"kind"`
	Scope    string `json:"scope"`
	Request  string `json:"request"`
	Snapshot string `json:"snapshot"`
	Offset   int    `json:"offset"`
	Byte     int    `json:"byte,omitempty"` // inside one long source: where its body continues
}

func encodedSize(value any) int    { data, _ := json.Marshal(value); return len(data) }
func fingerprint(value any) string { data, _ := json.Marshal(value); return digest(data) }

func (b Book) continuation(kind string, request, snapshot any) continuation {
	root, _ := filepath.Abs(b.Root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return continuation{Version: 1, Kind: kind, Scope: digest([]byte(root + "\n" + b.Scope)), Request: fingerprint(request), Snapshot: fingerprint(snapshot)}
}

func (want continuation) start(token string, total int) (int, error) {
	offset, from, err := want.startAt(token, total)
	if err == nil && from != 0 {
		return 0, errors.New("the wiki cursor does not match the search terms, project or revision; read again from the start")
	}
	return offset, err
}

func (want continuation) startAt(token string, total int) (int, int, error) {
	if token == "" {
		return 0, 0, nil
	}
	bad := errors.New("the wiki cursor does not match the search terms, project or revision; read again from the start")
	if len(token) > 2048 {
		return 0, 0, bad
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, 0, bad
	}
	var got continuation
	if json.Unmarshal(data, &got) != nil || got.Offset < 0 || got.Byte < 0 || (got.Offset == 0 && got.Byte == 0) || got.Offset >= total {
		return 0, 0, bad
	}
	offset, from := got.Offset, got.Byte
	got.Offset, got.Byte = 0, 0
	if got != want {
		return 0, 0, bad
	}
	return offset, from, nil
}

func (c continuation) next(offset, total int) string {
	if offset >= total {
		return ""
	}
	c.Offset = offset
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

func (b Book) QueryPage(query, cursor string) (View, error) {
	all, err := b.Query(query)
	if err != nil {
		return all, err
	}
	state := b.continuation("query", query, all)
	total := len(all.Lessons)
	if all.Search != nil {
		total = len(all.Search.Candidates)
	}
	start, err := state.start(cursor, total)
	if err != nil {
		return View{}, err
	}
	out := all
	out.Lessons = []Page{}
	out.Matches = nil
	if start > 0 {
		out.Pending = nil
		out.Issues = nil
		out.Observations = nil
		out.Maintenance = nil
	}
	out.Remaining = Remaining{}
	if start == 0 { // pages without kind and scene (0.9.0); not included in continuation replies
		out.Remaining = b.RemainingUnclassified()
	}
	if all.Search != nil {
		out.Search = &SearchResult{Total: all.Search.Total, Candidates: []SearchCandidate{}}
	}
	i := start
	for ; i < total; i++ {
		if all.Search != nil {
			candidate := all.Search.Candidates[i]
			out.Search.Candidates = append(out.Search.Candidates, candidate)
			if i > start && encodedSize(out) > replyBudget {
				out.Search.Candidates = out.Search.Candidates[:len(out.Search.Candidates)-1]
				break
			}
			for _, p := range all.Lessons {
				if p.ID == candidate.ID {
					out.Lessons = append(out.Lessons, p)
					if encodedSize(out) > replyBudget {
						out.Lessons = out.Lessons[:len(out.Lessons)-1]
					} else {
						out.Matches = append(out.Matches, candidate.Match)
					}
				}
			}
		} else {
			out.Lessons = append(out.Lessons, all.Lessons[i])
			if i > start && encodedSize(out) > replyBudget {
				out.Lessons = out.Lessons[:len(out.Lessons)-1]
				break
			}
		}
	}
	out.NextCursor = state.next(i, total)
	return out, nil
}

func (b Book) ReadSourcesPage(ids []string, cursor string) (map[string]any, error) {
	all, err := b.ReadSources(ids)
	if err != nil {
		return nil, err
	}
	ids = unique(ids)
	state := b.continuation("sources", ids, all)
	start, from, err := state.startAt(cursor, len(ids))
	if err != nil {
		return nil, err
	}
	feedback := map[string]store.Feedback{}
	observations := map[string]Observation{}
	for _, f := range all["source_material"].([]store.Feedback) {
		feedback[f.ID] = f
	}
	for _, o := range all["observations"].([]Observation) {
		observations[o.ID] = o
	}
	originals := all["originals"].(map[string][]Original)
	fs, osources, texts := []store.Feedback{}, []Observation{}, map[string][]Original{}
	out := map[string]any{}
	i := start
	for ; i < len(ids); i++ {
		id := ids[i]
		nf, no := len(fs), len(osources)
		if f, ok := feedback[id]; ok {
			fs = append(fs, f)
		}
		o, isObservation := observations[id]
		if i == start && from > 0 {
			// a cursor inside a long source: it must point at a character boundary inside an observation's body
			if !isObservation || from >= len(o.Content) || !utf8.RuneStart(o.Content[from]) {
				return nil, errors.New("the wiki cursor does not match the search terms, project or revision; read again from the start")
			}
			o.Content = o.Content[from:]
			// the last part is a tail too: say which part it is so that it is not taken for the whole body
			out["observation_part"] = map[string]any{"id": id, "from": from, "to": from + len(o.Content), "bytes": from + len(o.Content)}
		}
		if isObservation {
			osources = append(osources, o)
		}
		if text, ok := originals[id]; ok {
			texts[id] = text
		}
		out["source_material"], out["observations"], out["originals"] = fs, osources, texts
		if encodedSize(out) <= replyBudget {
			continue
		}
		if i > start {
			fs, osources = fs[:nf], osources[:no]
			delete(texts, id)
			break
		}
		if isObservation && i == start { // one long source over the budget: return part of its body and continue inside it
			body := osources[no].Content
			end := len(body)
			for encodedSize(out) > replyBudget-2048 && end > 1 {
				end /= 2
				for end > 0 && !utf8.RuneStart(body[end]) {
					end--
				}
				osources[no].Content = body[:end]
			}
			if end < len(body) {
				out["observation_part"] = map[string]any{"id": id, "from": from, "to": from + end, "bytes": from + len(body)}
				out["source_material"], out["observations"], out["originals"] = fs, osources, texts
				c := state
				c.Offset, c.Byte = i, from+end
				data, _ := json.Marshal(c)
				out["next_cursor"] = base64.RawURLEncoding.EncodeToString(data)
				return out, nil
			}
		}
	}
	out["source_material"], out["observations"], out["originals"] = fs, osources, texts
	if next := state.next(i, len(ids)); next != "" {
		out["next_cursor"] = next
	}
	return out, nil
}

// Index and review log that grow with the count. Kept separate from the body limits of each page and source record.
func readRegistry(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("the wiki index must be a regular file")
	}
	return io.ReadAll(f)
}
