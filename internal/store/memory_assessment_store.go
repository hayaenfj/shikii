package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/rootfile"
)

// assessmentFileName is the file of the assessment of one quote of one delivery, inside the project's memory-reviews folder.
func assessmentFileName(id, key string) string {
	h := sha256.Sum256([]byte(id + ":" + key))
	return hex.EncodeToString(h[:]) + ".json"
}

func (s Store) memoryAssessmentPath(scope, id, key string) string {
	return filepath.Join(s.recordPath("memory-reviews", "", scope), assessmentFileName(id, key))
}

// errAssessmentBroken is an assessment file whose content cannot be read as one (the report counts it as not yet assessed); a file that
// cannot be opened, or is a link, is a different error that is never hidden.
var errAssessmentBroken = errors.New("cannot confirm the provisional memory evaluation")

func (s Store) memoryAssessment(scope, id, key string) (*MemoryAssessment, error) {
	f, err := s.openFile(s.memoryAssessmentPath(scope, id, key), os.O_RDONLY)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readAssessment(f, id, key)
}

// readAssessment reads the assessment of the quote key of the delivery id from f (and closes it): errAssessmentBroken when the
// content is not one.
func readAssessment(f *os.File, id, key string) (*MemoryAssessment, error) {
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (32<<10)+1))
	var a MemoryAssessment
	if err != nil || len(data) > 32<<10 || json.Unmarshal(data, &a) != nil || a.SourceID != id || a.Key != key || a.At.IsZero() || !validMemoryAssessment(a) {
		return nil, errAssessmentBroken
	}
	return &a, nil
}

// assessmentReader reads the assessments of many quotes of one project for a report. memoryAssessment reads the store name and
// opens the project's folders from the top for every quote, and almost every answer is "not there". The reader reads the store name
// and opens records/memory-reviews/<store> once (through parentRoot and without following a link, as MemoryAssessments does), lists
// its names once, and opens only the quotes whose file is listed, with the same checks (openIn) and the same reading (readAssessment)
// as memoryAssessment. A quote whose file is not listed is not assessed, which is what memoryAssessment answers for a file that is
// not there; an assessment saved after the listing is seen by the next report, as one saved just after a quote was asked for is today.
// Nothing is touched until the first quote (as the first memoryAssessment is the first to read the store name), and nothing takes
// the memory lock.
//
// The list answers only for names exactly the form shikii writes (assessmentName: 64 lower-case hexadecimal digits and ".json"),
// compared as they are. A disk that ignores case, or reads another spelling as the same letter (APFS reads the long s as s), finds a
// file by a name that is not the listed one, and no folding of the list tells which names a given disk reads as one. So a folder
// that holds a name of any other form is not listed at all: every quote of that report is asked for by memoryAssessment, which asks
// the disk for the exact name, as it did before the list.
//
// The folder is opened once, so verify checks at the end of the report that its path still names it: asking for each quote would
// have found a link put in its place (parentRoot says so) at the next quote.
type assessmentReader struct {
	s      Store
	scope  string
	entry  string              // a path inside the folder, as parentRoot takes it
	asked  bool                // list has run
	absent bool                // the folder is not there: no quote is assessed
	root   *os.Root            // the folder, opened once; nil when it could not be listed or holds a name of another form
	listed map[string]struct{} // the names in the folder, all of the form shikii writes
}

func (s Store) newAssessmentReader(scope string) *assessmentReader {
	return &assessmentReader{s: s, scope: scope}
}

// assessmentName reports whether name is exactly a name assessmentFileName makes.
func assessmentName(name string) bool {
	hash, ok := strings.CutSuffix(name, ".json")
	if !ok || len(hash) != 2*sha256.Size {
		return false
	}
	for i := 0; i < len(hash); i++ {
		if c := hash[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// list opens and lists the folder. Whatever goes wrong other than "the folder is not there" (a link in the way, a folder that cannot
// be opened or listed), and a name of another form in the folder, the reader stays without a folder and every quote is asked for by
// memoryAssessment, which says it as before.
func (r *assessmentReader) list() {
	r.asked = true
	r.entry = filepath.Join(r.s.recordPath("memory-reviews", "", r.scope), "entry")
	root, _, err := r.s.parentRoot(r.entry, false)
	if os.IsNotExist(err) {
		r.absent = true
		return
	}
	if err != nil {
		return
	}
	folder, err := rootfile.OpenNoFollow(root, ".", os.O_RDONLY, 0)
	if err != nil {
		root.Close()
		return
	}
	names, err := folder.Readdirnames(-1)
	folder.Close()
	if err != nil {
		root.Close()
		return
	}
	listed := make(map[string]struct{}, len(names))
	for _, name := range names {
		if !assessmentName(name) {
			root.Close() // a name a disk may read as another: memoryAssessment asks the disk, for each quote
			return
		}
		listed[name] = struct{}{}
	}
	r.root, r.listed = root, listed
}

func (r *assessmentReader) get(id, key string) (*MemoryAssessment, error) {
	if !r.asked {
		r.list()
	}
	if r.absent {
		return nil, nil
	}
	if r.root == nil {
		return r.s.memoryAssessment(r.scope, id, key)
	}
	name := assessmentFileName(id, key)
	if _, ok := r.listed[name]; !ok {
		return nil, nil
	}
	f, err := openIn(r.root, name, os.O_RDONLY)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readAssessment(f, id, key)
}

// verify says, at the end of a report, whether the path still names the folder that was opened: the same checks parentRoot makes for
// every quote (a link in the way is said as "links cannot be used in the record folder"), then whether what it opens is the folder this
// reader read. A folder that is gone says nothing, as asking for each quote answers "not there" for a folder that is not.
func (r *assessmentReader) verify() error {
	if r.root == nil {
		return nil // no folder was opened: every quote was asked for by memoryAssessment, which checks the path each time
	}
	now, _, err := r.s.parentRoot(r.entry, false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer now.Close()
	was, err := r.root.Stat(".")
	if err != nil {
		return err
	}
	is, err := now.Stat(".")
	if err != nil {
		return err
	}
	if !os.SameFile(was, is) {
		return errors.New("the record folder changed")
	}
	return nil
}

func (r *assessmentReader) close() {
	if r.root != nil {
		r.root.Close()
	}
}

// followedUnshown is the evidence of the entry the skill describes for work that went the way a lesson says with nothing to show
// that the lesson changed it: result unknown, correction_result followed and the decision. There is no evidence to ask the agent
// for, and a real Codex (0.169.0) that sent exactly that entry was refused twice.
const followedUnshown = "The agent reports that the work went the way the lesson says; no check shows the lesson changed it."

func (s Store) SaveMemoryAssessment(scope string, a MemoryAssessment) error {
	// What an entry with followed needs depends on the lesson it names, which is known once the delivery is found below: on a
	// lesson that is not a correction it goes only with unknown, and then needs no evidence. So such an entry is not refused
	// here for a decision or evidence it lacks; below, one order decides: the delivery, whether followed fits the lesson, then
	// what the entry lacks. A refusal asking for a field is never followed by one that says to take followed out.
	sent := a
	if a.CorrectionResult == "followed" {
		if strings.TrimSpace(a.Evidence) == "" {
			a.Evidence = followedUnshown
		}
		if strings.TrimSpace(a.Decision) == "" {
			a.Decision = followedUnshown // only for the check of the entry's form; an entry without a decision is refused below
		}
	}
	if scope == "" || !validMemoryAssessment(a) {
		return errors.New(memoryAssessmentRefusal(a))
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
	deliveredTo := ""          // the subagent the memory was delivered to, empty for the conversation's own agent
	conversation := a.SourceID // the conversation id. For a delivery to a subagent, the parent turn
	for _, d := range deliveries {
		for _, q := range d.Quotes {
			if d.ID == a.SourceID && q.Key == a.Key {
				delivered = true
				correction = q.Correction != nil
				deliveredTo = d.Agent
				a.Phrase = d.Phrase // the door, kept with the assessment (0.12)
				if d.Parent != "" {
					conversation = d.Parent
				}
			}
		}
	}
	if !delivered {
		return errors.New("name a memory actually delivered to this request")
	}
	if a.Agent != "" && a.Agent != deliveredTo {
		return errors.New("a subagent assesses only a memory delivered to itself")
	}
	if !correction && a.CorrectionResult != "" {
		// A lesson that is not a correction takes one behaviour observation: followed, the work went the way the lesson says. With
		// the result unknown it records that the lesson applied to the moment although its effect is not shown, which is what the
		// delivery's question (lesson-applies) learns from. The others describe what happened to a correction, and a helpful lesson
		// that is not a correction is not scored as a correction taking effect, as before.
		if a.CorrectionResult != "followed" {
			return errors.New("correction_result is only for a delivered correction; leave it out for this lesson, or send followed when the work went the way the lesson says (the other behaviour observations can only be attached to corrections delivered to this request)")
		}
		if a.Result != "unknown" {
			return errors.New("correction_result followed is only for result unknown (the work went the way the lesson says, but the lesson's effect is not shown): keep result " + a.Result + ", leave correction_result out and send the entry again")
		}
	}
	if sent.CorrectionResult == "followed" {
		need := sent
		if !correction {
			need.Evidence = a.Evidence // unknown and followed on a lesson that is not a correction: there is no evidence to ask for
		}
		// a correction said to be followed counts as the correction taking effect: it keeps needing its check result
		if strings.TrimSpace(need.Decision) == "" || strings.TrimSpace(need.Evidence) == "" {
			return errors.New(memoryAssessmentRefusal(need))
		}
		a.Decision = sent.Decision
	}
	if correction && a.CorrectionResult == "" {
		a.CorrectionResult = "unknown"
	}
	if a.InRequest && (a.Agent != "" || deliveredTo != "") {
		return errors.New("a lesson delivered to a subagent is assessed by that subagent, in its final report")
	}
	// A subagent reports what it did with a lesson when it ends, inside the parent's turn: it does not wait for the person's next
	// message, which is about the parent's reply, not about the subagent's work. The agent of the conversation can assess a
	// lesson in the work record of the request it was delivered in (InRequest): it just did the work, and a session that sends
	// no further request is never asked again. Any other assessment waits for the conversation to be recorded.
	if a.Agent == "" && !a.InRequest {
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
	}
	old, err := s.memoryAssessment(scope, a.SourceID, a.Key)
	replace := errors.Is(err, errAssessmentBroken) // a file whose content cannot be read is replaced by this assessment (the query lists the delivery as not yet assessed; confirmation audit 2 of 0.12)
	if replace {
		old, err = nil, nil
	}
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
	if replace {
		_ = root.Remove(name) // the broken file gives way; the link below publishes the new one
	}
	return root.Link(tmp, name)
}
