package morse

import (
	"strings"
)

// Global State
var IsStopping bool

func EncodeWord(input string) []rune {
	var fullPattern []rune

	// 1. Detect Prosign (e.g., <AS>)
	if strings.HasPrefix(input, "<") && strings.HasSuffix(input, ">") {
		// Strip the brackets to get the key, e.g., "AS"
		key := strings.TrimSuffix(strings.TrimPrefix(input, "<"), ">")

		// Lookup in your ProSignTable
		if code, exists := ProSignTable[key]; exists {
			return []rune(code) // Return raw sequence without '|' characters
		}
	}

	for i, char := range input {
		// 1. Handle Literal Word Space
		if char == ' ' {
			fullPattern = append(fullPattern, ' ')
			continue
		}

		// 2. Map the Character to Morse
		if pattern, ok := MorseTable[char]; ok {
			fullPattern = append(fullPattern, []rune(pattern)...)

			// 3. Add a Letter Gap ('|') if the next char isn't a space or the end
			if i+1 < len(input) && input[i+1] != ' ' {
				fullPattern = append(fullPattern, '|')
			}
		}
	}
	return fullPattern
}
