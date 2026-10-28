package store

// Memory deliveries are kept apart from their contribution to decisions. An evaluation is the AI's observation, not a truth or a calibration.
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
	Said       string      `json:"said,omitempty"` // a person's past answer (key decision:<trace>): what the person answered, in the page's words; Request is that request
}

type MemoryDelivery struct {
	ID      string        `json:"id"`
	Scope   string        `json:"project"`
	Request string        `json:"request"`
	Quotes  []MemoryQuote `json:"quotes"`
	At      time.Time     `json:"at"`
	Parent  string        `json:"parent,omitempty"` // delivery to a subagent: the parent turn (the conversation source id). The review is evaluated alongside the parent's conversation (0.7.4)
	Agent   string        `json:"agent,omitempty"`  // the subagent's id
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
	Answers          int `json:"answers"`         // of Delivered, the person's past answers (decision:<trace>, 0.11)
	AnswersHelpful   int `json:"answers_helpful"` // of Answers, those the reflection found helpful
}

type MemorySummary struct {
	Corrections CorrectionStats `json:"corrections"`
	Window      int             `json:"window"` // upper limit on recent delivery turns. Do not pick only successful examples.
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

func (s Store) memoryPath(scope string) string {
	return s.recordPath("memories", ".jsonl", scope)
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
		if strings.HasPrefix(q.Key, "decision:") {
			// a person's past answer: the past request and what the person said, nothing of a lesson or a conversation
			if q.Key != "decision:"+q.ID || q.Request == "" || q.Said == "" || q.Correction != nil || q.Revision != "" || q.Lesson != "" || q.AppliesTo != "" || len(q.Sources) != 0 || q.Summary != "" || q.Reply != "" {
				return false
			}
			continue
		}
		if q.Said != "" {
			return false
		}
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

// Only the last 4MB and the latest 256 turns. Writes before a newline and broken lines are not taken.
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
		// the file belongs to one project by its name; a line may name the path before the folder was moved (0.9.5), so it is not compared
		if len(line) > 32<<10 || json.Unmarshal([]byte(line), &d) != nil || !validMemoryDelivery(d) {
			continue
		}
		out = append(out, d)
		if len(out) > feedbackRecent {
			out = out[1:]
		}
	}
	// a delivery keeps the earlier save. A resend with the same ID does not increase the totals.
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
	// the same lock file an older shikii at this path takes, since the store's name is the one it uses
	return s.lockFile(strings.TrimSuffix(s.memoryPath(scope), ".jsonl")+".lock", true)
}

// lockFile takes an exclusive lock on path, waiting up to 2 seconds. With create, the first concurrent creation is limited to one; an
// existing lock is opened, not created.
func (s Store) lockFile(path string, create bool) (func(), error) {
	f, err := s.openFile(path, os.O_RDWR)
	if create && os.IsNotExist(err) {
		f, err = s.openFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR)
		if os.IsExist(err) {
			f, err = s.openFile(path, os.O_RDWR)
		}
	}
	if err != nil {
		return nil, err
	}
	until := time.Now().Add(2 * time.Second)
	for syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		if time.Now().After(until) {
			f.Close()
			return nil, errors.New("the memory record is busy")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}

func (s Store) SaveMemoryDelivery(d MemoryDelivery) error {
	d.At = time.Now().UTC()
	if !validMemoryDelivery(d) {
		return errors.New("a memory delivery needs a request, quotes, sources and a version")
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
			return errors.New("memories delivered to the same request cannot be changed later")
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
	return filepath.Join(s.recordPath("memory-reviews", "", scope), hex.EncodeToString(h[:])+".json")
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
		return nil, errors.New("cannot confirm the provisional memory evaluation")
	}
	return &a, nil
}

func (s Store) SaveMemoryAssessment(scope string, a MemoryAssessment) error {
	if scope == "" || !validMemoryAssessment(a) {
		return errors.New("a provisional memory evaluation needs grounds for the target, the judgment and the result. Use unknown when unclear")
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
	conversation := a.SourceID // the conversation id. For a delivery to a subagent, the parent turn
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
		return errors.New("name a memory actually delivered to this request")
	}
	if !correction && a.CorrectionResult != "" {
		return errors.New("correction_result is only for a delivered correction; leave it out for this lesson (behaviour observations can only be attached to corrections delivered to this request)")
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
		return errors.New("review only after checking the reply to the request and the person's next message")
	}
	old, err := s.memoryAssessment(scope, a.SourceID, a.Key)
	if err != nil {
		return err
	}
	if old != nil {
		if old.Result == a.Result && old.Decision == a.Decision && old.Evidence == a.Evidence && old.CorrectionResult == a.CorrectionResult {
			return nil
		}
		return errors.New("the original provisional memory evaluation is not overwritten")
	}
	a.At = time.Now().UTC()
	data, err := json.Marshal(a)
	if err != nil {
		return err
	}
	// do not leave an unfinished evaluation under the final name. Publish it once, inside the lock.
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
			if strings.HasPrefix(q.Key, "decision:") {
				r.Stats.Answers++
				if a != nil && a.Result == "helpful" {
					r.Stats.AnswersHelpful++
				}
			}
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

// PersonAnswer is the person's own latest answer to one decision of a question, with the decision's request text.
type PersonAnswer struct {
	TraceID, SpecID, Request, Truth string
	At                              time.Time
}

// PersonAnswers returns, for the questions specIDs, the person's own answers (the page, a spot check, a hand label: personChecks), the
// latest per decision; a truth a result or the machine wrote later does not hide it (0.11). Truths from results, the agent (mcp), re-asking
// (replay), the judge or the added context are never returned: they are not what the person said. A decision from a session that read
// production data (tainted) is left out, since the answer goes into the agent's context, which may be sent out. Request is the decision's
// original text when it was kept (SaveRecordText), else the request input.
func (s Store) PersonAnswers(specIDs []string) ([]PersonAnswer, error) {
	out := []PersonAnswer{}
	for _, id := range specIDs {
		latest := map[string]Label{}
		hashes := map[string][]string{}
		tainted := map[string]bool{}
		order := []string{}
		err := s.scanJSONL(s.labelsPath(id), func(line []byte) error {
			var l Label
			if json.Unmarshal(line, &l) != nil || l.TraceID == "" {
				return nil
			}
			tainted[l.TraceID] = tainted[l.TraceID] || l.Tainted
			if !personChecks[l.Source] || Partial(l.Truth) || strings.TrimSpace(l.Inputs["request"]) == "" {
				return nil
			}
			cur, ok := latest[l.TraceID]
			if !ok {
				order = append(order, l.TraceID)
			}
			if !ok || !l.At.Before(cur.At) {
				latest[l.TraceID] = l
			}
			hashes[l.TraceID] = append(hashes[l.TraceID], l.InputsHash)
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, trace := range order {
			l := latest[trace]
			if tainted[trace] {
				continue
			}
			request := l.Inputs["request"]
			for _, hash := range hashes[trace] { // a decision that added context has a label per step; the kept text is under the record's hash
				originals, _ := s.RecordText(Record{TraceID: trace, SpecID: l.SpecID, InputsHash: hash, Inputs: l.Inputs})
				if text := originals["request"]; text != "" {
					request = text
					break
				}
			}
			out = append(out, PersonAnswer{TraceID: trace, SpecID: l.SpecID, Request: strings.TrimSpace(request), Truth: l.Truth, At: l.At})
		}
	}
	return out, nil
}
