package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// R6 (A1/A4): a hard-linked config.toml is rewritten in place (O_TRUNC, then write). When the write fails after the truncation (here the
// disk fills up), the person's file is left cut short: neither the old content nor the new.
func TestRe2HardLinkInPlaceFailureKeepsTheFile(t *testing.T) {
	mnt := os.Getenv("RE2_MNT")
	if mnt == "" {
		t.Skip("RE2_MNT")
	}
	const bs = 4096
	for k := 8; k <= 14; k++ {
		dir := filepath.Join(mnt, fmt.Sprintf("k%d", k))
		os.RemoveAll(dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		// old content: 8 blocks less 10 bytes, so the new one needs a 9th block
		old := "model = \"m\"\n" + strings.Repeat("# x\n", (8*bs-10-12)/4)
		path := filepath.Join(dir, "config.toml")
		if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(path, filepath.Join(dir, "dotfiles.toml")); err != nil {
			t.Fatal(err)
		}
		filler := filepath.Join(dir, "filler")
		f, _ := os.Create(filler)
		chunk := make([]byte, bs)
		n := 0
		for {
			if _, err := f.Write(chunk); err != nil {
				break
			}
			n++
		}
		f.Close()
		var st syscall.Statfs_t
		syscall.Statfs(mnt, &st)
		_ = os.Truncate(filler, int64((n-k)*bs))
		syscall.Statfs(mnt, &st)
		did, err := addCodexServer(path, []string{"mcp"})
		got, _ := os.ReadFile(path)
		t.Logf("k=%d free blocks=%d did=%v err=%v len(old)=%d len(got)=%d", k, st.Bavail, did, err, len(old), len(got))
		os.Remove(filler)
		if err != nil && string(got) != old {
			t.Fatalf("A1/A4: a failed write changed the file: %d bytes left of %d, has shikii=%v, err=%v", len(got), len(old), strings.Contains(string(got), "mcp_servers.shikii"), err)
		}
		os.RemoveAll(dir)
	}
}
