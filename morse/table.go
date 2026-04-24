package morse

import (
	"regexp"
	"strings"
	"yama/config"
)

// MorseRegex now only permits A-Z, 0-9, and supported punctuation.
var MorseRegex = regexp.MustCompile(`[^A-Z0-9\.\,\?\/\:\;\=\+\-\"\@\<\>\s]`)

// MorseTable is the dynamic "Source of Truth"
var MorseTable = make(map[rune]string)
var ProSignTable = make(map[string]string)

// ProSignTable remains static
var baseProSignMap = map[string]string{
	"AR": ".-.-.", "AS": ".-...", "BT": "-...-", "KA": "-.-.-",
	"SK": "...-.-", "VA": "...-.-", "VE": "...-.-", "SN": "...-.-",
	"BK": "-... -.-", "HH": "........", "DU": "-....-",
	"SOS": "...---...", "CH": "----",
}

var basicMap = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.",
	'G': "--.", 'H': "....", 'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..",
	'M': "--", 'N': "-.", 'O': "---", 'P': ".--.", 'Q': "--.-", 'R': ".-.",
	'S': "...", 'T': "-", 'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-",
	'Y': "-.--", 'Z': "--..",
	'0': "-----", '1': ".----", '2': "..---", '3': "...--", '4': "....-",
	'5': ".....", '6': "-....", '7': "--...", '8': "---..", '9': "----.",
	// Standard Punctuation
	'.': ".-.-.-",
	',': "--..--",
	'?': "..--..",
	'/': "-..-.",
	// ProSign Equivalents (MUST be in basic to prevent discarding)
	'=': "-...-",  // <BT>
	'+': ".-.-.",  // <AR>
	'-': "-....-", // <DU>
}

var extendedPunctuationMap = map[rune]string{
	':': "---...", ';': "-.-.-.", '"': ".-..-.", '@': ".--.-.", '\'': ".----.",
}

func RebuildMorseTable(useExtended bool, useSkip bool, skipList string) {
	// 1. Wipe the working copies completely clean
	MorseTable = make(map[rune]string)
	ProSignTable = make(map[string]string)

	// 2. Rebuild the working copies from the blueprints
	for k, v := range basicMap {
		MorseTable[k] = v
	}
	if useExtended {
		for k, v := range extendedPunctuationMap {
			MorseTable[k] = v
		}
	}

	// === THE MASTER PROSIGN SWITCH ===
	// Only load prosigns into the working table if the user enabled them!
	if config.User.Playprosigns { // Adjust this to match your actual boolean variable
		for k, v := range baseProSignMap {
			ProSignTable[k] = v
		}
	}
	// =================================

	// 3. Apply the Skip List
	if useSkip && len(skipList) > 0 {
		tokens := Tokenize(strings.ToUpper(skipList))
		for _, t := range tokens {
			if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
				// It's a prosign! Delete from the working ProSignTable
				lookup := t[1 : len(t)-1]
				delete(ProSignTable, lookup)
			} else {
				// It's a normal char! Delete from the working MorseTable
				if len(t) > 0 {
					r := []rune(t)[0]
					delete(MorseTable, r)
				}
			}
		}
	}
}

func ProcessMorseString(input string) string {
	// 1. Force Upper
	work := strings.ToUpper(input)

	// 2. Initial clean: Keep the original regex as a safety net for weird unicode
	// (Assuming MorseRegex is still defined in your package)
	work = MorseRegex.ReplaceAllString(work, "")

	// 3. Break into words first to naturally preserve our spaces
	words := strings.Fields(work)
	var cleanWords []string

	for _, w := range words {
		var cleanWordBuilder strings.Builder

		// 4. Token-Aware Filtering on each individual word
		tokens := Tokenize(w)
		for _, t := range tokens {
			if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
				// Prosign Check: Does it survive the working copy?
				lookup := t[1 : len(t)-1]
				if _, ok := ProSignTable[lookup]; ok {
					cleanWordBuilder.WriteString(t)
				}
			} else if len(t) > 0 {
				// Standard Character Check: Does it survive the working copy?
				r := []rune(t)[0]
				if _, ok := MorseTable[r]; ok {
					cleanWordBuilder.WriteString(t)
				}
			}
		}

		// 5. Only keep the word if it isn't empty after filtering
		cleanWord := cleanWordBuilder.String()
		if len(cleanWord) > 0 {
			cleanWords = append(cleanWords, cleanWord)
		}
	}

	// 6. SPACE CONDENSER
	// Reassemble the final string. strings.Join guarantees exactly one space between words.
	return strings.Join(cleanWords, " ")
}
