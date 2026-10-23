package store

// 記憶の配送は判断への寄与と分けて残す。評価はAIの観察であり、正解や較正ではない。
import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"
)

type MemoryQuote struct {
	Correction *Correction `json:"correction,omitempty"`
	Key        string      `json:"key"`
	ID         string      `json:"id"`
	Revision   string      `json:"revision,omitempty"`
	Lesson     string      `json:"lesson,omitempty"`
	AppliesTo  string      `json:"applies_to,omitempty"`
	Sources    []string    `json:"sources,omitempty"`
	Request    string      `json:"request,omitempty"`
	Summary    string      `json:"summary,omitempty"`
	Reply      string      `json:"reply,omitempty"`
}

type MemoryDelivery struct {
	ID      string        `json:"id"`
	Scope   string        `json:"project"`
	Request string        `json:"request"`
	Quotes  []MemoryQuote `json:"quotes"`
	At      time.Time     `json:"at"`
	Parent  string        `json:"parent,omitempty"` // サブエージェントへの配送: 親のターン(会話の出典 id)。振り返りは親の会話に添えて評価する(0.7.4)
	Agent   string        `json:"agent,omitempty"`  // サブエージェントの id
}

type MemoryAssessment struct {
	CorrectionResult string    `json:"correction_result,omitempty"`
	SourceID         string    `json:"source_id"`
	Key              string    `json:"key"`
	Result           string    `json:"result"`
	Decision         string    `json:"decision"`
	Evidence         string    `json:"evidence"`
	At               time.Time `json:"at"`
}

type MemoryStats struct {
	Delivered        int `json:"delivered"`
	Helpful          int `json:"helpful"`
	Unused           int `json:"unused"`
	Harmful          int `json:"harmful"`
	Unknown          int `json:"unknown"`
	Pending          int `json:"pending"`
	AwaitingFeedback int `json:"awaiting_feedback"`
}

type MemorySummary struct {
	Corrections CorrectionStats `json:"corrections"`
	Window      int             `json:"window"` // 直近の配送ターン数の上限。成功例だけを選ばない。
	Stats       MemoryStats     `json:"stats"`
}

type MemoryObservation struct {
	SourceID   string            `json:"source_id"`
	Parent     string            `json:"parent,omitempty"`
	Agent      string            `json:"agent,omitempty"`
	Request    string            `json:"request"`
	At         time.Time         `json:"at"`
	Quote      MemoryQuote       `json:"quote"`
	Assessment *MemoryAssessment `json:"assessment,omitempty"`
}

type MemoryReport struct {
	MemorySummary
	Recent []MemoryObservation `json:"recent"`
	All    []MemoryObservation `json:"-"`
}

func memoryScopeKey(scope string) string {
	h := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(h[:])
}

func (s Store) memoryPath(scope string) string {
	return filepath.Join(s.Dir, "records", "memories", memoryScopeKey(scope)+".jsonl")
}

func validMemoryDelivery(d MemoryDelivery) bool {
	if !textTrace.MatchString(d.ID) || d.Scope == "" || d.Request == "" || len(d.Request) > feedbackTextMax || len(d.Quotes) == 0 || len(d.Quotes) > 2 || d.At.IsZero() {
		return false
	}
	data, _ := json.Marshal(d.Quotes)
	if len(data) > 1800 {
		return false
	}
	seen := map[string]bool{}
	for _, q := range d.Quotes {
		if !textTrace.MatchString(q.ID) || seen[q.Key] {
			return false
		}
		seen[q.Key] = true
		if q.Correction != nil {
			if !q.Correction.Valid() || q.Revision == "" {
				return false
			}
			cited := false
			for _, id := range q.Sources {
				cited = cited || id == q.Correction.SourceID
			}
			if !cited {
				return false
			}
		}
		if q.Revision != "" {
			if !textTrace.MatchString(q.Revision) || q.Key != "lesson:"+q.ID+":"+q.Revision || q.Lesson == "" || q.AppliesTo == "" || len(q.Sources) == 0 || len(q.Sources) > 16 {
				return false
			}
			for _, id := range q.Sources {
				if !textTrace.MatchString(id) {
					return false
				}
			}
		} else if q.Key != "feedback:"+q.ID || q.Request == "" || q.Summary == "" || q.Reply == "" {
			return false
		}
	}
	return true
}

// 末尾4MB・直近256ターンだけ。改行前の書き込みや壊れた行は採らない。
func (s Store) MemoryDeliveries(scope string) ([]MemoryDelivery, error) {
	out := []MemoryDelivery{}
	f, err := s.openFile(s.memoryPath(scope), os.O_RDONLY)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := max(int64(0), info.Size()-feedbackReadMax)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	r := bufio.NewReader(io.LimitReader(f, feedbackReadMax))
	if start > 0 {
		if _, err := r.ReadString('\n'); err != nil {
			return out, nil
		}
	}
	for {
		line, err := r.ReadString('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		var d MemoryDelivery
		if len(line) > 32<<10 || json.Unmarshal([]byte(line), &d) != nil || d.Scope != scope || !validMemoryDelivery(d) {
			continue
		}
		out = append(out, d)
		if len(out) > feedbackRecent {
			out = out[1:]
		}
	}
	// 配送は先の保存を採る。同じIDの再送で集計を増やさない。
	seen := map[string]bool{}
	unique := []MemoryDelivery{}
	for _, d := range out {
		if !seen[d.ID] {
			seen[d.ID] = true
			unique = append(unique, d)
		}
	}
	return unique, nil
}

func (s Store) memoryLock(scope string) (func(), error) {
	path := strings.TrimSuffix(s.memoryPath(scope), ".jsonl") + ".lock"
	// 最初の同時作成も一人に限る。既存の錠は作成せず開く。
	f, err := s.openFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR)
	if os.IsExist(err) {
		f, err = s.openFile(path, os.O_RDWR)
	}
	if err != nil {
		return nil, err
	}
	until := time.Now().Add(2 * time.Second)
	for syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		if time.Now().After(until) {
			f.Close()
			return nil, errors.New("記憶の記録が混み合っています")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

func (s Store) SaveMemoryDelivery(d MemoryDelivery) error {
	d.At = time.Now().UTC()
	if !validMemoryDelivery(d) {
		return errors.New("記憶の配送には依頼・引用・出典・版が必要です")
	}
	unlock, err := s.memoryLock(d.Scope)
	if err != nil {
		return err
	}
	defer unlock()
	all, err := s.MemoryDeliveries(d.Scope)
	if err != nil {
		return err
	}
	for _, old := range all {
		if old.ID == d.ID {
			if old.Request == d.Request && reflect.DeepEqual(old.Quotes, d.Quotes) {
				return nil
			}
			return errors.New("同じ依頼へ届けた記憶を後から変更できません")
		}
	}
	return s.appendJSONL(s.memoryPath(d.Scope), d)
}

func validMemoryAssessment(a MemoryAssessment) bool {
	if !validCorrectionResult(a.CorrectionResult) || (a.CorrectionResult != "" && a.CorrectionResult != "unknown" && strings.TrimSpace(a.Decision) == "") {
		return false
	}
	if !textTrace.MatchString(a.SourceID) || a.Key == "" || len(a.Key) > 80 || strings.TrimSpace(a.Evidence) == "" || len(a.Decision) > feedbackTextMax || len(a.Evidence) > feedbackTextMax {
		return false
	}
	switch a.Result {
	case "helpful", "harmful", "unused":
		return strings.TrimSpace(a.Decision) != ""
	case "unknown":
		return true
	}
	return false
}

func (s Store) memoryAssessmentPath(scope, id, key string) string {
	h := sha256.Sum256([]byte(id + ":" + key))
	return filepath.Join(s.Dir, "records", "memory-reviews", memoryScopeKey(scope), hex.EncodeToString(h[:])+".json")
}

func (s Store) memoryAssessment(scope, id, key string) (*MemoryAssessment, error) {
	f, err := s.openFile(s.memoryAssessmentPath(scope, id, key), os.O_RDONLY)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (32<<10)+1))
	var a MemoryAssessment
	if err != nil || len(data) > 32<<10 || json.Unmarshal(data, &a) != nil || a.SourceID != id || a.Key != key || a.At.IsZero() || !validMemoryAssessment(a) {
		return nil, errors.New("記憶の仮評価を確認できません")
	}
	return &a, nil
}

func (s Store) SaveMemoryAssessment(scope string, a MemoryAssessment) error {
	if scope == "" || !validMemoryAssessment(a) {
		return errors.New("記憶の仮評価には対象・判断・結果の根拠が必要です。不明はunknownにします")
	}
	unlock, err := s.memoryLock(scope)
	if err != nil {
		return err
	}
	defer unlock()
	deliveries, err := s.MemoryDeliveries(scope)
	if err != nil {
		return err
	}
	delivered, correction := false, false
	conversation := a.SourceID // 会話の id。サブエージェントへの配送なら親のターン
	for _, d := range deliveries {
		for _, q := range d.Quotes {
			if d.ID == a.SourceID && q.Key == a.Key {
				delivered = true
				correction = q.Correction != nil
				if d.Parent != "" {
					conversation = d.Parent
				}
			}
		}
	}
	if !delivered {
		return errors.New("この依頼へ実際に届けた記憶を指定してください")
	}
	if !correction && a.CorrectionResult != "" {
		return errors.New("この依頼へ届いた訂正にだけ行動の観察を付けられます")
	}
	if correction && a.CorrectionResult == "" {
		a.CorrectionResult = "unknown"
	}
	feedback, err := s.RecentFeedback(scope)
	if err != nil {
		return err
	}
	observed := false
	for _, f := range feedback {
		observed = observed || f.ID == conversation
	}
	if !observed {
		return errors.New("依頼の返答と次の人の発言を確認してから振り返ります")
	}
	old, err := s.memoryAssessment(scope, a.SourceID, a.Key)
	if err != nil {
		return err
	}
	if old != nil {
		if old.Result == a.Result && old.Decision == a.Decision && old.Evidence == a.Evidence && old.CorrectionResult == a.CorrectionResult {
			return nil
		}
		return errors.New("記憶の元の仮評価は塗り替えません")
	}
	a.At = time.Now().UTC()
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	// 未完成の評価を最終名で残さない。錠の中で一度だけ公開する。
	root, name, err := s.parentRoot(s.memoryAssessmentPath(scope, a.SourceID, a.Key), true)
	if err != nil {
		return err
	}
	defer root.Close()
	tmp := "." + rand.Text() + ".tmp"
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return root.Link(tmp, name)
}

func (s Store) MemoryReport(scope string) (MemoryReport, error) {
	r := MemoryReport{MemorySummary: MemorySummary{Window: feedbackRecent}, Recent: []MemoryObservation{}}
	deliveries, err := s.MemoryDeliveries(scope)
	if err != nil {
		return r, err
	}
	feedback, err := s.RecentFeedback(scope)
	if err != nil {
		return r, err
	}
	observed := map[string]bool{}
	for _, f := range feedback {
		observed[f.ID] = true
	}
	for i := len(deliveries) - 1; i >= 0; i-- {
		d := deliveries[i]
		for _, q := range d.Quotes {
			a, err := s.memoryAssessment(scope, d.ID, q.Key)
			if err != nil {
				return r, err
			}
			item := MemoryObservation{SourceID: d.ID, Parent: d.Parent, Agent: d.Agent, Request: d.Request, At: d.At, Quote: q, Assessment: a}
			if q.Correction != nil {
				r.Corrections.Delivered++
				if a == nil {
					r.Corrections.Pending++
				} else {
					switch a.CorrectionResult {
					case "followed":
						r.Corrections.Followed++
					case "repeated":
						r.Corrections.Repeated++
					case "not_applicable":
						r.Corrections.NotApplicable++
					default:
						r.Corrections.Unknown++
					}
				}
			}
			r.All = append(r.All, item)
			r.Stats.Delivered++
			if a == nil {
				r.Stats.Pending++
				if conversation := d.ID; !observed[conversation] && !(d.Parent != "" && observed[d.Parent]) {
					r.Stats.AwaitingFeedback++
				}
				continue
			}
			switch a.Result {
			case "helpful":
				r.Stats.Helpful++
			case "unused":
				r.Stats.Unused++
			case "harmful":
				r.Stats.Harmful++
			case "unknown":
				r.Stats.Unknown++
			}
			if len(r.Recent) < 6 {
				r.Recent = append(r.Recent, item)
			}
		}
	}
	return r, nil
}
