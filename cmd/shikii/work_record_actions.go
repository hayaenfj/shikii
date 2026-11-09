package main

import (
	"strconv"
)

func (b *workRecordBatch) recordActions() bool {
	root, scope, req, w, token, book := b.root, b.scope, b.req, b.w, b.token, b.book
	resolveSources, save, commit, add, halt, result := b.resolve, b.save, b.commit, b.add, b.halt, b.result
	if req.Reading != nil {
		item, e := recordWorkReading(book, w, *req.Reading, resolveSources, save, commit)
		if e != nil {
			item.Error = e.Error()
		}
		add(item)
		if halt() {
			return true
		}
	}
	if req.Trial != nil {
		item, e := recordWorkTrial(book, w, *req.Trial, resolveSources, save, commit)
		if e != nil {
			item.Error = e.Error()
		}
		add(item)
		if halt() {
			return true
		}
	}
	if req.Comparison != nil {
		item, view, e := recordWorkComparison(book, w, *req.Comparison, resolveSources, save, commit)
		if e != nil {
			item.Error = e.Error()
		}
		result.Comparison = view
		add(item)
		if halt() {
			return true
		}
	}
	for i, d := range req.Discoveries {
		item, e := recordWorkDiscovery(book, w, token, d, save, commit)
		item.Ref = "discovery" + strconv.Itoa(i+1)
		if e != nil {
			item.Error = e.Error()
		}
		add(item)
		if halt() {
			return true
		}
	}
	for i, o := range req.Outcomes {
		item, e := recordWorkOutcome(book, w, o, resolveSources, save, commit)
		item.Ref = "outcome" + strconv.Itoa(i+1)
		if e != nil {
			item.Error = e.Error()
		}
		add(item)
		if halt() {
			return true
		}
	}
	if req.Agreed != nil || req.Open != nil || req.ContinuesPrevious != nil {
		item, value, e := recordWorkPlan(root, scope, token, w, req, save)
		if e != nil {
			item.Error = e.Error()
		}
		result.Plan = value
		add(item)
		if halt() {
			return true
		}
	}
	return false
}
