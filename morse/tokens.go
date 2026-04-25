package morse

import (
	"regexp"
)

// Token carries the actual calculated duration in seconds
type Token struct {
	Type     string  // "dot", "dash", "space"
	Duration float64 // The literal time in seconds
}

// Tokenize breaks a string like "bi<ar>ll" into ["b", "i", "<ar>", "l", "l"]
func Tokenize(input string) []string {
	// Simple bracket compression: <<< -> <
	re := regexp.MustCompile(`<{2,}`)
	input = re.ReplaceAllString(input, "<")
	re = regexp.MustCompile(`>{2,}`)
	input = re.ReplaceAllString(input, ">")

	var tokens []string
	runes := []rune(input)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '<' {
			end := -1
			for j := i; j < len(runes); j++ {
				if runes[j] == '>' {
					end = j
					break
				}
			}
			if end != -1 {
				tokens = append(tokens, string(runes[i:end+1]))
				i = end
				continue
			}
		}
		tokens = append(tokens, string(runes[i]))
	}
	return tokens
}
