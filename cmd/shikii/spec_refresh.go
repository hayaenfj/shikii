package main

import (
	"bytes"
	"embed"
	"errors"
	"os"
	"reflect"

	"gopkg.in/yaml.v3"
)

// legacy/ keeps the built-in text of the needs-plan question from earlier versions. A project copied one of them when it was made.
//
//go:embed legacy/*.yaml
var legacySeedFS embed.FS

// legacyNeedsPlanSeeds lists those earlier built-in texts: 0.34.0 and the development build before the choice version.
var legacyNeedsPlanSeeds = []string{"legacy/needs-plan-0.34.0.yaml", "legacy/needs-plan-0.34.1-dev.yaml"}

// refreshNeedsPlanCopy writes the current built-in text over a project's copy when that copy is still an earlier built-in text: the comments
// aside, only the generated calibration block may differ. A copy the person edited is kept as it is. It reports whether it wrote the copy.
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
	copied, err := seedDocument(data)
	if err != nil {
		return false, nil // a copy that does not read as a decision is the person's: keep it
	}
	matched := false
	for _, name := range legacyNeedsPlanSeeds {
		b, err := legacySeedFS.ReadFile(name)
		if err != nil {
			return false, err
		}
		old, err := seedDocument(b)
		if err != nil {
			return false, err
		}
		if reflect.DeepEqual(copied, old) {
			matched = true
			break
		}
	}
	if !matched || bytes.Equal(data, current) {
		return false, nil
	}
	return true, writeAtomic(path, current)
}

// seedDocument reads a decision file as data: the generated calibration block and the comments are not part of the wording.
func seedDocument(b []byte) (map[string]any, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	delete(doc, "calibration")
	return doc, nil
}
