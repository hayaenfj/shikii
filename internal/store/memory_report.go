package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"syscall"
	"time"
)

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
	if d.Tool != "" && (d.Parent == "" || len(d.Tool) > toolMax || len(d.Quotes) != 1) { // a delivery at a tool call is one quote within a turn
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
			if q.Rationale != "" || q.Recheck || q.DiscoveredFor != "" {
				return false
			}
			// a person's past answer: the past request and what the person said, nothing of a lesson or a conversation
			if q.Key != "decision:"+q.ID || q.Request == "" || q.Said == "" || q.Correction != nil || q.Revision != "" || q.Lesson != "" || q.AppliesTo != "" || len(q.Sources) != 0 || q.Summary != "" || q.Reply != "" {
				return false
			}
			continue
		}
		if q.Said != "" {
			return false
		}
		if q.Revision == "" && (q.Rationale != "" || q.Recheck || q.DiscoveredFor != "") {
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

// memoryLineMax is the longest line (with its newline) a delivery may take: the reader skips longer ones and the writer refuses them.
const memoryLineMax = 32 << 10

// Only the last 4MB and the latest 256 turns. Writes before a newline and broken lines are not taken.
func (s Store) MemoryDeliveries(scope string) ([]MemoryDelivery, error) {
	out := []MemoryDelivery{}
	f, start, err := s.openRecentHistory(s.memoryPath(scope))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

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
		if len(line) > memoryLineMax || json.Unmarshal([]byte(line), &d) != nil || !validMemoryDelivery(d) {
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
	// The transient delivery cache is not part of a saved quote or its idempotency comparison.
	d.Quotes = append([]MemoryQuote{}, d.Quotes...)
	for i := range d.Quotes {
		d.Quotes[i].CacheKey = ""
	}
	d.At = time.Now().UTC()
	if !validMemoryDelivery(d) {
		return errors.New("a memory delivery needs a request, quotes, sources and a version")
	}
	// The reader skips a longer line without a word. The phrase, the agent and the parent have no limit of their own, so refuse
	// here what could be written but never read (and would be appended again on every resend).
	if line, err := json.Marshal(d); err != nil {
		return err
	} else if len(line)+1 > memoryLineMax {
		return errors.New("a memory delivery is too long to be read back (its phrase, agent or parent)")
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

func (s Store) MemoryReport(scope string) (MemoryReport, error) {
	return s.MemoryReportWithFeedback(scope, func() ([]Feedback, error) { return s.RecentFeedback(scope) })
}

// MemoryReportWithFeedback reads deliveries before the supplied history, keeping error precedence.
func (s Store) MemoryReportWithFeedback(scope string, read func() ([]Feedback, error)) (MemoryReport, error) {
	r := MemoryReport{MemorySummary: MemorySummary{Window: feedbackRecent}, Recent: []MemoryObservation{}}
	deliveries, err := s.MemoryDeliveries(scope)
	if err != nil {
		return r, err
	}
	feedback, err := read()
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
			if errors.Is(err, errAssessmentBroken) {
				a, err = nil, nil // an assessment whose content cannot be read counts as not yet assessed: one broken file never stops the reflection (confirmation audit 1 of 0.12)
			}
			if err != nil {
				return r, err // a record that cannot be opened, or is a link, is said, not hidden
			}
			item := MemoryObservation{SourceID: d.ID, Parent: d.Parent, Agent: d.Agent, Tool: d.Tool, Phrase: d.Phrase, Request: d.Request, At: d.At, Quote: q, Assessment: a}
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
			if d.Tool != "" {
				r.Stats.AtTool++
				if a != nil && a.Result == "helpful" {
					r.Stats.AtToolHelpful++
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

// PairEffect counts how the reflection assessed one quote (a lesson revision) delivered through one door (a command phrase, a file
// name or "request"): the pair the delivery rule learns from (0.12).
type PairEffect struct {
	Helpful int
	Unused  int
}
