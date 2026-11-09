package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hayaenfj/shikii/internal/spec"
)

// adoptWording writes the winner as a new version and calibrates on the spot. It marks the result as adopted (saving json / md is the caller's job).
func adoptWording(res *tuneResult, ef endpointFlags, storeDir, labeledPath string, reports ...io.Writer) (string, error) {
	base, err := spec.Load(res.SpecPath)
	if err != nil {
		return "", err
	}
	if base.Hash() != res.SpecHash {
		return "", fmt.Errorf("the spec changed since this run (hash %s → %s); run tune again", res.SpecHash, base.Hash())
	}
	if res.WinnerVariant == nil {
		return "", errors.New("nothing to adopt")
	}
	var before, after candidateResult
	for _, c := range res.Candidates {
		if c.Name == "current" {
			before = c
		}
		if c.Name == res.Winner {
			after = c
		}
	}
	newPath, err := writeTunedVersion(base, candidate{res.Winner, base.WithVariant(*res.WinnerVariant)}, before, after, res.Truths)
	if err != nil {
		return "", err
	}
	lab := labeledPath
	if lab == "" {
		if base.Eval.Labeled != "" {
			lab = base.Eval.Labeled
		} else {
			lab = "none"
		}
	}
	// Adopting and calibrating go together: wording that cannot get a line does not become a version (if kept, serve would use a new version with no calibration and act would silently disappear)
	if err := runCalibrate(newPath, calibrateOpts{output: reportWriter(reports...), ef: ef, storeDir: storeDir, labeled: lab, bins: 10, write: true}); err != nil {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("not adopted: calibration of the new wording failed, so %s was not kept: %w", filepath.Base(newPath), err)
	}
	if n, err := spec.Load(newPath); err != nil || len(n.Calibration) == 0 {
		_ = os.Remove(newPath)
		return "", fmt.Errorf("not adopted: the new wording could not be calibrated (precision at act below the threshold), so %s was not kept", filepath.Base(newPath))
	}
	res.Adopted, res.AdoptedPath = true, newPath
	return newPath, nil
}
