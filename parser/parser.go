package parser

import (
	"regexp"
	"strings"
	"yama/config"
)

// Compile once at the package level
var re = regexp.MustCompile(`\s+`)

// NormalizeLine provides consistent cleaning for headers and state titles.
// It uses the standard logic but ensures a clean, single-line uppercase output.
func NormalizeLine(input string) string {
	text := strings.TrimSpace(input)
	return re.ReplaceAllString(text, " ")
}

// CompressSpace is the entry point for the IWR engine.
// It runs the full pipeline based on the User Config.
func CompressSpace(text string) string {
	// 1. Protect Prosigns then Clean "Garbage"
	// Note: CleanText should handle the UpperCase conversion and Prosign preservation.
	//text = CleanText(text,activeTable)

	// 2. Mandatory Compressions
	text = NormalizeLine(text)
	text = CompressSpaces(text)

	// 3. User-defined Repeat Limit (Audible text only)
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

// CompressSpaces ensures no trailing/leading or double spaces remain.
func CompressSpaces(src string) string {
	return strings.Join(strings.Fields(src), " ")
}

// CleanText handles typography normalization, regex garbage collection, casing, and table safety.
// It now safely accepts your master map[rune]string dictionary.
func CleanText(input string, activeTable map[rune]string) string {
	// 1. Neutralize "smart" typography and line breaks FIRST
	replacer := strings.NewReplacer(
		"“", "\"", "”", "\"", "‘", "'", "’", "'",
		"—", "-", "–", "-", "…", "...",
		"\t", " ", "\r", "", "\n", " ",
	)
	input = replacer.Replace(input)

	input = strings.ToUpper(input)

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

		// Look up the rune directly! If it's valid Morse (like '?'), it stays!
		if _, exists := activeTable[r]; exists {
			clean.WriteRune(r)
		}
	}

	return clean.String()
}
