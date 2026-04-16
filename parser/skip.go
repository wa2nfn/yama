package parser

import (
	"regexp"
	"strings"
	"yama/config"
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

	// 1. Expand contractions BEFORE we start blindly ripping out characters.
	// This saves the words (e.g., "DON'T" -> "DO NOT") before the apostrophe is swallowed.
	if skipChars['\''] {
		text = expandContractions(text)
	}

	// 2. Remove Prosigns
	for ps := range skipProSigns {
		text = strings.ReplaceAll(text, strings.ToUpper(ps), "")
		text = strings.ReplaceAll(text, strings.ToLower(ps), "")
	}

	// 3. Remove Skipped Characters
	var sb strings.Builder
	for _, r := range text {
		if !skipChars[r] {
			sb.WriteRune(r)
		}
	}

	// Assuming CompressSpace is a helper function elsewhere in your parser package
	return CompressSpace(sb.String())
}

func expandContractions(text string) string {
	
	for contraction, expansion := range config.Contractions {
		text = strings.ReplaceAll(text, contraction, expansion)
	}

	return text
}
