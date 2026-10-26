// Package textsim measures how alike two requirements read: word sets and
// their Jaccard similarity. Shared by the reduce clustering and the
// consistency check so both judge similarity the same way.
package textsim

import (
	"regexp"
	"strings"
)

// minWordLen drops very short tokens (articles, glue words) that inflate
// similarity.
const minWordLen = 3

var wordRe = regexp.MustCompile(`[\p{L}\p{N}]+`)

// Words returns the lowercased word set of s, without very short tokens.
func Words(s string) map[string]bool {
	words := make(map[string]bool)
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		if len([]rune(w)) >= minWordLen {
			words[w] = true
		}
	}
	return words
}

// Jaccard is |a ∩ b| / |a ∪ b|, 0 when either set is empty.
func Jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}
