package main

import (
	"github.com/hayaenfj/shikii/internal/notebook"
)

type workRecordBatch struct {
	root, scope string
	req         workRequest
	token       workToken
	book        notebook.Book
	w           *workState
	save        func() error
	commit      workCommitter
	add         func(workItem)
	halt        func() bool
	resolve     func([]string) ([]string, error)
	result      *workResult
}
