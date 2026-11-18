package morse

import (
	"sort"
	"strings"
	"yama/config"
)

// buildIWRPlaylist handles all text formatting, syllables, separators, and Word/Text Builder logic
func buildIWRPlaylist(text string) []PlayContext {
	var validSeparators []string
	if config.User.WordSeparator != "" {
		inProsign := false
		var currentToken string

		for _, rChar := range config.User.WordSeparator {
			if rChar == ' ' {
				continue
			}
			if rChar == '<' {
				inProsign = true
				currentToken = "<"
			} else if rChar == '>' && inProsign {
				currentToken += ">"
				inner := currentToken[1 : len(currentToken)-1]

				isValid := true
				if len(inner) < 2 || len(inner) > 3 {
					isValid = false
				} else {
					for _, c := range inner {
						if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
							isValid = false
							break
						}
					}
				}

				if isValid {
					upperInner := strings.ToUpper(inner)
					if _, ok := ProSignTable[upperInner]; ok {
						validSeparators = append(validSeparators, strings.ToUpper(currentToken))
					} else {
						validSeparators = append(validSeparators, strings.ToUpper(currentToken))
					}
				}
				inProsign = false
				currentToken = ""
			} else if inProsign {
				currentToken += string(rChar)
			} else {
				upperChar := []rune(strings.ToUpper(string(rChar)))[0]
				if _, ok := MorseTable[upperChar]; ok {
					validSeparators = append(validSeparators, string(rChar))
				}
			}
		}
	}

	words := strings.Fields(text)
	if (config.User.TextBuilder && config.User.TextBuilderSort) ||
		(config.User.WordBuilder && config.User.WordBuilderSort) {
		sort.Slice(words, func(i, j int) bool {
			return len(words[i]) < len(words[j])
		})
	}

	var startMsgWords []string
	if config.User.StartMsg && config.User.StartMsgText != "" {
		startMsgWords = strings.Fields(config.User.StartMsgText)
	}

	var endMsgWords []string
	if config.User.EndMsg && config.User.EndMsgText != "" {
		endMsgWords = strings.Fields(config.User.EndMsgText)
	}

	if config.User.WordOrder {
		fastShuffleSlice(words)
	}

	if config.User.TextBuilder {
		count := config.User.TextWordCount
		if count < 2 {
			count = 2
		}

		sep := strings.TrimSpace(config.User.TextSeparator)
		var textBuilderWords []string

		for start := 0; start < len(words); start += count {
			end := start + count
			if end > len(words) {
				end = len(words)
			}
			chunk := words[start:end]
			for i := 0; i < len(chunk); i++ {
				for j := 0; j <= i; j++ {
					textBuilderWords = append(textBuilderWords, chunk[j])
				}
			}
			if sep != "" && end < len(words) {
				sepTokens := strings.Fields(sep)
				textBuilderWords = append(textBuilderWords, sepTokens...)
			}
		}
		words = textBuilderWords
	}

	playlist := []PlayContext{}
	queueRawWord := func(rawWord string) {
		var cleanBuilder strings.Builder
		for _, t := range Tokenize(rawWord) {
			if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
				lookup := strings.ToUpper(t[1 : len(t)-1])
				if _, ok := ProSignTable[lookup]; ok {
					cleanBuilder.WriteString(t)
				}
			} else {
				if len(t) > 0 {
					r := []rune(strings.ToUpper(t))[0]
					if _, ok := MorseTable[r]; ok {
						cleanBuilder.WriteString(t)
					}
				}
			}
		}

		w := cleanBuilder.String()
		if len(strings.TrimSpace(w)) > 0 {
			playlist = append(playlist, PlayContext{
				Word:     w,
				IsIWR:    false,
				HideText: false,
			})
		}
	}

	for _, w := range startMsgWords {
		queueRawWord(w)
	}

	for _, rawWord := range words {
		if IsStopping {
			return playlist
		}

		var cleanBuilder strings.Builder
		for _, t := range Tokenize(rawWord) {
			if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
				lookup := strings.ToUpper(t[1 : len(t)-1])
				if _, ok := ProSignTable[lookup]; ok {
					cleanBuilder.WriteString(t)
				}
			} else {
				if len(t) > 0 {
					rCh := []rune(strings.ToUpper(t))[0]
					if _, ok := MorseTable[rCh]; ok {
						cleanBuilder.WriteString(t)
					}
				}
			}
		}
		w := cleanBuilder.String()

		if len(strings.TrimSpace(w)) == 0 {
			continue
		}

		isProsign := strings.HasPrefix(w, "<") && strings.HasSuffix(w, ">")

		if config.User.SyllableExpansion && !isProsign {
			if expanded, exists := syllables[w]; exists {
				expandedParts := strings.Fields(expanded)
				for _, part := range expandedParts {
					playlist = append(playlist, PlayContext{
						Word:     part,
						IsIWR:    isIWRMatch(part),
						HideText: false,
					})
				}
				continue
			}
		}

		var finalWord = w
		var isIwrMatchFound bool

		if config.User.RandomizeWords && len(w) > 1 && !isProsign {
			finalWord = shuffleWord(w)
			isIwrMatchFound = false
		} else {
			isIwrMatchFound = isIWRMatch(w)
		}

		isCurrentSep := false
		for _, sep := range validSeparators {
			if finalWord == sep {
				isCurrentSep = true
				break
			}
		}

		lastIsSep := false
		if len(playlist) > 0 {
			lastItem := playlist[len(playlist)-1].Word
			for _, sep := range validSeparators {
				if lastItem == sep {
					lastIsSep = true
					break
				}
			}
		}

		if isCurrentSep && lastIsSep {
			continue
		}

		if config.User.WordBuilder && len(w) > 1 && !isProsign {
			for i := 1; i < len(w); i++ {
				playlist = append(playlist, PlayContext{
					Word:     w[:i],
					IsIWR:    false,
					HideText: false,
				})
			}
			playlist = append(playlist, PlayContext{Word: finalWord, IsIWR: false, HideText: false})
			playlist = append(playlist, PlayContext{Word: finalWord, IsIWR: false, HideText: false})

			if config.User.IWREnabled {
				playlist = append(playlist, PlayContext{Word: finalWord, IsIWR: true, HideText: false})
			}

		} else {
			playlist = append(playlist, PlayContext{
				Word:     finalWord,
				IsIWR:    isIwrMatchFound,
				HideText: false,
			})
		}

		if config.User.WordBuilder && len(validSeparators) > 0 {
			lastItem := playlist[len(playlist)-1].Word
			lastIsSep = false
			for _, sep := range validSeparators {
				if lastItem == sep {
					lastIsSep = true
					break
				}
			}
			if !lastIsSep {
				selectedSep := validSeparators[fastRand(len(validSeparators))]
				playlist = append(playlist, PlayContext{Word: selectedSep, IsIWR: false, HideText: false})
			}
		}
	}

	for _, w := range endMsgWords {
		queueRawWord(w)
	}

	return playlist
}
