//go:build !unix

package handoff

import "io/fs"

const noFollow = 0

func linkCount(fs.FileInfo) uint64 { return 1 }
