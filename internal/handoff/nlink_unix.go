//go:build unix

package handoff

import (
	"io/fs"
	"syscall"
)

// noFollow は最後の名前がリンクなら開かない印(追記の時の差し替えの競合を断つ)。
const noFollow = syscall.O_NOFOLLOW

// linkCount はハードリンクの数。2 以上なら置き場の外の同じ中身を指している恐れがあるので扱わない。
func linkCount(info fs.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}
