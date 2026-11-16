package main

import (
	"bytes"
	"embed"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/hayaenfj/shikii/internal/spec"
	"gopkg.in/yaml.v3"
)

// legacy/ keeps the built-in text of the needs-plan question from earlier versions. A project copied one of them when it was made.
//
//go:embed legacy/*.yaml
var legacySeedFS embed.FS

// legacyNeedsPlanSeeds lists those earlier built-in texts: 0.34.0 and the development build before the choice version.
var legacyNeedsPlanSeeds = []string{"legacy/needs-plan-0.34.0.yaml", "legacy/needs-plan-0.34.1-dev.yaml"}

// refreshBeforeWrite runs after a copy has been read and matched, just before it is written. It is a seam for tests, which make a
// person's edit in that window; the built-in value does nothing.
var refreshBeforeWrite = func() {}

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
	refreshBeforeWrite()
	// The person may have edited the copy after it was read and matched: keep that edit. Reading it again narrows the window to the time
	// between this read and the rename; no file lock can stop an editor that does not take one.
	again, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(again, data) {
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
	// Comments are read in document order before the calibration pair is left out. Only the generated note on its key is ignored: a comment
	// the person added to the calibration block is theirs and keeps the copy.
	var comments []string
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		for _, c := range []string{n.HeadComment, n.LineComment, n.FootComment} {
			if c != "" && !generatedCalibrationNote(c) {
				comments = append(comments, c)
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&doc)
	pairs := make([]*yaml.Node, 0, len(root.Content))
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "calibration" {
			pairs = append(pairs, root.Content[i], root.Content[i+1])
		}
	}
	root.Content = pairs
	var wording map[string]any
	if err := root.Decode(&wording); err != nil {
		return nil, nil, err
	}
	return wording, comments, nil
}

// generatedCalibrationNote says whether a comment is a note that `shikii calibrate --write` puts on the generated calibration key: the current note
// or the one earlier builds wrote.
func generatedCalibrationNote(c string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c), "#"))
	return t == spec.CalibrationNote || t == spec.CalibrationNoteEarlierJapanese
}
