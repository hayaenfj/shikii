package main

import (
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The number of items one work record takes in each field is written in three places: the refusal (recordItemsOverLimit), the
// maxItems of shikii_wiki's work schema, and the "Fixed limits" of the skill's recording.md. Nothing tied them together (audit of
// 0.33.0, U-9), so a limit changed in one place would leave an agent told one number and refused at another. Every slice field of
// workRequest is a field of the record: its limit is measured from the refusal itself and compared with the other two.
func TestRecordItemLimitsAgreeInTheRefusalTheSchemaAndTheGuide(t *testing.T) {
	enforced := enforcedRecordItemLimits(t)
	schema := schemaRecordItemLimits(t, enforced)
	guide := guideRecordItemLimits(t)

	// The check itself: a limit that differs in one place, or a field without a limit, fails.
	changed := map[string]int{}
	for k, v := range schema {
		changed[k] = v
	}
	changed["notes"]++
	if problem := recordItemLimitProblem(enforced, changed, guide); !strings.Contains(problem, "notes") {
		t.Fatalf("the check accepts a schema limit that differs from the refusal: %q", problem)
	}
	unlimited := map[string]int{"notes": 0}
	if problem := recordItemLimitProblem(unlimited, map[string]int{"notes": 2}, map[string]int{"notes": 2}); !strings.Contains(problem, "notes") {
		t.Fatalf("the check accepts a field the refusal does not limit: %q", problem)
	}

	if len(enforced) < 8 {
		t.Fatalf("measured only %d record fields: %v", len(enforced), enforced)
	}
	if problem := recordItemLimitProblem(enforced, schema, guide); problem != "" {
		t.Fatal(problem)
	}
}

// enforcedRecordItemLimits measures, for each slice field of workRequest (by its JSON name), the largest number of items
// recordItemsOverLimit accepts; 0 means it accepted every count tried.
func enforcedRecordItemLimits(t *testing.T) map[string]int {
	t.Helper()
	limits := map[string]int{}
	typ := reflect.TypeOf(workRequest{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type.Kind() != reflect.Slice || !field.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		limits[name] = 0
		for n := 1; n <= 1024; n++ {
			var req workRequest
			reflect.ValueOf(&req).Elem().Field(i).Set(reflect.MakeSlice(field.Type, n, n))
			refused := false
			for _, over := range recordItemsOverLimit(req) {
				refused = refused || strings.HasPrefix(over, name+": ")
			}
			if refused {
				limits[name] = n - 1
				break
			}
		}
	}
	return limits
}

func schemaRecordItemLimits(t *testing.T, fields map[string]int) map[string]int {
	t.Helper()
	properties := workSchema["properties"].(map[string]any)
	limits := map[string]int{}
	for name := range fields {
		field, _ := properties[name].(map[string]any)
		max, ok := field["maxItems"].(int)
		if !ok {
			t.Errorf("the work schema has no maxItems for %s: %v", name, field["maxItems"])
		}
		limits[name] = max
	}
	return limits
}

// recordingGuideWords are the words the "Fixed limits" paragraph of recording.md uses for each field. A new field of the record
// fails the comparison until its words are here and its limit is in the guide.
var recordingGuideWords = map[string]string{"observations": "observations", "notes": "notes", "reviews": "scores", "memory": "memory assessments",
	"reviewed": "reviewed sources", "maintenance": "maintenance reviews", "discoveries": "discoveries", "outcomes": "procedure outcomes"}

func guideRecordItemLimits(t *testing.T) map[string]int {
	t.Helper()
	var paragraph string
	for _, line := range strings.Split(string(mustRead(t, "../../skills/shikii/references/recording.md")), "\n") {
		if strings.HasPrefix(line, "Fixed limits:") {
			paragraph = line
		}
	}
	_, batch, found := strings.Cut(paragraph, "at most ")
	batch, _, ended := strings.Cut(batch, " per batch")
	if !found || !ended {
		t.Fatalf("recording.md has no \"Fixed limits: ... at most ... per batch\" sentence: %q", paragraph)
	}
	limits := map[string]int{}
	for name, words := range recordingGuideWords {
		m := regexp.MustCompile(`(?:^|, | and )(\d+) ` + regexp.QuoteMeta(words) + `(?:,| and |$)`).FindStringSubmatch(batch)
		if m == nil {
			continue
		}
		limits[name], _ = strconv.Atoi(m[1])
	}
	return limits
}

func recordItemLimitProblem(enforced, schema, guide map[string]int) string {
	var problems []string
	for name, limit := range enforced {
		switch {
		case limit == 0:
			problems = append(problems, name+" has no item limit in the refusal")
		case schema[name] != limit || guide[name] != limit:
			problems = append(problems, name+": the refusal takes "+strconv.Itoa(limit)+", the schema "+strconv.Itoa(schema[name])+", recording.md "+strconv.Itoa(guide[name]))
		}
	}
	sort.Strings(problems)
	return strings.Join(problems, "; ")
}
