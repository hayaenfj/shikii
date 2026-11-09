package notebook

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func (b Book) pageBytes(p Page) ([]byte, error) { return b.renderPage(p, englishLabels) }

func (b Book) renderPage(p Page, l pageLabels) ([]byte, error) {
	b = b.scopedRead()
	front, err := yaml.Marshal(p)
	if err != nil {
		return nil, err
	}
	var links strings.Builder
	for _, source := range p.Sources {
		fmt.Fprintf(&links, "- [%s](../../../records/wiki-sources/%s/%s.json)\n", source.ID, b.storeName(), source.ID)
	}
	correction := ""
	if p.Correction != nil {
		correction = "\n\n" + l.correction + p.Correction.Quote + "\n\n" + l.action + p.Correction.Action
	}
	article := ""
	if p.ChangeReason != "" {
		article = "\n\n" + l.reason + p.ChangeReason
	}
	if p.Body != "" {
		article += "\n\n" + p.Body
	}
	if p.Rationale != "" {
		article += "\n\n## Rationale\n\n" + p.Rationale
	}
	if len(p.Related) > 0 {
		article += "\n\n" + l.related + "\n\n"
		for _, id := range p.Related {
			article += fmt.Sprintf("- [%s](%s.md)\n", id, id)
		}
	}
	return []byte("---\n" + string(front) + "---\n\n# " + p.Title + "\n\n" + p.Lesson + "\n\n" + l.applies + p.AppliesTo + article + correction + "\n\n" + l.sources + "\n\n" + links.String() + "\n" + l.note + "\n"), nil
}

// matchesPage says whether data is exactly what shikii writes for p (with the current labels, or with the labels of before 0.9.2).
func (b Book) matchesPage(data []byte, p Page) bool {
	b = b.scopedRead()
	for _, l := range []pageLabels{englishLabels, legacyLabels} {
		if expected, err := b.renderPage(p, l); err == nil && bytes.Equal(data, expected) {
			return true
		}
	}
	return false
}

func parsePage(data []byte) (Page, error) {
	var p Page
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return p, errors.New("not a generated wiki lesson")
	}
	end := bytes.Index(data[4:], []byte("\n---\n"))
	if end < 0 {
		return p, errors.New("wiki lesson has no metadata boundary")
	}
	if err := yaml.Unmarshal(data[4:4+end], &p); err != nil {
		return p, err
	}
	if !idPattern.MatchString(p.ID) || (p.Status != "hypothesis" && p.Status != "deleted") || p.Title == "" || p.Lesson == "" || p.AppliesTo == "" || len(p.Sources) == 0 || len(p.Body) > 16<<10 {
		return p, errors.New("invalid wiki lesson metadata")
	}
	if p.Previous != "" && !idPattern.MatchString(p.Previous) {
		return p, errors.New("invalid previous wiki revision")
	}
	if len(p.Rationale) > 1600 || len(p.Premises) > 4 {
		return p, errors.New("invalid rationale or premise metadata")
	}
	if err := validateLearning(p); err != nil {
		return p, err
	}
	if err := validatePassages(p.Body, p.Passages); err != nil {
		return p, err
	}
	seenPremises := map[string]bool{}
	for _, ref := range p.Premises {
		_, err := hex.DecodeString(ref.Hash)
		if !premisePath(ref.Path) || seenPremises[ref.Path] || len(ref.Hash) != 64 || err != nil {
			return p, errors.New("invalid premise reference")
		}
		seenPremises[ref.Path] = true
	}
	for _, id := range p.Related {
		if !idPattern.MatchString(id) || id == p.ID {
			return p, errors.New("invalid related wiki page")
		}
	}
	return p, nil
}

func revision(p Page) string {
	p.Recheck = nil
	p.Revision = ""
	b, _ := json.Marshal(p)
	return digest(b)[:16]
}
