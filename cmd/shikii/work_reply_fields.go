package main

// workReplyField says what the bound of a preparation (limitWorkPreparation) does with one field of a reply.
type workReplyField struct {
	// Kept is true for a field the bound never leaves out. Cap is then the most bytes its JSON takes (the bytes of the value, as
	// workContextSize counts them), and a test builds the largest value the code can make and holds it to this number.
	Kept bool
	Cap  int
	// Back is, for a field the bound may leave out, the way to read it again: the words the reply itself gives for it, in
	// output.next_action or in the name output gives the field. A test checks that every word is in a reply that left the field out.
	Back string
}

// workReplyFields registers every field a reply of prepare can carry, by its name in the reply. It is an allow list: a field that
// is not here is not a field the bound knows what to do with, and the test that reads the code that builds a reply fails on it
// (TestEveryFieldBuiltIntoAPreparationReplyIsRegistered). Add a field here, as kept with its cap or as one that may go with its way back,
// when a reply gets one.
//
// A field the producer sizes by the bound itself is kept with the largest bound a caller can name as its cap: the answer to an
// explicit call (source, experience) is the reason for the call, and a caller that asks for more than fits is told to name a larger
// max_bytes.
var workReplyFields = map[string]workReplyField{
	// kept: small and fixed, or sized by the task itself (a request is never cut here; the hook delivers its head when the whole
	// does not fit, hookTaskHeadMax)
	"receipt":             {Kept: true, Cap: 2048}, // a session id takes up to 200 bytes, and JSON writes some characters (such as <) in six bytes each
	"task":                {Kept: true, Cap: workSourceBudget*3 - workTaskReserve},
	"task_context":        {Kept: true, Cap: 768},
	"agent_supplied_task": {Kept: true, Cap: 24},
	"planning_required":   {Kept: true, Cap: 24},
	"more_pending":        {Kept: true, Cap: 24},
	"delivery":            {Kept: true, Cap: 24},
	"how_to_record":       {Kept: true, Cap: 1280},
	"knowledge_query":     {Kept: true, Cap: 6*requestMax + 512},
	"output":              {Kept: true, Cap: 2560},
	"reading_notices":     {Kept: true, Cap: 1024},
	"procedure_recovery":  {Kept: true, Cap: 640},
	"experience":          {Kept: true, Cap: workSourceBudget * 3},
	"source":              {Kept: true, Cap: workSourceBudget * 3},
	"source_query":        {Kept: true, Cap: 2 * 16000},

	// may go: the order is limitWorkPreparation's, then leaveOutLastFields' (maintenance, carried, knowledge)
	"memory_upgrade":       {Back: "memory-upgrade"},
	"collective":           {Back: "max_bytes"},
	"conversation":         {Back: "host conversation"},
	"conversation_context": {Back: "host conversation"},
	"context_notice":       {Back: "max_bytes"},
	"knowledge_context":    {Back: "max_bytes"},
	"reflection_context":   {Back: "reflect query"},
	"reasoning":            {Back: "max_bytes"},
	"reading":              {Back: "max_bytes"},
	"attention":            {Back: "max_bytes"},
	"coverage":             {Back: "max_bytes"},
	"earlier_search":       {Back: "max_bytes"},
	"found_alone":          {Back: "max_bytes"},
	"pending":              {Back: "reflect query"},
	"observations":         {Back: "reflect query"},
	"maintenance":          {Back: "reflect query"},
	"carried":              {Back: "stay valid with this receipt"},
	"knowledge":            {Back: "lesson:<id>@<revision>"},
}
