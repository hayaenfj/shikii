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
// a conversation source is returned whole, and its long text (originals, up to 2 MB) that does not fit comes in parts of its JSON (original_part).
const replyBudget = 60 << 10

// ErrStaleCursor: the cursor was made for other search terms, another project, or before what it splits changed. Read again from the start
// (the page does so by itself: serve answers it with 409).
var ErrStaleCursor = errors.New("the wiki cursor does not match the search terms, project or revision; read again from the start")

type continuation struct {
	Version  int    `json:"v"`
	Kind     string `json:"kind"`
	Scope    string `json:"scope"`
	Request  string `json:"request"`
	Snapshot string `json:"snapshot"`
	Offset   int    `json:"offset"`
	Byte     int    `json:"byte,omitempty"` // inside one long source: where its body continues
}

type encodedByteCount int

func (n *encodedByteCount) Write(data []byte) (int, error) {
	*n += encodedByteCount(len(data))
	return len(data), nil
}

// Count the encoder's output without allocating a returned copy of the JSON.
func encodedSize(value any) int {
	var n encodedByteCount
	if json.NewEncoder(&n).Encode(value) != nil {
		return 0
	}
	return int(n) - 1 // Encoder adds one newline; Marshal does not.
}
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
		return 0, ErrStaleCursor
	}
	return offset, err
}

func (want continuation) startAt(token string, total int) (int, int, error) {
	if token == "" {
		return 0, 0, nil
	}
	bad := ErrStaleCursor
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
	// the snapshot covers only what is split into parts: conversations waiting for reflection grow by one each agent turn and must not
	// invalidate the cursor of a page reading on (the first reply carries them alone)
	state := b.continuation("query", query, struct {
		Lessons []Page
		Search  *SearchResult
	}{all.Lessons, all.Search})
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

// Find a fitting UTF-8 prefix before refining its capacity. A host can switch
// presentation, so a small prefix need not fit whenever a larger one does.
// Keep the existing halving path as the lower bound and measure the final page.
func fitSourcePart(text string, fits func(int) bool, hints ...int) error {
	_, width := utf8.DecodeRuneInString(text)
	full := len(text)
	problem := func() error { return errors.New("source metadata exceeds the response budget; use a larger max_bytes") }
	// A complete tail may select a different host presentation and fit on its own.
	if fits(full) {
		return nil
	}
	if full <= width {
		return problem()
	}
	boundary := func(end int) int {
		for end > 0 && !utf8.RuneStart(text[end]) {
			end--
		}
		return max(end, width)
	}
	low, high := 0, full
	if len(hints) > 0 && hints[0] > 0 && hints[0] < full {
		end := boundary(hints[0])
		if fits(end) {
			low = end
			for {
				high = min(full, 2*low)
				if high == full {
					break
				}
				high = boundary(high)
				if !fits(high) {
					break
				}
				low = high
			}
		} else {
			high = end
			for end > width {
				end = boundary(end / 2)
				if fits(end) {
					low = end
					break
				}
				high = end
			}
		}
	}
	// A small hint can miss the host's presentation window. Recover the original
	// halving path rather than treating the failed hint as unusable metadata.
	if low == 0 {
		high = full
		end := boundary(full / 2)
		for !fits(end) {
			if end <= width {
				return problem()
			}
			high = end
			end = boundary(end / 2)
		}
		low = end
	}
	high--
	for low < high {
		end := low + (high-low+1)/2
		for end > low && !utf8.RuneStart(text[end]) {
			end--
		}
		if end == low {
			break
		}
		if fits(end) {
			low = end
		} else {
			high = end - 1
		}
	}
	if !fits(low) {
		return problem()
	}
	return nil
}

// originalPart returns the part of one conversation's originals that fits from byte from: original_part carries a piece of the JSON
// of that conversation's originals list (json) and says which piece it is. Joined in order, the pieces are that JSON whole.
func originalPart(out map[string]any, state continuation, fs []store.Feedback, osources []Observation, texts map[string][]Original, i, total int, id string, raw []byte, from, budget int, size func(any) int) (map[string]any, error) {
	out["source_material"], out["observations"], out["originals"] = fs, osources, texts
	err := fitSourcePart(string(raw[from:]), func(n int) bool {
		end := from + n
		out["original_part"] = map[string]any{"id": id, "from": from, "to": end, "bytes": len(raw), "json": string(raw[from:end])}
		delete(out, "next_cursor")
		if end < len(raw) {
			c := state
			c.Offset, c.Byte = i, end
			data, _ := json.Marshal(c)
			out["next_cursor"] = base64.RawURLEncoding.EncodeToString(data)
		} else if next := state.next(i+1, total); next != "" {
			out["next_cursor"] = next
		}
		return size(out) <= budget
	}, budget)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (b Book) ReadSourcesPage(ids []string, cursor string, limits ...int) (map[string]any, error) {
	budget := replyBudget
	if len(limits) > 1 || len(limits) == 1 && limits[0] < 4096 {
		return nil, errors.New("source response budget must be at least 4096 bytes")
	}
	if len(limits) == 1 {
		budget = min(budget, limits[0])
	}

	return b.ReadSourcesPageMeasured(ids, cursor, budget, encodedSize)
}

// ReadSourcesPageMeasured sizes a source page in its actual host envelope.
// The host supplies the measurement; source content cannot alter the budget.
func (b Book) ReadSourcesPageMeasured(ids []string, cursor string, budget int, size func(any) int) (map[string]any, error) {
	if budget < 4096 || size == nil {
		return nil, errors.New("invalid measured source response budget")
	}
	budget = min(budget, replyBudget*3)
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
		o, isObservation := observations[id]
		text, hasText := originals[id]
		if i == start && from > 0 && !isObservation {
			// a cursor inside the long originals of a conversation (the conversation itself came with the first part)
			raw, _ := json.Marshal(text)
			if !hasText || from >= len(raw) || !utf8.RuneStart(raw[from]) {
				return nil, ErrStaleCursor
			}
			return originalPart(out, state, fs, osources, texts, i, len(ids), id, raw, from, budget, size)
		}
		if f, ok := feedback[id]; ok {
			fs = append(fs, f)
		}
		if i == start && from > 0 {
			// a cursor inside a long source: it must point at a character boundary inside an observation's body
			if !isObservation || from >= len(o.Content) || !utf8.RuneStart(o.Content[from]) {
				return nil, ErrStaleCursor
			}
			o.Content = o.Content[from:]
			// the last part is a tail too: say which part it is so that it is not taken for the whole body
			out["observation_part"] = map[string]any{"id": id, "from": from, "to": from + len(o.Content), "bytes": from + len(o.Content)}
		}
		if isObservation {
			osources = append(osources, o)
		}
		if hasText {
			texts[id] = text
		}
		out["source_material"], out["observations"], out["originals"] = fs, osources, texts
		// Measure the continuation with the candidate, including when a whole
		// source fits but the next source needs another page.
		delete(out, "next_cursor")
		if next := state.next(i+1, len(ids)); next != "" {
			out["next_cursor"] = next
		}
		if size(out) <= budget {
			continue
		}
		if i > start {
			fs, osources = fs[:nf], osources[:no]
			delete(texts, id)
			break
		}
		if isObservation && i == start { // one long source over the budget: return part of its body and continue inside it
			body := osources[no].Content
			err := fitSourcePart(body, func(end int) bool {
				osources[no].Content = body[:end]
				out["observation_part"] = map[string]any{"id": id, "from": from, "to": from + end, "bytes": from + len(body)}
				delete(out, "next_cursor")
				if end < len(body) {
					c := state
					c.Offset, c.Byte = i, from+end
					data, _ := json.Marshal(c)
					out["next_cursor"] = base64.RawURLEncoding.EncodeToString(data)
				} else if next := state.next(i+1, len(ids)); next != "" {
					out["next_cursor"] = next
				}
				return size(out) <= budget
			}, budget)
			if err != nil {
				return nil, err
			}
			return out, nil
		}
		if hasText && !isObservation && i == start { // a conversation whose originals (up to 2 MB) do not fit: the conversation now, its originals in parts
			delete(texts, id)
			raw, _ := json.Marshal(text)
			return originalPart(out, state, fs, osources, texts, i, len(ids), id, raw, 0, budget, size)
		}
	}
	out["source_material"], out["observations"], out["originals"] = fs, osources, texts
	delete(out, "next_cursor")
	if next := state.next(i, len(ids)); next != "" {
		out["next_cursor"] = next
	}
	if size(out) > budget {
		return nil, errors.New("source metadata exceeds the response budget; use a larger max_bytes")
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
