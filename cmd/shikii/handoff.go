package main

// 作業記録(.shikii/handoff/、0.8.1): 途中で書き換える作業ファイル(監査の報告、リリースの手順と記録、差分、画像)の置き場。
// 教訓は wiki、確かめた時点の証拠は wiki の原資料、書き換えながら進める物はここ。
// CLI は shikii handoff、MCP は shikii_wiki の handoff_* の操作、画面は知識の欄の「作業記録」(読むだけ)。

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
	return nil, errors.New("使い方: handoff_list / handoff_read / handoff_write / handoff_append")
}

// cmdHandoff は shikii handoff list | read <path> | write <path> | append <path>(write と append は本文を標準入力)。
func cmdHandoff(args []string) error {
	usage := errors.New("使い方: shikii handoff list | read <パス> | write <パス> | append <パス>（write と append は本文を標準入力。パスは .shikii/handoff/ からの相対）")
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
			fmt.Fprintln(stdout, "作業記録はまだありません(.shikii/handoff/)")
			return nil
		}
		kind := map[string]string{"text": "文字", "image": "画像", "other": "その他"}
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
			fmt.Fprintf(stdout, "%s(%d バイト): %s\n", f.Path, f.Size, f.Note)
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
			return errors.New("標準入力が 1 MiB を超えます。1 MiB ずつに分けて append します")
		}
		req.Path, req.Content = args[1], string(data)
		res, err := runHandoff(root, req)
		if err != nil {
			return err
		}
		e := res.(handoff.Entry)
		fmt.Fprintf(stdout, "書きました: .shikii/handoff/%s(%d バイト)\n", e.Path, e.Size)
		return nil
	}
	return usage
}

// handleHandoffFile は画面が作業記録の 1 件を読む口(GET ?path=)。画像は画像として、文字は text/plain で配る(HTML を描かせない)。
func (s *server) handleHandoffFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, 405, errors.New("作業記録の口は GET だけです"))
		return
	}
	f, e, err := (handoff.Book{Store: s.o.storeDir}).Open(r.URL.Query().Get("path"))
	if err != nil {
		code := 404
		if strings.Contains(err.Error(), "相対") || strings.Contains(err.Error(), "隠し名") {
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
