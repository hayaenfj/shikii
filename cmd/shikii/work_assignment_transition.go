package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
)

func assignmentRelated(a, b assignmentContext) bool {
	if a.Group == b.Group {
		return true
	}
	return notebook.TopicMatches(a.Purpose+" "+a.Question, b.Purpose+" "+b.Question)
}

func assignmentTransition(from, to string) bool {
	if to == "observed" {
		return from == "active" || from == "stale"
	}
	if from == "active" || from == "stale" {
		return to == "completed" || to == "failed" || to == "cancelled"
	}
	return from == "completed" && (to == "integrated" || to == "reverted" || to == "cancelled") || from == "failed" && to == "cancelled" || from == "integrated" && to == "reverted"
}
