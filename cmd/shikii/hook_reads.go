package main

import "github.com/hayaenfj/shikii/internal/notebook"

// eventPages is the validated-parsing view of the hook event in progress (nil outside one). One event reads the wiki in several
// steps (delivery, reflection, classification, the memory upgrade, the checks at Stop), and each step used to parse and verify
// every article again. Sharing the view parses an article once per event while it stays byte-identical: every step still opens and
// reads the file, compares all its bytes, and rechecks sources and watched premises (notebook.Book.ReadView). It is dropped when the
// event ends, so a long-running process (serve, mcp) never keeps it between requests.
var eventPages *notebook.Book

// beginEventReads starts the shared view for one hook event and returns the function that ends it.
func beginEventReads() func() {
	view := (notebook.Book{}).ReadView()
	eventPages = &view
	return func() { eventPages = nil }
}

// wikiBook is the Book that reads the wiki under root. Inside a hook event it carries the event's view; elsewhere it is a plain Book.
// Writers keep using a plain Book.
func wikiBook(root, scope string) notebook.Book {
	if eventPages == nil {
		return notebook.Book{Root: root, Scope: scope}
	}
	book := *eventPages
	book.Root, book.Scope = root, scope
	return book
}
