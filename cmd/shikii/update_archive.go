package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

func updateArchive(data []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("cannot extract the release")
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, 256<<20))
	result := map[string][]byte{}
	seen := map[string]bool{}
	var total int64
	for count := 0; ; count++ {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("cannot read the release contents")
		}
		if count >= 128 || h.Size < 0 || h.Size > 64<<20 {
			return nil, errors.New("the extracted release exceeds the size limit")
		}
		total += h.Size + 512
		if total > 256<<20 {
			return nil, errors.New("the extracted release exceeds the size limit")
		}
		if filepath.Base(h.Name) != h.Name || h.Name == "." || h.Name == ".." || strings.ContainsAny(h.Name, "/\\") || seen[h.Name] || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA) {
			return nil, errors.New("the release contains invalid paths, duplicates or links")
		}
		seen[h.Name] = true
		if !containsString(updateFiles, h.Name) {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil || len(body) == 0 {
			return nil, errors.New("cannot read the required files in the release")
		}
		result[h.Name] = body
	}
	for _, name := range updateFiles {
		if len(result[name]) == 0 {
			return nil, fmt.Errorf("the release has no %s", name)
		}
	}
	return result, nil
}
