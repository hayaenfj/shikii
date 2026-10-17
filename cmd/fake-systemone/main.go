// fake-systemone は System One 形式(/v1/systemone)を話す偽のモデル。鍵も GPU も無しで
// decide → label → calibrate → check → drift の輪を試すためのもの。判定の中身は語の重なりで決める
// おもちゃで、答えは state と質問から決まる(同じ入力には同じ答え)。
//
//	fake-systemone [--port 8099] [--temperature 1.0] [--overconfident]
//
// --overconfident は確率を端に寄せる(較正が要るモデルの真似)。
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"unicode"
)

type question struct {
	Type         string          `json:"type"`
	Instructions string          `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria"`
}

type request struct {
	State     json.RawMessage     `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]question `json:"questions"`
}

func main() {
	port := flag.Int("port", 8099, "待ち受けポート")
	temp := flag.Float64("temperature", 1.0, "分布の鋭さ(小さいほど端に寄る)")
	over := flag.Bool("overconfident", false, "確率を端に寄せる(temperature 0.35 相当)")
	flag.Parse()
	if *over {
		*temp = 0.35
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
	http.HandleFunc("/v1/systemone", func(w http.ResponseWriter, r *http.Request) {
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"bad json"}`, 400)
			return
		}
		state := stateText(req.State)
		answers := map[string]any{}
		for key, q := range req.Questions {
			a, err := answer(state, q, *temp)
			if err != nil {
				http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), 400)
				return
			}
			answers[key] = a
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "fake-systemone/1",
			"answers": answers,
			"usage":   map[string]int{"input_tokens": len(state) / 4, "output_tokens": 0},
		})
	})
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("fake-systemone listening on http://%s (temperature=%.2f)", addr, *temp)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func stateText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// answer は型ごとに分布を作る。語の重なり + 入力由来の決定的な揺れ。
func answer(state string, q question, temp float64) (map[string]any, error) {
	switch q.Type {
	case "choice":
		var crit map[string]string
		if err := json.Unmarshal(q.Criteria, &crit); err != nil {
			return nil, fmt.Errorf("choice criteria must be an object")
		}
		ids := make([]string, 0, len(crit))
		for id := range crit {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		z := make([]float64, len(ids))
		for i, id := range ids {
			z[i] = overlap(state, id+" "+crit[id])*10 + jitter(state, q.Instructions, id)
		}
		p := softmax(z, temp)
		probs := map[string]float64{}
		best, bp := "", -1.0
		for i, id := range ids {
			probs[id] = p[i]
			if p[i] > bp {
				best, bp = id, p[i]
			}
		}
		n := float64(len(ids))
		conf := 0.0
		if n > 1 {
			conf = (bp - 1/n) / (1 - 1/n)
		}
		return map[string]any{"type": "choice", "choice": best, "probabilities": probs, "confidence": conf}, nil
	case "score":
		var levels []string
		if err := json.Unmarshal(q.Criteria, &levels); err != nil {
			return nil, fmt.Errorf("score criteria must be an array")
		}
		z := make([]float64, len(levels))
		for i, l := range levels {
			z[i] = overlap(state, l)*10 + jitter(state, q.Instructions, l)
		}
		p := softmax(z, temp)
		probs := map[string]float64{}
		legend := map[string]string{}
		var exp float64
		for i := range levels {
			k := fmt.Sprintf("%d", i)
			probs[k] = p[i]
			legend[k] = levels[i]
			exp += float64(i) * p[i]
		}
		return map[string]any{"type": "score", "score": exp, "probabilities": probs, "legend": legend, "confidence": maxOf(p)}, nil
	case "noul":
		z := overlap(state, q.Instructions)*10 - 2 + jitter(state, q.Instructions, "yes")
		return map[string]any{"type": "noul", "noul": 1 / (1 + math.Exp(-z/temp))}, nil
	}
	return nil, fmt.Errorf("unknown question type %q", q.Type)
}

// overlap は state の語(英単語か文字 2-gram)のうち text にも出るものの割合。
// state 側で割るので、説明文が長くても不利にならない(短い依頼文 vs 長い選択肢の説明、が典型)。
func overlap(state, text string) float64 {
	a, b := grams(state), grams(text)
	if len(a) == 0 {
		return 0
	}
	hit := 0
	for g := range a {
		if b[g] && !stop[g] {
			hit++
		}
	}
	return float64(hit) / float64(len(a))
}

// stop は重なりに数えない語(どの説明にも出る)。
var stop = map[string]bool{"the": true, "a": true, "an": true, "to": true, "or": true, "and": true, "of": true, "in": true, "is": true, "it": true, "する": true, "して": true}

func grams(s string) map[string]bool {
	out := map[string]bool{}
	var word []rune
	flush := func() {
		if len(word) >= 2 {
			out[strings.ToLower(string(word))] = true
		}
		word = word[:0]
	}
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) && r < 0x3000:
			word = append(word, r)
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
			flush()
			word = append(word, r)
		default:
			flush()
		}
	}
	flush()
	// 日本語は文字 2-gram も足す
	rs := []rune(s)
	for i := 0; i+1 < len(rs); i++ {
		if rs[i] >= 0x3000 && rs[i+1] >= 0x3000 {
			out[string(rs[i:i+2])] = true
		}
	}
	return out
}

// jitter は入力から決まる小さな揺れ(−0.5..0.5)。同じ入力には同じ値。
func jitter(parts ...string) float64 {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return float64(binary.LittleEndian.Uint32(h[:4]))/math.MaxUint32 - 0.5
}

func softmax(z []float64, temp float64) []float64 {
	m := math.Inf(-1)
	for _, v := range z {
		m = math.Max(m, v)
	}
	out := make([]float64, len(z))
	var sum float64
	for i, v := range z {
		out[i] = math.Exp((v - m) / temp)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

func maxOf(xs []float64) float64 {
	m := 0.0
	for _, x := range xs {
		m = math.Max(m, x)
	}
	return m
}
