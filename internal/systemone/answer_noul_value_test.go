package systemone

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A yes/no answer without a value and a yes/no answer of zero are different answers: the reader of a response
// must be able to tell them apart, through the client as well as when decoding directly.
func TestAnswerTellsAMissingNoulValueFromZero(t *testing.T) {
	for _, c := range []struct {
		body  string
		has   bool
		value float64
	}{
		{`{"type":"noul","noul":0}`, true, 0},
		{`{"type":"noul","noul":0.0}`, true, 0},
		{`{"type":"noul","noul":0.42}`, true, 0.42},
		{`{"type":"noul"}`, false, 0},
		{`{"type":"noul","noul":null}`, false, 0},
		{`{"type":"choice","choice":"a","probabilities":{"a":0.9,"b":0.1}}`, false, 0},
		{`{}`, false, 0},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"model":"m","answers":{"q":` + c.body + `}}`))
		}))
		resp, err := New(srv.URL, "", "m").Ask(context.Background(), "s", map[string]Question{"q": {Type: "noul", Instructions: "?"}})
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", c.body, err)
		}
		if a := resp.Answers["q"]; a.HasNoul != c.has || a.Noul != c.value {
			t.Errorf("%s: got HasNoul=%v Noul=%v, want %v %v", c.body, a.HasNoul, a.Noul, c.has, c.value)
		}
	}
	// The other fields are read as before.
	var a Answer
	if err := json.Unmarshal([]byte(`{"type":"score","score":1.5,"confidence":0.5,"probabilities":{"0":0.5,"1":0.5},"legend":{"0":"low"}}`), &a); err != nil {
		t.Fatal(err)
	}
	if a.Type != "score" || a.Score != 1.5 || a.Confidence != 0.5 || a.Probabilities["1"] != 0.5 || a.Legend["0"] != "low" || a.HasNoul {
		t.Fatalf("the other fields must be read as before: %+v", a)
	}
	// A value that is not a number is still a bad response.
	if err := json.Unmarshal([]byte(`{"type":"noul","noul":"high"}`), &a); err == nil {
		t.Fatal("a noul value that is not a number must not be read")
	}
}

// A fake model that builds its answers from the Answer type writes a yes/no answer with its value, a zero
// included: left out, the zero would arrive as an answer without a value. Other answers carry no noul field.
func TestAnswerWritesAYesNoValueOfZero(t *testing.T) {
	for _, p := range []float64{0, 0.42} {
		b, err := json.Marshal(Answer{Type: "noul", Noul: p})
		if err != nil {
			t.Fatal(err)
		}
		var back Answer
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"noul":`) || !back.HasNoul || back.Noul != p || back.Type != "noul" {
			t.Errorf("p=%v: written as %s, read back as %+v", p, b, back)
		}
	}
	// Inside a Response (a map of answers), as the fakes write it.
	b, err := json.Marshal(Response{Model: "m", Answers: map[string]Answer{"q": {Type: "noul", Noul: 0}}})
	if err != nil || !strings.Contains(string(b), `"noul":0`) {
		t.Fatalf("a zero inside a response must be written: %s %v", b, err)
	}
	b, err = json.Marshal(Answer{Type: "choice", Choice: "a", Confidence: 0.9, Probabilities: map[string]float64{"a": 0.9, "b": 0.1}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "noul") || !strings.Contains(string(b), `"choice":"a"`) || !strings.Contains(string(b), `"probabilities":{"a":0.9,"b":0.1}`) || !strings.Contains(string(b), `"confidence":0.9`) {
		t.Fatalf("a choice answer must be written as before, without a noul field: %s", b)
	}
}
