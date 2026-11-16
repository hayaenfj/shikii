package main

import (
	"bytes"
	"embed"
	"errors"
	"os"
	"reflect"
	"slices"

	"gopkg.in/yaml.v3"
)

// legacy/ keeps the built-in text of the needs-plan question from earlier versions. A project copied one of them when it was made.
//
//go:embed legacy/*.yaml
var legacySeedFS embed.FS

// legacyNeedsPlanSeeds lists those earlier built-in texts: 0.34.0 and the development build before the choice version.
var legacyNeedsPlanSeeds = []string{"legacy/needs-plan-0.34.0.yaml", "legacy/needs-plan-0.34.1-dev.yaml"}

// refreshNeedsPlanCopy writes the current built-in text over a project's copy when that copy is still an earlier built-in text: the same
// wording and the same comments, aside from the generated calibration block. A copy the person edited, or added a comment to, is kept as it
// is. The copy keeps its permission bits. It reports whether it wrote the copy.
func refreshNeedsPlanCopy(path string, current []byte) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("a decision copy must be an ordinary file to be refreshed")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	copied, copiedComments, err := decisionWording(data)
	if err != nil {
		return false, nil // a copy that does not read as a decision is the person's: keep it
	}
	matched := false
	for _, name := range legacyNeedsPlanSeeds {
		b, err := legacySeedFS.ReadFile(name)
		if err != nil {
			return false, err
		}
		old, oldComments, err := decisionWording(b)
		if err != nil {
			return false, err
		}
		if reflect.DeepEqual(copied, old) && slices.Equal(copiedComments, oldComments) {
			matched = true
			break
		}
	}
	if !matched || bytes.Equal(data, current) {
		return false, nil
	}
	return true, writeAtomicMode(path, current, info.Mode().Perm())
}

// decisionWording reads a decision file as data: the wording (the mapping without the generated calibration block) and its comments in document
// order. The comments are the person's too when they added one, so they are compared with the built-in text's comments.
func decisionWording(b []byte) (map[string]any, []string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, errors.New("a decision must be a mapping")
	}
	root := doc.Content[0]
	// the calibration pair carries the generated comment on its key: it is left out with the pair
	pairs := make([]*yaml.Node, 0, len(root.Content))
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "calibration" {
			pairs = append(pairs, root.Content[i], root.Content[i+1])
		}
	}
	root.Content = pairs
	var comments []string
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		for _, c := range []string{n.HeadComment, n.LineComment, n.FootComment} {
			if c != "" {
				comments = append(comments, c)
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&doc)
	var wording map[string]any
	if err := root.Decode(&wording); err != nil {
		return nil, nil, err
	}
	return wording, comments, nil
}
