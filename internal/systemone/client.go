// Package systemone は System One 形式(TypeSafe Jev / llama.cpp /v1/systemone 等)の
// 薄いクライアント。形式は所有しない。ベース URL の違いだけを吸収する。
package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Question は 1 つの質問。Criteria は choice なら OrderedCriteria(id→説明、並び順を保つ)、score なら []string。
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// OrderedCriteria は choice の選択肢。JSON オブジェクトとして**この並びのまま**送る
// (Go の map は鍵がソートされるので、順序バイアスの測定や spec の並びの再現ができない)。
type OrderedCriteria []Criterion

// Criterion は選択肢 1 つ。
type Criterion struct {
	ID          string
	Description string
}

// MarshalJSON は {"id":"desc",…} を並び順どおりに出す。
func (oc OrderedCriteria) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, c := range oc {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(c.ID)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(c.Description)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// UnmarshalJSON は受け取り側(テストのモック)用。JSON の並び順を保って読む。
func (oc *OrderedCriteria) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("criteria: expected object")
	}
	var out OrderedCriteria
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return err
		}
		var v string
		if err := dec.Decode(&v); err != nil {
			return err
		}
		out = append(out, Criterion{ID: kt.(string), Description: v})
	}
	*oc = out
	return nil
}

// Request は /v1/systemone の本体。
type Request struct {
	State     string              `json:"state"`
	Model     string              `json:"model,omitempty"`
	Questions map[string]Question `json:"questions"`
}

// Answer は 1 質問の答え。型ごとに使うフィールドが違う。
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

// Response は応答。Model は実際に答えたモデル(例: "jev-1.13.0")。
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Client は 1 エンドポイントへの接続。
type Client struct {
	BaseURL string // 例: https://api.typesafe.ai, http://127.0.0.1:8080
	Path    string // 既定 /v1/systemone。OpenRouter は /api/alpha/decisions
	APIKey  string // 空なら Authorization を付けない(ローカル)
	Model   string // 例: jev-latest、typesafe/jev-1.13(OpenRouter)
	HTTP    *http.Client
}

// DefaultPath は System One 形式の標準の口。TypeSafe・llama.cpp・多くの互換サーバーが使う。
const DefaultPath = "/v1/systemone"

// New はクライアントを作る。baseURL にパスが含まれていれば(例: https://openrouter.ai/api/alpha/decisions)
// それをそのまま使い、無ければ /v1/systemone を足す。
// RedactURL は URL に埋め込まれた資格情報(user:token@host)を伏せる。ログと誤り文に使う。
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Redacted()
}

func New(baseURL, apiKey, model string) *Client {
	base, path := SplitEndpoint(baseURL)
	return &Client{
		BaseURL: base,
		Path:    path,
		APIKey:  apiKey,
		Model:   model,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// SplitEndpoint は URL を「ホストまで」と「パス」に分ける。パスが無い("/" だけ)なら DefaultPath。
func SplitEndpoint(raw string) (base, path string) {
	raw = strings.TrimRight(raw, "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw, DefaultPath
	}
	if u.Path == "" {
		return raw, DefaultPath
	}
	u2 := *u
	u2.Path, u2.RawPath, u2.RawQuery = "", "", ""
	return u2.String(), u.Path
}

// Ask は state に対して質問群を 1 回で評価する。
func (c *Client) Ask(ctx context.Context, state string, qs map[string]Question) (*Response, error) {
	body, err := json.Marshal(Request{State: state, Model: c.Model, Questions: qs})
	if err != nil {
		return nil, err
	}
	path := c.Path
	if path == "" {
		path = DefaultPath
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("systemone %s: HTTP %d: %s", RedactURL(c.BaseURL), resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("systemone: bad response: %w", err)
	}
	return &out, nil
}
