package main

import (
	"strings"
	"testing"
)

// A go-ahead reply typed in project b must not be joined onto the request of project a: the record of project b would carry project a's request.
func TestAckFromAnotherProjectIsNotJoined(t *testing.T) {
	snap := &hookSession{Request: "A request in project a", RequestText: "A request in project a", Scope: "/projects/a"}
	request, text := groupedRequest(snap, "\u3067\u306f\u958b\u59cb\u3057\u307e\u3057\u3087\u3046", "\u3067\u306f\u958b\u59cb\u3057\u307e\u3057\u3087\u3046", "/projects/b", true)
	if strings.Contains(request, "project a") || strings.Contains(text, "project a") {
		t.Errorf("a request from project a is joined into a record of project b: request=%q text=%q", request, text)
	}
}
