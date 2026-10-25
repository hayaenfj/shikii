//go:build unix

package handoff

import (
	"io/fs"
	"syscall"
)

// noFollow is the flag that refuses to open the last name if it is a link (cuts off the swap race when appending).
const noFollow = syscall.O_NOFOLLOW

// linkCount is the number of hard links. If 2 or more, it may point to the same contents outside the place, so it is not handled.
func linkCount(info fs.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}
