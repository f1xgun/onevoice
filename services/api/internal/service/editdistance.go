package service

// runeLevenshtein returns the Levenshtein edit distance between a and b measured
// in RUNES, so multi-byte Cyrillic characters and emoji count as one unit each
// (a byte-level distance would over-count edits on Russian text). Two-row
// dynamic programming: O(len(a)*len(b)) time, O(len(b)) space.
func runeLevenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}

	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
