package parser

import (
	"strings"
	"yama/config"
)

// textNormalizer handles all typography and whitespace replacements in a single optimized pass.
// Declared globally so it only compiles once.
var textNormalizer = strings.NewReplacer(
	"“", "\"", "”", "\"", "‘", "'", "’", "'",
	"—", "-", "–", "-", "…", "...",
	"\t", " ", "\r", "", "\n", " ",
)

// NormalizeText prepares raw data (fixes typography, spacing, and casing).
func NormalizeText(input string) string {
	return strings.ToUpper(textNormalizer.Replace(input))
}

// FilterValidMorse scrubs any character that doesn't exist in the active table.
func FilterValidMorse(input string, activeTable map[rune]string) string {
	if activeTable == nil {
		return input
	}

	var clean strings.Builder
	clean.Grow(len(input)) // Memory optimization

	for _, r := range input {
		// Always protect spaces AND our ProSign brackets!
		if r == ' ' || r == '<' || r == '>' {
			clean.WriteRune(r)
			continue
		}

		// Look up the rune directly in the active table
		if _, exists := activeTable[r]; exists {
			clean.WriteRune(r)
		}
	}

	return clean.String()
}

// CompressSpace is the final polish for the engine.
// It removes trailing/leading/double spaces, and applies the user's RepeatLimit.
func CompressSpace(text string) string {
	// strings.Fields splits by any whitespace, strings.Join stitches with single spaces.
	text = strings.Join(strings.Fields(text), " ")

	// User-defined Repeat Limit (Audible text only)
	if config.User.RepeatLimit > 0 {
		text = RepeatLimit(text, config.User.RepeatLimit)
	}

	return text
}

// RepeatLimit prevents long strings of the same character (e.g., ".......")
// from playing indefinitely, capping them at the user's limit.
func RepeatLimit(src string, maxLimit int) string {
	if maxLimit < 1 {
		return src
	}
	var out []rune
	var last rune
	count := 0

	flush := func() {
		if count == 0 {
			return
		}
		n := count
		if n > maxLimit {
			n = maxLimit
		}
		for i := 0; i < n; i++ {
			out = append(out, last)
		}
		count = 0
	}

	for _, r := range src {
		if r == last {
			count++
		} else {
			flush()
			last = r
			count = 1
		}
	}
	flush()
	return string(out)
}
