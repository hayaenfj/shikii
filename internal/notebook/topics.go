package notebook

// TopicMatches is candidate retrieval, never semantic equivalence or evidence
// that an earlier observation applies. Use the same content terms as wiki search.
func TopicMatches(a, b string) bool {
	left, right := searchTerms(a), searchTerms(b)
	pairs := 0
	for term := range left {
		if !right[term] || !isContentTerm(term) {
			continue
		}
		if isIdentifierTerm(term) {
			return true
		}
		pairs++
	}
	return pairs >= 2
}
