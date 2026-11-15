package main

import (
	"path/filepath"
	"strings"
)

// homePathDisplay shows a path under the home folder of the agent folder (~/.shikii/agent) with ~ in its place.
func homePathDisplay(agent string) func(string) string {
	return displayUnder(filepath.Dir(filepath.Dir(agent)))
}

// displayUnder shows a path under home with ~ in its place (for the lines shikii prints), and any other path as it is.
func displayUnder(home string) func(string) string {
	return func(p string) string {
		if strings.HasPrefix(p, home+string(filepath.Separator)) {
			return "~" + strings.TrimPrefix(p, home)
		}
		return p
	}
}
