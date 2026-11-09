package notebook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func index(pages []Page) []byte {
	var out strings.Builder
	out.WriteString("# Wiki grown from conversations\n\nAn index of lessons the AI summarized from source records. Each lesson is a hypothesis, not permission to act or a correct answer.\n\n")
	replaced := map[string]bool{}
	for _, p := range pages {
		for _, id := range p.Replaces {
			replaced[id] = true
		}
	}
	for _, p := range pages {
		status := p.Status
		if p.Status != "deleted" && replaced[p.ID] {
			status = "updated by a later lesson"
		}
		title, _ := json.Marshal(p.Title)
		fmt.Fprintf(&out, "- [%s](%s.md) — %s / %d sources / %s\n", p.ID, p.ID, string(title), len(p.Sources), status)
	}
	out.WriteString("\nChange history: [log.md](log.md)\n")
	return []byte(out.String())
}

func logEvent(dir, key string) error {
	path := filepath.Join(dir, "log.md")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	_, _ = f.Seek(max(int64(0), info.Size()-(1<<20)), io.SeekStart)
	tail, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return err
	}
	if !bytes.Contains(tail, []byte(" | "+key+"\n")) {
		_, err = fmt.Fprintf(f, "- %s | %s\n", time.Now().UTC().Format(time.RFC3339), key)
	}
	return err
}
