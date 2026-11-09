package store

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"testing"
)

func TestJSONLAppendAfterUnfinishedTail(t *testing.T) {
	for _, kind := range []string{"record", "labels", "labels-once"} {
		for _, tail := range []string{"partial", "complete-without-newline"} {
			t.Run(kind+"/"+tail, func(t *testing.T) {
				st := Store{Dir: t.TempDir()}
				path := st.recordsPath("synthetic")
				appendItem := func(id string) error {
					return st.AppendRecord(Record{TraceID: id, SpecID: "synthetic"})
				}
				if kind != "record" {
					path = st.labelsPath("synthetic")
					appendItem = func(id string) error {
						ls := []Label{{TraceID: id, SpecID: "synthetic", SpecHash: "hash", Model: "model", Truth: "yes", Source: "outcome"}}
						if kind == "labels-once" {
							return st.AppendLabelsOnce(ls)
						}
						return st.AppendLabels(ls)
					}
				}
				if err := appendItem("old"); err != nil {
					t.Fatal(err)
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				broken := append(bytes.Clone(before), []byte(`{"trace_id":"unfinished`)...)
				if tail == "complete-without-newline" {
					broken = bytes.TrimSuffix(before, []byte("\n"))
				}
				if err := os.WriteFile(path, broken, 0600); err != nil {
					t.Fatal(err)
				}
				if err := appendItem("new"); err != nil {
					t.Fatal(err)
				}
				if kind == "record" {
					for _, id := range []string{"old", "new"} {
						if _, err := st.FindRecord("synthetic", id); err != nil {
							t.Error(err)
						}
					}
				} else {
					first, firstErr := st.Labels("synthetic", "hash", "model")
					if firstErr != nil || len(first) != 2 {
						t.Fatalf("first retry is readable: %v %v", first, firstErr)
					}
					if kind == "labels-once" {
						if err := appendItem("new"); err != nil {
							t.Fatal(err)
						}
					}
					ls, err := st.Labels("synthetic", "hash", "model")
					if err != nil || len(ls) != 2 || ls[0].TraceID != "old" || ls[1].TraceID != "new" {
						t.Fatalf("old and first successful retry remain readable, without duplicate labels: %v %v", ls, err)
					}
				}
			})
		}
	}
}

func TestJSONLShortWriteRollbackAndRetry(t *testing.T) {
	if kind := os.Getenv("SHIKII_TEST_JSONL_SHORT_KIND"); kind != "" {
		dir := os.Getenv("SHIKII_TEST_JSONL_SHORT_DIR")
		limit, err := strconv.ParseUint(os.Getenv("SHIKII_TEST_JSONL_SHORT_LIMIT"), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		signal.Ignore(syscall.SIGXFSZ)
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: limit, Max: limit}); err != nil {
			t.Fatal(err)
		}
		err = appendRecoveryFixture(Store{Dir: dir}, kind, "new")
		if !errors.Is(err, syscall.EFBIG) && !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("short write must return a file-size error: %v", err)
		}
		return
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("file-size limit fixture is checked on Darwin and Linux")
	}
	for _, kind := range []string{"record", "labels", "labels-once"} {
		for _, recovery := range []string{"rollback", "truncate-refused"} {
			t.Run(kind+"/"+recovery, func(t *testing.T) {
				st := Store{Dir: t.TempDir()}
				if err := appendRecoveryFixture(st, kind, "old"); err != nil {
					t.Fatal(err)
				}
				path := st.recordsPath("synthetic")
				if kind != "record" {
					path = st.labelsPath("synthetic")
				}
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				limit := uint64(len(before) + 80)
				if kind != "record" {
					limit += 520
				}
				if recovery == "truncate-refused" {
					limit = 80
				}
				binary, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(binary, "-test.run=^TestJSONLShortWriteRollbackAndRetry$")
				cmd.Env = append(os.Environ(), "SHIKII_TEST_JSONL_SHORT_KIND="+kind, "SHIKII_TEST_JSONL_SHORT_DIR="+st.Dir, "SHIKII_TEST_JSONL_SHORT_LIMIT="+strconv.FormatUint(limit, 10))
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("child: %v %s", err, output)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.HasPrefix(after, before) {
					t.Fatalf("old data must survive the failed append: %v", err)
				}
				if recovery == "rollback" && !bytes.Equal(before, after) {
					t.Errorf("failed append must roll back only its own bytes: before=%d after=%d err=%v", len(before), len(after), err)
				}
				if err := appendRecoveryFixture(st, kind, "new"); err != nil {
					t.Fatal(err)
				}
				if kind == "record" {
					if _, err := st.FindRecord("synthetic", "new"); err != nil {
						t.Fatal(err)
					}
				} else {
					first, firstErr := st.Labels("synthetic", "hash", "model")
					if firstErr != nil || len(first) != 4 || first[2].TraceID != "new" || first[3].TraceID != "new" {
						t.Fatalf("first successful retry must expose the full batch: %v %v", first, firstErr)
					}
					if kind == "labels-once" {
						if err := appendRecoveryFixture(st, kind, "new"); err != nil {
							t.Fatal(err)
						}
					}
					ls, err := st.Labels("synthetic", "hash", "model")
					if err != nil || len(ls) != 4 {
						t.Fatalf("old and full first retry batch remain readable: %d %v", len(ls), err)
					}
				}
			})
		}
	}
}

func appendRecoveryFixture(st Store, kind, id string) error {
	size := 256
	if id == "new" {
		size = 2048
	}
	if kind == "record" {
		return st.AppendRecord(Record{TraceID: id, SpecID: "synthetic", Inputs: map[string]string{"message": string(bytes.Repeat([]byte("x"), size))}})
	}
	ls := []Label{}
	for _, rung := range []string{"minimal", "context"} {
		payload := 256
		if rung == "context" {
			payload = size
		}
		ls = append(ls, Label{TraceID: id, SpecID: "synthetic", SpecHash: "hash", Model: "model", Truth: "yes", Source: "outcome", Rung: rung, Inputs: map[string]string{"message": string(bytes.Repeat([]byte("x"), payload))}})
	}
	// Distinct truths let AppendLabelsOnce retain both batch members independently.
	ls[1].Truth = "no"
	if kind == "labels-once" {
		return st.AppendLabelsOnce(ls)
	}
	return st.AppendLabels(ls)
}

func TestJSONLAppendWritersShareTheRecoveryLock(t *testing.T) {
	for _, kind := range []string{"record", "labels", "labels-once"} {
		t.Run(kind, func(t *testing.T) {
			st := Store{Dir: t.TempDir()}
			open := openInRoot
			defer func() { openInRoot = open }()
			checked := false
			openInRoot = func(root *os.Root, name string, flags int, mode os.FileMode) (*os.File, error) {
				if flags&os.O_APPEND != 0 {
					checked = true
					guard, err := root.OpenFile(name+".lock", os.O_CREATE|os.O_RDWR, 0600)
					if err != nil {
						t.Fatal(err)
					}
					err = syscall.Flock(int(guard.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
					if err == nil {
						_ = syscall.Flock(int(guard.Fd()), syscall.LOCK_UN)
					}
					guard.Close()
					if !errors.Is(err, syscall.EWOULDBLOCK) {
						t.Errorf("the recovery lock must exclude a peer before opening the data: %v", err)
					}
				}
				return open(root, name, flags, mode)
			}
			if err := appendRecoveryFixture(st, kind, "old"); err != nil {
				t.Fatal(err)
			}
			if !checked {
				t.Fatal("no append was checked")
			}
		})
	}
}
