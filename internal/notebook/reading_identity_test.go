package notebook

import "testing"

func TestReadingQualityUsesAnExplicitResultBoundary(t *testing.T) {
	d := ReadingDetails{Check: "Keep the condition.", Sufficient: true}
	pairs := [][2]string{
		{"All conditions retained.", `{"observed":"All conditions retained.","anything":{"request":"a"}}`},
		{`{"observed":"All conditions retained.","provenance":{"task_id":"a"}}`, `{"observed":"All conditions retained.","other_metadata":[1,2]}`},
		{`{"observed":{"checks":1}}`, `{"type":"reading-quality/v1","check":"Keep the condition.","sufficient":true,"observed":"{\"checks\":1.0}"}`},
	}
	for _, p := range pairs {
		a, e := readingQualityIdentity(p[0], d)
		if e != nil {
			t.Fatal(e)
		}
		b, e := readingQualityIdentity(p[1], d)
		if e != nil {
			t.Fatal(e)
		}
		if a != b {
			t.Error("ancillary fields or report format multiplied the same explicit result")
		}
	}
	for _, s := range []string{`{"quality":{"checks":1},"provenance":{"task":"a"}}`, `{"arbitrary":1}`, `[{"observed":"result"}]`, `{"observed":"result"} trailing-provenance`, `[{"observed":"result"}`, `{"observed":"{\"quality\":true"}`, `{"type":"reading-quality/v1","check":"Keep the condition.","sufficient":true,"observed":"{\"quality\":true"}`} {
		if _, e := readingQualityIdentity(s, d); e == nil {
			t.Error("ambiguous structured evidence must not teach a preference")
		}
	}
	a, _ := readingQualityIdentity(`{"observed":{"checks":1}}`, d)
	b, _ := readingQualityIdentity(`{"observed":{"checks":2}}`, d)
	if a == b {
		t.Fatal("different explicit results were collapsed")
	}
}

func TestReadingQualityJSONNumbersKeepExactValues(t *testing.T) {
	d := ReadingDetails{Check: "Keep the condition.", Sufficient: true}
	groups := [][]string{{"1", "1.0", "1e0", "10e-1", "0.01e2"}, {"0", "-0", "0e999999999", "-0.000"}, {"9007199254740993", "9007199254740993.0", "90071992547409930e-1"}, {"-1.23", "-123e-2", "-0.0123e2"}, {"1e1000000", "10e999999"}}
	for _, g := range groups {
		var first string
		for _, n := range g {
			s := `{"type":"reading-quality/v1","check":"Keep the condition.","sufficient":true,"observed":"{\"value\":` + n + `}"}`
			v, e := readingQualityIdentity(s, d)
			if e != nil {
				t.Fatal(e)
			}
			if first != "" && v != first {
				t.Errorf("equivalent exact numeric value diverged: %s", n)
			}
			first = v
		}
	}
	a, _ := readingQualityIdentity(`{"observed":{"value":9007199254740992}}`, d)
	b, _ := readingQualityIdentity(`{"observed":{"value":9007199254740993}}`, d)
	if a == b {
		t.Fatal("integer precision was lost")
	}
}
