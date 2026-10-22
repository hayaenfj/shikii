package main

// 結果から付く正解(source outcome)。AI の解釈は使わず、作業ツリーと人の発言の形だけを見る。
// prompt-kind: 依頼の間に作業ツリーが変わったか(settlePending)。
// changes-in-scope: そのターンで変えたファイルが、次の人の依頼の間に元へ戻されたか(戻された = 範囲の外。戻されずに次の次の依頼まで残った = 範囲の中、部分の重み)。
// done-as-asked: 次の人の発言が同じ依頼の言い直しか(言い直し = 終えていない。はっきり別の依頼 = 終えた、部分の重み)。短い返事と中間は付けない。
// 観測は代わりの物差しで、人の正解と同じ重みにはしない(はっきり悪い側だけ重み 1)。

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hayaenfj/shikii/internal/decide"
	"github.com/hayaenfj/shikii/internal/store"
)

// 固定の規則(オプションは無い)。
const (
	outcomeKeptWeight  = 0.5 // 戻されずに残った・別の依頼へ進んだ(悪くなかった印。良かった証拠ではない)
	outcomeRepeatAbove = 0.6 // 前の依頼との似方(文字の 2 つ組の Dice)がこれ以上なら言い直し
	outcomeMovedBelow  = 0.2 // これ以下なら別の依頼
	scopeWatchTurns    = 2   // 変えたファイルを見張る人の依頼の数(次の依頼の間に戻されたかを、その次の依頼で決める)
	scopeWatchFiles    = 200 // 見張るファイルの上限(超えた分は見ない)
)

// outcomeSpecs は結果から正解が付く判定。迷い(ask)でも人の答えを待たない(結果が答える)。
var outcomeSpecs = map[string]bool{"prompt-kind": true, "changes-in-scope": true, "done-as-asked": true, "wiki-place": true, "wiki-conflict": true}

// waitsForPersonFor は、この判定が人の答えを待つか。
// 線の無い判定は自動で何もしないので抜き取る物が無い。結果から正解が付く判定は迷っても結果を待つ。
func waitsForPersonFor(specID string, d *decide.Decision) bool {
	if d.Band == decide.Act {
		return d.IsCalibrated && store.AuditSample(d.TraceID)
	}
	return d.Band == decide.Ask && !outcomeSpecs[specID]
}

// scopeWatch は changes-in-scope の判定と、そのターンで変えたファイルのターン前の状態("mode sha"、無かったファイルは "")。
type scopeWatch struct {
	Trace  string            `json:"trace"`
	Before map[string]string `json:"before"`
	Seen   int               `json:"seen,omitempty"` // 判定の後に来た人の依頼の数
}

// newScopeWatch は変えたファイルのターン前の状態を控える。
func newScopeWatch(trace, before string, changed []string) scopeWatch {
	prior := parseListing(before)
	w := scopeWatch{Trace: trace, Before: map[string]string{}}
	for i, p := range changed {
		if i >= scopeWatchFiles {
			break
		}
		w.Before[p] = prior[p]
	}
	return w
}

// reverted は、控えたファイルのどれかがターン前の状態へ戻ったか(無かったファイルは消えたか)。
func (w scopeWatch) reverted(now map[string]string) bool {
	for p, before := range w.Before {
		if now[p] == before {
			return true
		}
	}
	return false
}

// settleScope は見張っている changes-in-scope に結果の正解を付ける(錠の中)。listed が false(git の外か間に合わない)なら比べない。
// boundary は人の依頼の境目: 見張りの数を 1 つ進め、scopeWatchTurns に届いて戻されていなければ「はい」(部分の重み)。
// 戻されたかは、判定の後に人の依頼が 1 つ以上来てから見る(同じ依頼の続きでエージェントが自分で戻したものは数えない)。
func settleScope(agent string, s *hookSession, listing string, listed, boundary bool) error {
	if len(s.Watch) == 0 {
		return nil
	}
	st := store.Store{Dir: agent}
	now := parseListing(listing)
	var firstErr error
	var rest []scopeWatch
	for _, w := range s.Watch {
		if listed && w.Seen >= 1 && w.reverted(now) {
			if err := labelTrace(st, "changes-in-scope", w.Trace, "no", 1, "outcome"); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if boundary {
			w.Seen++
		}
		if w.Seen >= scopeWatchTurns {
			if listed {
				if err := labelTrace(st, "changes-in-scope", w.Trace, "yes", outcomeKeptWeight, "outcome"); err != nil && firstErr == nil {
					firstErr = err
				}
			}
			continue // 比べられなかった見張りは付けずに捨てる
		}
		rest = append(rest, w)
	}
	s.Watch = rest
	return firstErr
}

// settleDone は前の依頼の done-as-asked に、次の人の発言の形から結果の正解を付ける(錠の中、依頼の境目)。
// 短い返事(「ok」「続けて」)と、似ても似つかなくもない中間は付けない。
func settleDone(agent string, c *completedTurn, reply string) error {
	if c == nil || utf8.RuneCountInString(strings.TrimSpace(reply)) < shortReply {
		return nil
	}
	trace := ""
	for _, t := range c.Targets {
		if t.Spec == "done-as-asked" {
			trace = t.TraceID
		}
	}
	if trace == "" {
		return nil
	}
	st := store.Store{Dir: agent}
	switch sim := textSimilarity(c.Request, reply); {
	case sim >= outcomeRepeatAbove:
		return labelTrace(st, "done-as-asked", trace, "no", 1, "outcome")
	case sim <= outcomeMovedBelow:
		return labelTrace(st, "done-as-asked", trace, "yes", outcomeKeptWeight, "outcome")
	}
	return nil
}

// textSimilarity は 2 つの文の似方(文字の 2 つ組の Dice 係数、0〜1)。空白・記号と大文字小文字を無視する。言語に依らない。
func textSimilarity(a, b string) float64 {
	grams := func(s string) map[string]int {
		var rs []rune
		for _, r := range strings.ToLower(s) {
			if unicode.IsLetter(r) || unicode.IsNumber(r) {
				rs = append(rs, r)
			}
		}
		m := map[string]int{}
		for i := 0; i+1 < len(rs); i++ {
			m[string(rs[i:i+2])]++
		}
		return m
	}
	ga, gb := grams(a), grams(b)
	na, nb := 0, 0
	for _, n := range ga {
		na += n
	}
	for _, n := range gb {
		nb += n
	}
	if na+nb == 0 {
		return 0
	}
	common := 0
	for g, n := range ga {
		common += min(n, gb[g])
	}
	return 2 * float64(common) / float64(na+nb)
}

// parseListing は `git ls-files --stage` の一覧を path → "mode sha" にする。
func parseListing(listing string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(listing, "\n") {
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		meta := strings.Fields(line[:tab])
		if len(meta) < 2 {
			continue
		}
		m[line[tab+1:]] = meta[0] + " " + meta[1] // mode と blob の id(chmod だけの変更も「変わった」)
	}
	return m
}
