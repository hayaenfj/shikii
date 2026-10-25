package main

// Work records (.shikii/handoff/, 0.8.1): the place for work files that are rewritten along the way (audit reports, release steps and logs, diffs, images).
// Lessons go in the wiki, evidence as it stood when checked goes in the wiki's sources, and things rewritten as the work goes on go here.
// The CLI is shikii handoff, MCP is the handoff_* operations of shikii_wiki, and the page shows them under "Work records" in the knowledge section (read only).

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/hayaenfj/shikii/internal/handoff"
)

func runHandoff(root string, req notebookRequest) (any, error) {
	b := handoff.Book{Store: root}
	switch req.Operation {
	case "handoff_list":
		list, err := b.List()
		if err != nil {
			return nil, err
		}
		return map[string]any{"folder": ".shikii/handoff/", "files": list}, nil
	case "handoff_read":
		return b.Read(req.Path)
	case "handoff_write", "handoff_append":
		return b.Write(req.Path, req.Content, req.Operation == "handoff_append")
	}
	return nil, errors.New("usage: handoff_list / handoff_read / handoff_write / handoff_append")
}

// cmdHandoff is shikii handoff list | read <path> | write <path> | append <path> (write and append take the body on standard input).
func cmdHandoff(args []string) error {
	usage := errors.New("usage: shikii handoff list | read <path> | write <path> | append <path> (write and append take the body on standard input; the path is relative to .shikii/handoff/)")
	if len(args) == 0 {
		return usage
	}
	root, _, err := currentProjectStore()
	if err != nil {
		return err
	}
	req := notebookRequest{Operation: "handoff_" + args[0]}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return usage
		}
		list, err := (handoff.Book{Store: root}).List()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Fprintln(stdout, "No work records yet (.shikii/handoff/)")
			return nil
		}
		kind := map[string]string{"text": "text", "image": "image", "other": "other"}
		for _, e := range list {
			fmt.Fprintf(stdout, "%s\t%s\t%d\t%s\n", e.Modified.Local().Format("2006-01-02 15:04"), kind[e.Kind], e.Size, e.Path)
		}
		return nil
	case "read":
		if len(args) != 2 {
			return usage
		}
		f, err := (handoff.Book{Store: root}).Read(args[1])
		if err != nil {
			return err
		}
		if f.Content == "" && f.Note != "" {
			fmt.Fprintf(stdout, "%s (%d bytes): %s\n", f.Path, f.Size, f.Note)
			return nil
		}
		_, err = io.WriteString(stdout, f.Content)
		return err
	case "write", "append":
		if len(args) != 2 {
			return usage
		}
		data, err := io.ReadAll(io.LimitReader(stdin, (1<<20)+1))
		if err != nil {
			return err
		}
		if len(data) > 1<<20 {
			return errors.New("standard input is over 1 MiB. Split it into pieces of 1 MiB and append them")
		}
		req.Path, req.Content = args[1], string(data)
		res, err := runHandoff(root, req)
		if err != nil {
			return err
		}
		e := res.(handoff.Entry)
		fmt.Fprintf(stdout, "wrote: .shikii/handoff/%s (%d bytes)\n", e.Path, e.Size)
		return nil
	}
	return usage
}

// handleHandoffFile is the endpoint the page uses to read one work record (GET ?path=). Images are served as images and text as text/plain (never rendered as HTML).
func (s *server) handleHandoffFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, errors.New("the work record endpoint only accepts GET"))
		return
	}
	f, e, err := (handoff.Book{Store: s.o.storeDir}).Open(r.URL.Query().Get("path"))
	if err != nil {
		code := 404
		if _, cerr := handoff.Clean(r.URL.Query().Get("path")); cerr != nil { // a bad path (outside the folder, hidden name, ...) is 400; a missing file is 404
			code = 400
		}
		writeErr(w, code, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; sandbox")
	h.Set("Cache-Control", "no-store")
	switch e.Kind {
	case "image":
		h.Set("Content-Type", handoff.ImageTypes[strings.ToLower(path.Ext(e.Path))])
	case "text":
		h.Set("Content-Type", "text/plain; charset=utf-8")
	default:
		h.Set("Content-Type", "application/octet-stream")
		h.Set("Content-Disposition", "attachment")
	}
	h.Set("Content-Length", fmt.Sprint(e.Size))
	_, _ = io.Copy(w, io.LimitReader(f, e.Size))
}
