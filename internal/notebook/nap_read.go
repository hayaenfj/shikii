package notebook

// The record `shikii nap` keeps of what the agent has read. A save names the sources it cites (conversations among them); that is the
// only sign shikii has that the agent went through a conversation `shikii nap` printed. .shikii/nap-read holds those ids, one per line,
// appended by every save and emptied by `shikii nap` when it prints (so ids named before a run printed are never counted by it).
// .reviewed cannot serve: the per-turn reflection puts it on a conversation before any nap prints it.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hayaenfj/shikii/internal/store"
)

// NapReadName is the file under the store's root that holds the ids of the sources the saves named since the last `shikii nap` printed.
const NapReadName = "nap-read"

// napReadMax stops the file from growing for ever when no `shikii nap` ever empties it: a save adds nothing once the file is this large,
// and an id that was not recorded only makes its conversation print again.
const napReadMax = 64 << 10

// NapShownName is the file where `shikii nap` lists what it printed: a first line (the window it started from, in Go quotes), then one
// line per conversation, "id offset hash cited" and, when the same line is in the record before it too, " copy" (older files have no fourth
// field). Only the ids are read here; cmd/shikii writes and parses the whole file.
const NapShownName = "nap-shown"

// NapShownMax is the most bytes of .shikii/nap-shown that are read.
const NapShownMax = 16 << 10

func napReadPath(root string) string { return filepath.Join(root, NapReadName) }

// NapStateUsable reports whether an open file may be read or rewritten as a nap state: an ordinary file with one name. A second name (a
// hard link) is another file of the person's, and writing to it in place would change that file too.
func NapStateUsable(f *os.File) bool {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Nlink == 1
}

// RecordNapRead appends the ids to .shikii/nap-read. It writes only to an ordinary file with one name (never through a symbolic link or
// into a hard-linked file), and it never fails a save: when the ids cannot be recorded they are not counted as read, and their
// conversations print again.
func (b Book) RecordNapRead(ids []string) {
	if len(ids) == 0 {
		return
	}
	f, err := os.OpenFile(napReadPath(b.Root), os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !NapStateUsable(f) || info.Size() >= napReadMax {
		return
	}
	_, _ = f.WriteString(strings.Join(ids, "\n") + "\n")
}

// NapReadIDs is the set of ids .shikii/nap-read holds: empty when the file is not there or is not an ordinary file with one name.
func NapReadIDs(root string) map[string]bool {
	ids := map[string]bool{}
	f, err := os.OpenFile(napReadPath(root), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ids
	}
	defer f.Close()
	if !NapStateUsable(f) {
		return ids
	}
	data, _ := io.ReadAll(io.LimitReader(f, napReadMax+64))
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) > 0 {
			ids[string(line)] = true
		}
	}
	return ids
}

// ClearNapRead empties .shikii/nap-read. A file that is not there is already empty. A file that is not an ordinary file with one name is
// left as it is and reported: ids can never be recorded in it, so nothing would ever count as read.
func ClearNapRead(root string) error {
	f, err := os.OpenFile(napReadPath(root), os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	if !NapStateUsable(f) {
		return errors.New("the record of what was read must be a regular file with one name")
	}
	return f.Truncate(0)
}

// napShownConversation is the conversation of the id when .shikii/nap-shown lists it (the last `shikii nap` printed it) and the record of
// conversations has a line for it. A save accepts such an id as a source although it is older than the newest 256 conversations and was
// never archived: the agent can say it read what the nap printed, so the nap goes on. An id the nap did not print is not found here, and
// neither is one whose conversation is no longer in the record. A file that is not an ordinary file with one name lists nothing.
func (b Book) napShownConversation(id string) (store.Feedback, bool, error) {
	f, err := os.OpenFile(filepath.Join(b.Root, NapShownName), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return store.Feedback{}, false, nil
	}
	defer f.Close()
	if !NapStateUsable(f) {
		return store.Feedback{}, false, nil
	}
	data, _ := io.ReadAll(io.LimitReader(f, NapShownMax))
	listed := false
	for i, line := range bytes.Split(data, []byte("\n")) {
		if fields := bytes.Split(line, []byte(" ")); i > 0 && len(fields) >= 3 && len(fields) <= 5 && string(fields[0]) == id {
			listed = true
			break
		}
	}
	if !listed {
		return store.Feedback{}, false, nil
	}
	return (store.Store{Dir: b.Root}).FeedbackByID(b.Scope, id)
}
