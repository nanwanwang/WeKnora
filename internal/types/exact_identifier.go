package types

import (
	"regexp"
	"strings"
)

// ExactIdentifiers preserves complete work-order-like identifiers. ASCII
// boundaries prevent PA-123456 matching XPA-123456 or PA-123456-REV.
var exactIdentifierPattern = regexp.MustCompile(`[A-Za-z]{2,8}-[0-9]{6,20}`)

func ExactIdentifiers(text string) []string {
	out := []string{}
	seen := map[string]bool{}
	word := func(b byte) bool {
		return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_'
	}
	for _, p := range exactIdentifierPattern.FindAllStringIndex(text, -1) {
		if p[0] > 0 && word(text[p[0]-1]) || p[1] < len(text) && word(text[p[1]]) {
			continue
		}
		id := strings.ToUpper(text[p[0]:p[1]])
		if !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	return out
}
func ContainsExactIdentifier(content string, identifiers []string) bool {
	for _, got := range ExactIdentifiers(content) {
		for _, wanted := range identifiers {
			if got == wanted {
				return true
			}
		}
	}
	return false
}
