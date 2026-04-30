package morse

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"strings"
	"time"
	"yama/config"
)

// Helper to generate the S20 or f20.5_15 string for filenames
func getSpeedBlock(charSpd, effSpd float64) string {
	modeChar := "S"
	if config.User.UseWordsworth {
		modeChar = "W"
	} else if config.User.UseFarnsworth {
		modeChar = "F"
	}

	// Round to 1 decimal place to prevent floating point garbage in filenames
	charSpd = math.Round(charSpd*10) / 10
	effSpd = math.Round(effSpd*10) / 10

	if strings.ToUpper(modeChar) == "S" {
		return fmt.Sprintf("%s%g", modeChar, charSpd)
	}
	return fmt.Sprintf("%s%g_%g", modeChar, charSpd, effSpd)
}

// ExportWAVBatch processes global text, chunks it, and generates sequential .wav files.
func ExportWAVBatch(fullText string, targetDir string, baseName string, maxWords int, maxFiles int) ([]string, error) {
	var generatedFiles []string

	words := strings.Fields(fullText)
	totalDocumentWords := len(words) // Track global size for speed ramping
	if totalDocumentWords == 0 {
		return nil, fmt.Errorf("no text to export")
	}

	if config.User.RandomOrder {
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		r.Shuffle(len(words), func(i, j int) {
			words[i], words[j] = words[j], words[i]
		})
	}

	if maxWords <= 0 {
		maxWords = len(words)
	}

	// --- SNAPSHOT ORIGINAL CONFIG ---
	origCharSpd := config.User.CharacterSpeed
	origEffSpd := config.User.EffectiveSpeed
	origIwrSpd := config.User.IWRSpeed
	origEndSpd := config.User.EndSpeed

	// Guarantee config goes back to normal if the loop crashes or completes
	defer func() {
		config.User.CharacterSpeed = origCharSpd
		config.User.EffectiveSpeed = origEffSpd
		config.User.IWRSpeed = origIwrSpd
		config.User.EndSpeed = origEndSpd
	}()
	// --------------------------------

	fileCount := 0
	wordsProcessed := 0

	for len(words) > 0 {
		if fileCount >= maxFiles {
			break
		}

		chunkSize := maxWords
		if len(words) <= int(float64(maxWords)*1.10) {
			chunkSize = len(words)
		} else if chunkSize > len(words) {
			chunkSize = len(words)
		}

		chunkWords := words[:chunkSize]
		words = words[chunkSize:]

		// --- GLOBAL CHUNK SPEED MATH ---
		chunkStartProgress := float64(wordsProcessed) / float64(totalDocumentWords-1)
		chunkEndProgress := float64(wordsProcessed+len(chunkWords)-1) / float64(totalDocumentWords-1)
		if totalDocumentWords <= 1 {
			chunkStartProgress, chunkEndProgress = 0, 0
		}

		chunkStartCharSpd := origCharSpd + (origEndSpd-origCharSpd)*chunkStartProgress
		chunkEndCharSpd := origCharSpd + (origEndSpd-origCharSpd)*chunkEndProgress
		chunkStartMultiplier := chunkStartCharSpd / origCharSpd

		// Temporarily apply the chunk's boundaries to the config for RenderWAVToFile
		config.User.CharacterSpeed = chunkStartCharSpd
		config.User.EffectiveSpeed = origEffSpd * chunkStartMultiplier
		config.User.IWRSpeed = origIwrSpd * chunkStartMultiplier
		config.User.EndSpeed = chunkEndCharSpd
		// -------------------------------

		playlist := buildPlaylistFromWords(chunkWords)
		if len(playlist) == 0 {
			wordsProcessed += len(chunkWords)
			continue
		}

		// Calculate speed block using the starting speed of THIS specific chunk
		speedBlock := getSpeedBlock(chunkStartCharSpd, config.User.EffectiveSpeed)

		cleanFileName := fmt.Sprintf("%s_%s_%d.wav", baseName, speedBlock, fileCount+1)
		fileName := ResolvePath(filepath.Join(targetDir, cleanFileName))

		err := RenderWAVToFile(playlist, fileName, 11025)
		if err != nil {
			return generatedFiles, err
		}

		generatedFiles = append(generatedFiles, fileName)
		fileCount++
		wordsProcessed += len(chunkWords)
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
