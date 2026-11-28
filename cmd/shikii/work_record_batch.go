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
	// gradeLeftOut is what the reply says about a result.sufficient that came with no evidence and was left out of the result
	// (leaveOutUnevidencedGrade); empty when none was.
	gradeLeftOut workGradeNote
}
