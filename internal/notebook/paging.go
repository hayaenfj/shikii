package notebook

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/hayaenfj/shikii/internal/store"
)

// 件数ではなくJSONの量を目安に分割する。単一の原資料は切らずに返す。
const replyBudget = 60 << 10

type continuation struct {
	Version  int    `json:"v"`
	Kind     string `json:"kind"`
	Scope    string `json:"scope"`
	Request  string `json:"request"`
	Snapshot string `json:"snapshot"`
	Offset   int    `json:"offset"`
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
	if token == "" {
		return 0, nil
	}
	bad := errors.New("wikiの続きが検索条件・プロジェクト・版と一致しません。先頭から読み直してください")
	if len(token) > 2048 {
		return 0, bad
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, bad
	}
	var got continuation
	if json.Unmarshal(data, &got) != nil || got.Offset <= 0 || got.Offset >= total {
		return 0, bad
	}
	offset := got.Offset
	got.Offset = 0
	if got != want {
		return 0, bad
	}
	return offset, nil
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
	if start == 0 { // 種類と場面の無い記事(0.9.0)。続きの返事には載せない
		if un, err := b.Unclassified(); err == nil && len(un) > 0 {
			out.UnclassifiedCount, out.UnclassifiedHow = len(un), unclassifiedHow
			for i, p := range un {
				if i == unclassifiedBatch {
					break
				}
				out.Unclassified = append(out.Unclassified, UnclassifiedPage{ID: p.ID, Title: p.Title, Lesson: p.Lesson, AppliesTo: p.AppliesTo})
			}
		}
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
	start, err := state.start(cursor, len(ids))
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
		if o, ok := observations[id]; ok {
			osources = append(osources, o)
		}
		if text, ok := originals[id]; ok {
			texts[id] = text
		}
		out["source_material"], out["observations"], out["originals"] = fs, osources, texts
		if i > start && encodedSize(out) > replyBudget {
			fs, osources = fs[:nf], osources[:no]
			delete(texts, id)
			break
		}
	}
	out["source_material"], out["observations"], out["originals"] = fs, osources, texts
	if next := state.next(i, len(ids)); next != "" {
		out["next_cursor"] = next
	}
	return out, nil
}

// 件数とともに育つ索引・見直し記録。個々の記事と原資料の本文上限とは分ける。
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
		return nil, errors.New("wikiの索引は通常ファイルである必要があります")
	}
	return io.ReadAll(f)
}
