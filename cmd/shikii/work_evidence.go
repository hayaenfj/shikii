package main

import (
	"strings"

	"github.com/hayaenfj/shikii/internal/notebook"
)

func completeWorkSourceReference(book notebook.Book, ref string) bool {
	return strings.HasPrefix(ref, "source:") && prepareWorkSource(book, strings.TrimPrefix(ref, "source:")).Complete
}
