package main

import (
	"path/filepath"
	"strings"
)

func homePathDisplay(agent string) func(string) string {
	home := filepath.Dir(filepath.Dir(agent))
	return func(p string) string {
		if strings.HasPrefix(p, home+string(filepath.Separator)) {
			return "~" + strings.TrimPrefix(p, home)
		}
		return p
	}
}
