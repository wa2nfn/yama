package morse

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"
	"yama/config"
)

// ExportWAVBatch processes global text, chunks it, and generates sequential .wav files.
func ExportWAVBatch(fullText string, targetDir string, baseName string, maxWords int, maxFiles int) ([]string, error) {
	var generatedFiles []string

	// 1. Break the entire raw text into an array of words
	words := strings.Fields(fullText)
	if len(words) == 0 {
		return nil, fmt.Errorf("no text to export")
	}

	// 2. Global Feature: Shuffle the ENTIRE document first (if enabled)
	if config.User.RandomOrder {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		r.Shuffle(len(words), func(i, j int) {
			words[i], words[j] = words[j], words[i]
		})
	}

	// 0 means dump it all into one giant file
	if maxWords <= 0 {
		maxWords = len(words)
	}

	fileCount := 0

	// 3. Slice the words array into chunks
	for i := 0; i < len(words); i += maxWords {
		if fileCount >= maxFiles {
			break
		}

		end := i + maxWords
		if end > len(words) {
			end = len(words)
		}

		chunkWords := words[i:end]

		// 4. Run the exact IWR engine filters on this specific chunk
		playlist := buildPlaylistFromWords(chunkWords)

		// Skip empty files if the filter threw away all the characters
		if len(playlist) == 0 {
			continue
		}

		// 5. Generate the actual .wav file
		fileName := fmt.Sprintf("%s/%s_%d.wav", targetDir, baseName, fileCount+1)

		// Force the 11025 sample rate here to ensure consistent 8-bit sizing
		err := RenderWAVToFile(playlist, fileName, 11025)
		if err != nil {
			log.Printf("Failed to render %s: %v", fileName, err)
			return generatedFiles, err
		}

		generatedFiles = append(generatedFiles, fileName)
		fileCount++
	}

	return generatedFiles, nil
}

// buildPlaylistFromWords is a perfect clone of the top-half of RunIWR.
// It funnels raw words through WordBuilder, RandomWords, and IWR checks.
func buildPlaylistFromWords(words []string) []PlayContext {
	var playlist []PlayContext
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	for _, rawWord := range words {
		// === THE FUNNEL ===
		var cleanBuilder strings.Builder
		for _, t := range Tokenize(rawWord) {
			if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
				lookup := strings.ToUpper(t[1 : len(t)-1])
				if _, ok := ProSignTable[lookup]; ok {
					cleanBuilder.WriteString(t)
				}
			} else {
				if len(t) > 0 {
					charRune := []rune(strings.ToUpper(t))[0]
					if _, ok := MorseTable[charRune]; ok {
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

		// Feature: WordBuilder
		if config.User.WordBuilder && len(w) > 1 && !isProsign {
			for i := 1; i < len(w); i++ {
				playlist = append(playlist, PlayContext{Word: w[:i], IsIWR: false, HideText: true})
			}
			playlist = append(playlist, PlayContext{Word: w, IsIWR: false, HideText: false})
			if config.User.IWREnabled {
				playlist = append(playlist, PlayContext{Word: w, IsIWR: true, HideText: true})
			}
			continue
		}

		// Feature: Random Words & IWR Matching
		var finalWord = w
		var isIwrMatchFound bool

		if config.User.RandomWords && len(w) > 1 && !isProsign {
			finalWord = shuffleWord(w, r)
			isIwrMatchFound = false
		} else {
			// uses the global IWR manager naturally!
			isIwrMatchFound = isIWRMatch(w)
		}

		playlist = append(playlist, PlayContext{
			Word:     finalWord,
			IsIWR:    isIwrMatchFound,
			HideText: false,
		})
	}

	return playlist
}
