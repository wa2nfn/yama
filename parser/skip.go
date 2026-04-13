package parser

import (
	"regexp"
	"strings"
)

var (
	skipChars    = make(map[rune]bool)
	skipProSigns = make(map[string]bool)

	prosignRegex = regexp.MustCompile(`<[^>]+>`)
)

// SetSkipList parses the raw config string using the provided ProSign validation map.
func SetSkipList(configStr string, validProSigns map[string]string) {
	skipChars = make(map[rune]bool)
	skipProSigns = make(map[string]bool)

	// 1. Extract and validate ProSigns using the map from tables.go
	matches := prosignRegex.FindAllString(configStr, -1)
	for _, match := range matches {
		inner := strings.Trim(match, "<>")
		inner = strings.ToUpper(inner)

		// If valid according to the passed-in morse.ProSignTable, add it
		if _, isValid := validProSigns[inner]; isValid {
			skipProSigns[strings.ToUpper(match)] = true
		}

		configStr = strings.Replace(configStr, match, "", 1)
	}

	// 2. Process the remaining string as individual characters
	for _, r := range configStr {
		if r != ' ' {
			skipChars[r] = true
		}
	}
}

// ApplySkip removes any characters or ProSigns defined in the active skip list.
func ApplySkip(text string) string {
	if len(skipChars) == 0 && len(skipProSigns) == 0 {
		return text
	}

	for ps := range skipProSigns {
		text = strings.ReplaceAll(text, strings.ToUpper(ps), "")
		text = strings.ReplaceAll(text, strings.ToLower(ps), "")
	}

	var sb strings.Builder
	for _, r := range text {
		if !skipChars[r] {
			sb.WriteRune(r)
		}
	}

	return CompressSpace(sb.String())
}
