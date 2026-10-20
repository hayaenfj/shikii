package store

// 会話の続きは観察であり、正解ではない。追加依頼も訂正も原文のまま残し、
// 後の似た依頼で参照する。通常の判定・正解とは別に records/feedback/ に置く。
import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Feedback struct {
	ID      string    `json:"id"` // 元の依頼の識別子。繰り返した同じ文も別の依頼
	Request string    `json:"request"`
	Summary string    `json:"summary"`
	Reply   string    `json:"reply"` // 次に人が伝えた発言。訂正か追加かは未確定
	At      time.Time `json:"at"`
}

const (
	feedbackTextMax = 1500
	feedbackReadMax = 4 << 20 // 全履歴を毎ターン読まない
	feedbackRecent  = 256
	feedbackRecall  = 2
)

func (s Store) feedbackPath(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return filepath.Join(s.Dir, "records", "feedback", hex.EncodeToString(sum[:])+".jsonl")
}

func (s Store) AppendFeedback(scope string, f Feedback) error {
	if scope == "" || f.ID == "" || strings.TrimSpace(f.Request) == "" || strings.TrimSpace(f.Summary) == "" || strings.TrimSpace(f.Reply) == "" {
		return errors.New("feedback needs a project, turn, request, closing message and human reply")
	}
	if len(f.Request) > feedbackTextMax || len(f.Summary) > feedbackTextMax || len(f.Reply) > feedbackTextMax {
		return errors.New("feedback text exceeds the per-field limit")
	}
	if f.At.IsZero() {
		f.At = time.Now().UTC()
	}
	return appendJSONL(s.feedbackPath(scope), f)
}

// recentFeedback は同じ仕事の末尾だけ読む。先頭が行の途中ならその行を捨て、
// 壊れた行を飛ばす。途中までの書き込みも採らない。重複した ID は最新だけ。
func (s Store) recentFeedback(scope string) ([]Feedback, error) {
	f, err := os.Open(s.feedbackPath(scope))
	if os.IsNotExist(err) {
		return nil, nil
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
			return nil, nil
		}
	}
	var recent []Feedback
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return nil, err
			}
			break // 改行まで書かれた観察だけを使う
		}
		var v Feedback
		if len(line) > 32<<10 || json.Unmarshal([]byte(line), &v) != nil || v.ID == "" || v.Request == "" || v.Summary == "" || v.Reply == "" ||
			len(v.Request) > feedbackTextMax || len(v.Summary) > feedbackTextMax || len(v.Reply) > feedbackTextMax {
			continue
		}
		recent = append(recent, v)
		if len(recent) > feedbackRecent {
			copy(recent, recent[1:])
			recent = recent[:feedbackRecent]
		}
	}
	seen := map[string]bool{}
	out := make([]Feedback, 0, len(recent))
	for i := len(recent) - 1; i >= 0; i-- {
		v := recent[i]
		if !seen[v.ID] {
			seen[v.ID] = true
			out = append(out, v)
		}
	}
	return out, nil
}

// RecentFeedback は wiki に取り込む原資料。正解の一覧とは分離している。
func (s Store) RecentFeedback(scope string) ([]Feedback, error) { return s.recentFeedback(scope) }

// TextMatch は検索語のうち、文章に含まれる語の割合。
// 長い教訓でも短い検索語で見つけられる。確信度や実行を許す線ではない。
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

// RecallFeedback は依頼の語の重なりで最大 2 件を返す。返すのは原文で、
// 意味の判定・成功や失敗の認定・較正はしない。送信先もモデルも増やさない。
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
	// 同点なら新しく観察したものを先に(ファイルの順を使い、時計のずれに依らない)。
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

// 英単語・識別子と、日本語の隣り合う 2 文字。分かち書き用の依存や AI は要らない。
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
