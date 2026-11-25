package store

// The rest of a conversation is an observation, not a truth. Follow-up requests and corrections are kept verbatim
// and consulted on later similar requests. Kept in records/feedback/, apart from ordinary decisions and truths.
import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Feedback struct {
	ID      string         `json:"id"` // identifier of the original request. The same text repeated is still a separate request
	Request string         `json:"request"`
	Summary string         `json:"summary"`
	Reply   string         `json:"reply"` // what the person said next. Whether it is a correction or an addition is not decided
	At      time.Time      `json:"at"`
	Targets []ReviewTarget `json:"targets,omitempty"`
	// Session is the agent session the conversation took place in. The lessons delivered in it are listed for assessment to that
	// session only: another session never saw how they were used and could only answer unknown, which is then final. Empty for
	// older records, which stay listed for every session. It is not part of a wiki source's hash (WithoutSession).
	Session string `json:"session,omitempty"`
}

// WithoutSession is the conversation as a wiki source keeps and hashes it: the session is who assesses its deliveries, not what
// was said, and a shikii from before the field reads the record without it.
func (f Feedback) WithoutSession() Feedback {
	f.Session = ""
	return f
}

type ReviewTarget struct {
	Spec    string `json:"spec"`
	TraceID string `json:"trace_id"`
}

const (
	FeedbackTextLimit = 1500
	// AssessmentTextLimit is how many characters the decision and the evidence of a memory assessment take, each. It counts
	// characters, not bytes: a text in Japanese (three bytes a character) gets the same room as one in English. A saved
	// assessment stays under the 32 KiB its file is read with: a character is at most six bytes in the file.
	AssessmentTextLimit = 1500
	feedbackTextMax     = FeedbackTextLimit
	feedbackReadMax     = 4 << 20  // do not read the whole history every turn
	feedbackLineMax     = 32 << 10 // the longest line (its newline counted) that is a conversation
	feedbackRecent      = 256
	feedbackRecall      = 2
)

func (s Store) feedbackPath(scope string) string {
	return s.recordPath("feedback", ".jsonl", scope)
}

func (s Store) AppendFeedback(scope string, f Feedback) error {
	if scope == "" || f.ID == "" || strings.TrimSpace(f.Request) == "" || strings.TrimSpace(f.Summary) == "" || strings.TrimSpace(f.Reply) == "" {
		return errors.New("feedback needs a project, turn, request, closing message and human reply")
	}
	if len(f.Request) > feedbackTextMax || len(f.Summary) > feedbackTextMax || len(f.Reply) > feedbackTextMax || len(f.Targets) > 2 {
		return errors.New("feedback text exceeds the per-field limit")
	}
	if f.At.IsZero() {
		f.At = time.Now().UTC()
	}
	return s.appendJSONL(s.feedbackPath(scope), f)
}

// recentFeedback reads only the tail of the same work. If the start is mid-line, that line is dropped,
// and broken lines are skipped. Partly written lines are not taken either. For duplicate IDs only the newest is kept.
func (s Store) recentFeedback(scope string) ([]Feedback, error) {
	return s.readRecentFeedback(scope, nil)
}

func (s Store) readRecentFeedback(scope string, capture hash.Hash) ([]Feedback, error) {
	f, start, err := s.openRecentHistory(s.feedbackPath(scope))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var input io.Reader = io.LimitReader(f, feedbackReadMax)
	if capture != nil {
		capture.Write([]byte("present\n"))
		if start > 0 {
			capture.Write([]byte("skip-first\n"))
		}
		input = io.TeeReader(input, capture)
	}
	r := bufio.NewReader(input)
	if start > 0 {
		if _, err := r.ReadString('\n'); err != nil {
			return nil, nil
		}
	}
	recent := make([]Feedback, feedbackRecent)
	count := 0
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
			break // use only observations written up to the newline
		}
		v, ok := decodeFeedbackLine([]byte(line))
		if !ok {
			continue
		}
		recent[count%feedbackRecent] = v
		count++
	}
	seen := map[string]bool{}
	out := make([]Feedback, 0, min(count, feedbackRecent))
	for i := count - 1; i >= max(0, count-feedbackRecent); i-- {
		v := recent[i%feedbackRecent]
		if !seen[v.ID] {
			seen[v.ID] = true
			out = append(out, v)
		}
	}
	return out, nil
}

// RecentFeedback is source material taken into the wiki. Kept apart from the list of truths.
func (s Store) RecentFeedback(scope string) ([]Feedback, error) { return s.recentFeedback(scope) }

// decodeFeedbackLine is one line of the history, or false for a line that is not a conversation: too long, broken, or missing a part.
func decodeFeedbackLine(line []byte) (Feedback, bool) {
	var v Feedback
	if len(line) > feedbackLineMax || json.Unmarshal(line, &v) != nil || v.ID == "" || v.Request == "" || v.Summary == "" || v.Reply == "" ||
		len(v.Request) > feedbackTextMax || len(v.Summary) > feedbackTextMax || len(v.Reply) > feedbackTextMax || len(v.Targets) > 2 {
		return v, false
	}
	return v, true
}

// FeedbackMark is a place in the history of conversations: the SHA-256 (hex) of one line's bytes without its newline, and the byte offset
// just after that line when the mark was made. A read after the mark goes on after the first line of the file that has the hash, found by
// hashing the lines from the beginning, and from the beginning when no line has it: the offset is not used to find the place, because
// shikii forget rewrites the file (removing the lines of a finished session) and a copy of the same line may then end at the old offset
// with lines between the copies never read. The history is only appended to, under its lock, so a line written after the marked one is
// after it in the file, whatever time it carries. Offset is kept in the stored form of a mark (a zero Offset or an empty Hash is the
// zero mark, the beginning).
type FeedbackMark struct {
	Offset int64
	Hash   string
}

// FeedbackLine is a conversation with the mark just after the line it was read from.
type FeedbackLine struct {
	Feedback
	Mark FeedbackMark
}

// FeedbackAfter is every conversation of the scope written after the mark (all of them for the zero mark), in the order they were
// written. It reads the file from the mark to its end, line by line, with no limit on the count or the size of the history:
// RecentFeedback's window (the newest 256 within the last 4 MiB) was made for recalling similar past answers, not for going through what
// has piled up. A line is taken by the same rules: a broken or too long line is skipped, a last line that is not ended by a newline yet is
// not taken (so no mark is ever past it), and of a repeated ID only the newest line stands, at its own place.
//
// The read goes on after the first line of the file that has the mark's hash, and from the beginning when no line has it (a conversation
// may then be read again, none is lost). The whole file is hashed once for it, which the daily run of shikii nap can afford.
func (s Store) FeedbackAfter(scope string, mark FeedbackMark) ([]FeedbackLine, error) {
	f, err := s.openFile(s.feedbackPath(scope), os.O_RDONLY)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	from, err := feedbackResume(f, mark)
	if err != nil {
		return nil, err
	}
	var rows []FeedbackLine   // in file order; a line replaced by a newer one of its ID is left empty
	place := map[string]int{} // the place in rows of the newest line of each ID
	err = scanFeedbackLines(f, from, func(line []byte, end int64) bool {
		v, ok := decodeFeedbackLine(line)
		if !ok {
			return true
		}
		if at, seen := place[v.ID]; seen {
			rows[at] = FeedbackLine{}
		}
		place[v.ID] = len(rows)
		rows = append(rows, FeedbackLine{v, FeedbackMark{end, feedbackLineHash(line)}})
		return true
	})
	if err != nil {
		return nil, err
	}
	out := make([]FeedbackLine, 0, len(place))
	for _, row := range rows {
		if row.ID != "" {
			out = append(out, row)
		}
	}
	return out, nil
}

// FeedbackByID is the conversation with the id from the whole history, not only the newest 256 of it (the newest line when the id was
// written more than once), read line by line. false when no line of the history holds it.
func (s Store) FeedbackByID(scope, id string) (Feedback, bool, error) {
	f, err := s.openFile(s.feedbackPath(scope), os.O_RDONLY)
	if os.IsNotExist(err) {
		return Feedback{}, false, nil
	}
	if err != nil {
		return Feedback{}, false, err
	}
	defer f.Close()
	var found Feedback
	ok := false
	err = scanFeedbackLines(f, 0, func(line []byte, _ int64) bool {
		if v, good := decodeFeedbackLine(line); good && v.ID == id {
			found, ok = v, true
		}
		return true
	})
	return found, ok, err
}

// scanFeedbackLines gives fn each complete line of f from the offset from (its newline included) and the offset just after it, until fn
// returns false. A line longer than the history keeps is not given, and neither is a last line with no newline yet.
func scanFeedbackLines(f *os.File, from int64, fn func(line []byte, end int64) bool) error {
	if _, err := f.Seek(from, io.SeekStart); err != nil {
		return err
	}
	r := bufio.NewReaderSize(f, 64<<10)
	end := from
	for {
		line, err := r.ReadSlice('\n')
		n := len(line)
		tooLong := false
		for err == bufio.ErrBufferFull { // a line longer than the buffer is never taken: read on to its end
			tooLong = true
			var rest []byte
			rest, err = r.ReadSlice('\n')
			n += len(rest)
		}
		if err != nil {
			if err != io.EOF {
				return err
			}
			return nil // use only observations written up to the newline
		}
		end += int64(n)
		if tooLong || n > feedbackLineMax {
			continue
		}
		if !fn(line, end) {
			return nil
		}
	}
}

// feedbackLineHash is the hash a mark holds: the SHA-256 of the line without its newline, in hex.
func feedbackLineHash(line []byte) string {
	sum := sha256.Sum256(bytes.TrimSuffix(line, []byte("\n")))
	return hex.EncodeToString(sum[:])
}

// feedbackResume is the offset a read after mark starts from: the end of the first line of the file (from its beginning) that has the
// mark's hash, else 0. The mark's own offset is not trusted even when the line that ends there has the hash: the same line may be in the
// file twice, and after shikii forget removed lines before the mark, the offset may be the end of the second copy, with what lies between
// the copies never read. Starting after the first copy reads a conversation again at worst, and skips none.
func feedbackResume(f *os.File, mark FeedbackMark) (int64, error) {
	if mark.Offset <= 0 || mark.Hash == "" {
		return 0, nil
	}
	found := int64(0)
	err := scanFeedbackLines(f, 0, func(line []byte, end int64) bool {
		if feedbackLineHash(line) == mark.Hash {
			found = end
			return false
		}
		return true
	})
	return found, err
}

// TextMatch is the share of query words that appear in the text.
// A long lesson can still be found with short query words. It is not a confidence or a line that allows acting.
func TextMatch(query, text string) float64 {
	a, b := feedbackTerms(query), feedbackTerms(text)
	common := 0
	for word := range a {
		if b[word] {
			common++
		}
	}
	if len(a) == 0 || (common < 2 && len(a) > 1) {
		return 0
	}
	return float64(common) / float64(len(a))
}

// RecallFeedback returns at most 2 by overlap with the request's words. It returns the original text and
// does not judge meaning, certify success or failure, or calibrate. It adds no destinations or models.
func (s Store) RecallFeedback(scope, request string) ([]Feedback, error) {
	if scope == "" {
		return nil, nil
	}
	query := feedbackTerms(request)
	if len(query) == 0 {
		return nil, nil
	}
	recent, err := s.recentFeedback(scope)
	if err != nil {
		return nil, err
	}
	type match struct {
		feedback Feedback
		score    float64
	}
	var matches []match
	for _, v := range recent {
		terms := feedbackTerms(v.Request)
		common := 0
		for word := range query {
			if terms[word] {
				common++
			}
		}
		score := 2 * float64(common) / float64(len(query)+len(terms))
		if common >= 2 && score >= 0.35 {
			matches = append(matches, match{v, score})
		}
	}
	// on ties, the newer observation comes first (uses file order, independent of clock skew).
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	var out []Feedback
	for _, m := range matches {
		out = append(out, m.feedback)
		if len(out) == feedbackRecall {
			break
		}
	}
	return out, nil
}

// English words and identifiers, and adjacent pairs of Japanese characters. No dependency or AI for word segmentation needed.
func feedbackTerms(text string) map[string]bool {
	out := map[string]bool{}
	var word, japanese []rune
	flush := func() {
		if len(word) >= 3 {
			w := string(word)
			switch w {
			case "the", "and", "for", "with", "this", "that", "please", "fix", "add", "update":
			default:
				out[w] = true
			}
		}
		for i := 1; i < len(japanese); i++ {
			out[string(japanese[i-1:i+1])] = true
		}
		word, japanese = nil, nil
	}
	for _, r := range strings.ToLower(text) {
		ja := unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana)
		switch {
		case ja:
			if len(word) > 0 {
				flush()
			}
			japanese = append(japanese, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			if len(japanese) > 0 {
				flush()
			}
			word = append(word, r)
		default:
			flush()
		}
	}
	flush()
	return out
}
