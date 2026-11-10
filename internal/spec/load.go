package spec

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads the YAML and validates it.
func Load(path string) (*Spec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Spec
	if err := yaml.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	s.path = path
	s.hadVariants = len(s.Variants) > 0
	s.loadedCalibration = copyCalibration(s.Calibration)
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}
