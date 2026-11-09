package main

func reduceAssignmentPeers(team map[string]any, reviews, questions, coverage *[]any) bool {
	switch {
	case len(*reviews) > 0:
		*reviews = (*reviews)[:len(*reviews)-1]
		team["procedure_reviews"] = *reviews
	case len(*questions) > 0:
		*questions = (*questions)[:len(*questions)-1]
		team["questions"] = *questions
	case len(*coverage) > 0:
		*coverage = (*coverage)[:len(*coverage)-1]
		team["coverage"] = *coverage
	default:
		return false
	}
	return true
}
