package parser

import (
	"strings"
)

func CleanHeaderFooter(s string) string {
	if s == "" {
		return ""
	}

	s = NormalizeLine(s) // Assuming this is defined elsewhere in your parser package
	s = strings.ToUpper(s)
	s = ApplySkip(s) // Assuming this uses your configured SkipList

	// Smart Length Trim (Target: 20 characters)
	limit := 20
	if len(s) > limit {
		// Look for the last '<' and '>' within our target slice
		lastOpen := strings.LastIndex(s[:limit], "<")
		lastClose := strings.LastIndex(s[:limit], ">")

		if lastOpen > lastClose {
			// We sliced right through the middle of a prosign! (e.g., "<A" instead of "<AR>")
			// Let's search the ORIGINAL string to find where this prosign actually ends.
			closingBracketOffset := strings.Index(s[lastOpen:], ">")

			if closingBracketOffset != -1 {
				// We found the end! Let it exceed the 20-char limit to finish the prosign safely.
				s = s[:lastOpen+closingBracketOffset+1]
			} else {
				// Malformed string (no closing bracket found anywhere), trim it back to before the '<'
				s = s[:lastOpen]
			}
		} else {
			// Safe cut, no prosign was harmed
			s = s[:limit]
		}
	}

	return s
}
