package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The entries that read with decodeObject (they pass over keys they do not know): what they answer for the inputs without an
// object, a byte order mark, a key they do not know, and a body over the limit.
func TestAuditGPassingOverEntriesOverManyBodies(t *testing.T) {
	_, _, h, _, _ := memoryEffectFixture(t)
	bom := "\xef\xbb\xbf"
	bodies := []struct{ name, body, want, not string }{
		{"empty", "", "(got nothing)", ""},
		{"white space", " \n\t ", "(got nothing)", ""},
		{"a mark alone", bom, "(got nothing)", ""},
		{"a mark and a break", bom + "\n", "(got nothing)", ""},
		{"null", "null", "(got null)", ""},
		{"spaces and null", "  \n null ", "(got null)", ""},
		{"a mark and null", bom + "null", "(got null)", ""},
		{"null then an object", "null {}", "(got null)", ""},
		{"a string", `"x"`, "(got string)", ""},
		{"an array", `[]`, "(got array)", ""},
		{"cut", `{"spec":`, "the input ends there", ""},
		{"an unknown key", `{"spec":"no-such-spec","zz_unknown":1}`, "", "unknown key"},
		{"a mark and an unknown key", bom + `{"spec":"no-such-spec","zz_unknown":1}`, "", "malformed"},
		{"too large", "{" + strings.Repeat(" ", maxBody+10) + "}", "", "pass the JSON"},
	}
	for _, path := range []string{"/v1/decide", "/v1/label", "/v1/redo", "/v1/answers/restore", "/v1/tune/run", "/v1/tune/adopt", "/v1/tune/review", "/v1/tune/variant", "/v1/wiki/claim"} {
		for _, b := range bodies {
			r := httptest.NewRecorder()
			req := httptest.NewRequest("POST", path, strings.NewReader(b.body))
			req.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(r, req)
			got := strings.Join(strings.Fields(r.Body.String()), " ")
			if b.want != "" && !strings.Contains(got, b.want) || b.not != "" && strings.Contains(got, b.not) || strings.Contains(got, "json: cannot") || strings.Contains(got, "invalid character") || strings.Contains(got, "EOF") {
				t.Errorf("%-20s %-28s -> %d %.200s", path, b.name, r.Code, got)
			} else if b.name == "too large" || b.name == "an unknown key" {
				t.Logf("%-20s %-28s -> %d %.160s", path, b.name, r.Code, got)
			}
		}
	}
}
