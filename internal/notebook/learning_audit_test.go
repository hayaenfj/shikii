package notebook

import (
	"strings"
	"testing"
	"time"
)

func growthIndependentFixture(t *testing.T) (Book, string) {
	t.Helper()
	b := Book{Root: t.TempDir(), Scope: "/synthetic/growth-independent"}
	s, err := b.Capture(Observation{Kind: "test", Title: "Synthetic check", Content: "Synthetic checks only", Reference: "synthetic-only", Version: "v1", ObservedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	return b, s.ID
}

func growthIndependentPage(t *testing.T, b Book, source, title, body string) Page {
	t.Helper()
	r, err := b.Ingest(Draft{Title: title, Lesson: "Synthetic guidance", AppliesTo: "Synthetic workflow", Kind: "fact", Scene: "Synthetic work", Body: &body, Sources: []string{source}})
	if err != nil {
		t.Fatal(err)
	}
	return *r.Page
}

func TestIndependentGrowthOverlappingConnection(t *testing.T) {
	b, source := growthIndependentFixture(t)
	a := growthIndependentPage(t, b, source, "First synthetic experience", "BANANANA")
	c := growthIndependentPage(t, b, source, "Second synthetic experience", "COCOCO")
	connections := []ExperienceConnection{{ID: a.ID, Revision: a.Revision, Quote: "ANANA", Conditions: "First synthetic condition", Reason: "First synthetic reason", Relation: "supports"}, {ID: c.ID, Revision: c.Revision, Quote: "COCO", Conditions: "Second synthetic condition", Reason: "Second synthetic reason", Relation: "counterexample"}}
	for i, p := range []Page{a, c} {
		q := connections[i].Quote
		if strings.Count(p.Body, q) != 1 || strings.Index(p.Body, q) == strings.LastIndex(p.Body, q) {
			t.Fatal("invalid overlapping fixture")
		}
	}
	r, err := b.Ingest(Draft{Title: "Synthetic overlapping synthesis", Lesson: "Synthetic synthesis", AppliesTo: "Synthetic workflow", Kind: "fact", Scene: "Synthetic work", Sources: []string{source}, Learning: &Learning{Connections: connections}})
	if err == nil {
		t.Fatalf("ambiguous overlapping connections were saved: retained_connections=%d", len(r.Page.Learning.Connections))
	}
}

func TestIndependentGrowthOverlappingOrganization(t *testing.T) {
	b, source := growthIndependentFixture(t)
	p := growthIndependentPage(t, b, source, "Synthetic organization source", "BANANANA")
	body := "ANANA"
	plan, err := b.Organize(Organization{Reason: "Preserve one exact synthetic passage", Sources: []OrganizationSource{{ID: p.ID, Revision: p.Revision, Lesson: "Read the related synthetic passage"}}, Targets: []Draft{{Title: "Synthetic overlap destination", Lesson: "Synthetic destination", AppliesTo: "Synthetic workflow", Kind: "fact", Scene: "Synthetic work", Body: &body}}, Moves: []KnowledgeMove{{From: p.ID, Quote: body, To: "Synthetic overlap destination"}}})
	if err == nil {
		t.Fatalf("ambiguous overlapping move was accepted: creates=%d patches=%d", len(plan.Creates), len(plan.Patches))
	}
}

func TestIndependentGrowthUniqueControl(t *testing.T) {
	b, source := growthIndependentFixture(t)
	quote := "One complete unique synthetic passage."
	p := growthIndependentPage(t, b, source, "Synthetic unique source", quote)
	plan, err := b.Organize(Organization{Reason: "Preserve one unique synthetic passage", Sources: []OrganizationSource{{ID: p.ID, Revision: p.Revision, Lesson: "Read the related synthetic passage"}}, Targets: []Draft{{Title: "Synthetic unique destination", Lesson: "Synthetic destination", AppliesTo: "Synthetic workflow", Kind: "fact", Scene: "Synthetic work", Body: &quote}}, Moves: []KnowledgeMove{{From: p.ID, Quote: quote, To: "Synthetic unique destination"}}})
	if err != nil || len(plan.Creates) != 1 || len(plan.Patches) != 1 {
		t.Fatal("the valid control was rejected", plan, err)
	}
}

func TestIndependentGrowthMovesUseOriginalRanges(t *testing.T) {
	b, source := growthIndependentFixture(t)
	first, second := "Move this complete instruction.", "lesson"
	p := growthIndependentPage(t, b, source, "Synthetic sequential move source", first+"\n\n"+second)
	plan, err := b.Organize(Organization{Reason: "Move two disjoint original passages", Sources: []OrganizationSource{{ID: p.ID, Revision: p.Revision, Lesson: "Read both related passages"}}, Targets: []Draft{{Title: "Synthetic first destination", Lesson: "First destination", AppliesTo: "Synthetic workflow", Kind: "fact", Scene: "Synthetic work", Body: &first}, {Title: "Synthetic second destination", Lesson: "Second destination", AppliesTo: "Synthetic workflow", Kind: "fact", Scene: "Synthetic work", Body: &second}}, Moves: []KnowledgeMove{{From: p.ID, Quote: first, To: "Synthetic first destination"}, {From: p.ID, Quote: second, To: "Synthetic second destination"}}})
	if err != nil {
		t.Fatal("valid disjoint moves were rejected", err)
	}
	for _, create := range plan.Creates {
		if _, err := b.Ingest(create.Draft); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := b.Patch(plan.Patches[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(saved.Page.Body, first) || strings.HasSuffix(saved.Page.Body, "\n\nlesson") || !strings.Contains(saved.Page.Body, "lesson:"+plan.Creates[0].Page.ID) {
		t.Fatalf("a later move replaced generated link text instead of its original source passage: %q", saved.Page.Body)
	}
}
