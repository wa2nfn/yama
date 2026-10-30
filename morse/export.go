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

// -----------------------------------------------------------------------------
// Global RNG (single source of randomness)
// -----------------------------------------------------------------------------

var R = rand.New(rand.NewSource(time.Now().UnixNano()))

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func ShuffleRunes(rng *rand.Rand, s string) string {
	runes := []rune(s)
	rng.Shuffle(len(runes), func(i, j int) {
		runes[i], runes[j] = runes[j], runes[i]
	})
	return string(runes)
}

func CleanWord(raw string) (string, bool) {
	var b strings.Builder
	isPro := false

	for _, t := range Tokenize(raw) {
		if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
			lookup := strings.ToUpper(t[1 : len(t)-1])
			if _, ok := ProSignTable[lookup]; ok {
				b.WriteString(t)
				isPro = true
			}
			continue
		}

		if len(t) > 0 {
			r := []rune(strings.ToUpper(t))[0]
			if _, ok := MorseTable[r]; ok {
				b.WriteString(t)
			}
		}
	}

	return b.String(), isPro
}

func AppendWordBuilder(pl *[]PlayContext, w string) {
	for i := 1; i < len(w); i++ {
		*pl = append(*pl, PlayContext{Word: w[:i], HideText: true})
	}
	*pl = append(*pl, PlayContext{Word: w})
	if config.User.IWREnabled {
		*pl = append(*pl, PlayContext{Word: w, IsIWR: true, HideText: true})
	}
}

func ProcessWordIWR(w string, isPro bool) (string, bool) {
	if config.User.RandomizeWords && len(w) > 1 && !isPro {
		return ShuffleRunes(R, w), false
	}
	return w, isIWRMatch(w)
}

func RampSpeed(origStart, origEnd float64, posStart, posEnd float64) (float64, float64, float64) {
	start := origStart + (origEnd-origStart)*posStart
	end := origStart + (origEnd-origStart)*posEnd
	mult := start / origStart
	return start, end, mult
}

func getSpeedBlock(charSpd, effSpd float64) string {
	modeChar := "S"
	if config.User.UseWordsworth {
		modeChar = "W"
	} else if config.User.UseFarnsworth {
		modeChar = "F"
	}

	charSpd = math.Round(charSpd*10) / 10
	effSpd = math.Round(effSpd*10) / 10

	if modeChar == "S" {
		return fmt.Sprintf("%s%g", modeChar, charSpd)
	}
	return fmt.Sprintf("%s%g_%g", modeChar, charSpd, effSpd)
}

// -----------------------------------------------------------------------------
// ExportWAVBatch — streamlined, operator‑correct
// -----------------------------------------------------------------------------

func ExportWAVBatch(fullText string, targetDir string, baseName string,
	maxWords int, maxFiles int) ([]string, error) {

	var generatedFiles []string

	words := strings.Fields(fullText)
	total := len(words)
	if total == 0 {
		return nil, fmt.Errorf("no text to export")
	}

	if config.User.WordOrder {
		R.Shuffle(len(words), func(i, j int) {
			words[i], words[j] = words[j], words[i]
		})
	}

	if maxWords <= 0 {
		maxWords = len(words)
	}

	// Snapshot original config
	origChar := config.User.CharacterSpeed
	origEff := config.User.EffectiveSpeed
	origIwr := config.User.IWRSpeed
	origEnd := config.User.EndSpeed

	defer func() {
		config.User.CharacterSpeed = origChar
		config.User.EffectiveSpeed = origEff
		config.User.IWRSpeed = origIwr
		config.User.EndSpeed = origEnd
	}()

	fileCount := 0
	processed := 0

	for len(words) > 0 {
		if fileCount >= maxFiles {
			break
		}

		chunkSize := maxWords
		if len(words) <= int(float64(maxWords)*1.10) {
			chunkSize = len(words)
		}
		if chunkSize > len(words) {
			chunkSize = len(words)
		}

		chunk := words[:chunkSize]
		words = words[chunkSize:]

		// Speed ramp
		if total > 1 {
			posStart := float64(processed) / float64(total-1)
			posEnd := float64(processed+len(chunk)-1) / float64(total-1)

			startSpd, endSpd, mult := RampSpeed(origChar, origEnd, posStart, posEnd)

			config.User.CharacterSpeed = startSpd
			config.User.EffectiveSpeed = origEff * mult
			config.User.IWRSpeed = origIwr * mult
			config.User.EndSpeed = endSpd
		}

		playlist := buildPlaylistFromWords(chunk)
		if len(playlist) == 0 {
			processed += len(chunk)
			continue
		}

		speedBlock := getSpeedBlock(config.User.CharacterSpeed, config.User.EffectiveSpeed)
		cleanName := fmt.Sprintf("%s_%s_%d.wav", baseName, speedBlock, fileCount+1)
		fileName := ResolvePath(filepath.Join(targetDir, cleanName))

		if err := RenderWAVToFile(playlist, fileName, 11025); err != nil {
			return generatedFiles, err
		}

		generatedFiles = append(generatedFiles, fileName)
		fileCount++
		processed += len(chunk)
	}

	return generatedFiles, nil
}

// -----------------------------------------------------------------------------
// Playlist builder — streamlined, identical behavior
// -----------------------------------------------------------------------------

func buildPlaylistFromWords(words []string) []PlayContext {
	var pl []PlayContext

	for _, raw := range words {
		w, isPro := CleanWord(raw)
		if len(strings.TrimSpace(w)) == 0 {
			continue
		}

		if config.User.WordBuilder && len(w) > 1 && !isPro {
			AppendWordBuilder(&pl, w)
			continue
		}

		final, isIwr := ProcessWordIWR(w, isPro)

		pl = append(pl, PlayContext{
			Word:     final,
			IsIWR:    isIwr,
			HideText: false,
		})
	}

	return pl
}
