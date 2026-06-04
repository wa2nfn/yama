package morse

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
	"yama/config"
)

// Add IsPaused so the engine can halt in place without losing its index
var IsPaused bool
var OnWordPlayed func(word string, isIWR bool)

// OnWordChange is now triggered inside audio.go's Flush loop
var OnWordChange func(char string, index int)
var OnStatusUpdate func(msg string)

type PlayContext struct {
	Word     string
	IsIWR    bool
	HideText bool
}

func shuffleWord(w string, r *rand.Rand) string {
	runes := []rune(w)
	r.Shuffle(len(runes), func(i, j int) {
		runes[i], runes[j] = runes[j], runes[i]
	})
	return string(runes)
}

func isIWRMatch(word string) bool {
	mgr := GetManager()
	if mgr != nil {
		// Ask the manager first! If the user explicitly put <BT> in their IWR list, honor it.
		if mgr.Match(word) {
			return true
		}
	}

	// If it's NOT in the IWR list, but it IS a standalone prosign,
	// ensure it defaults to normal speed (false).
	if strings.HasPrefix(word, "<") && strings.HasSuffix(word, ">") {
		return false
	}

	return false
}

func buildWordBuffer(ctx PlayContext, p TimingProfile) {

	tokens := Tokenize(ctx.Word)

	for i, token := range tokens {
		if IsStopping {
			return
		}

		var pattern string
		var ok bool

		if strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">") {
			lookup := strings.ToUpper(token[1 : len(token)-1])
			pattern, ok = ProSignTable[lookup]
		} else {
			// Ensure char is Upper for table lookup
			r := []rune(token)[0]
			pattern, ok = MorseTable[r]
		}

		if !ok {
			continue // Gatekeeper handles skipped chars natively
		}

		isFirstElement := true
		for j, symbol := range pattern {
			dur := p.DotDuration
			if symbol == '-' {
				dur = p.DashDuration
			}

			label := ""
			if isFirstElement {
				label = token
				isFirstElement = false
			}

			QueuePCM(TonePCM(float64(p.Tone), int(dur*float64(SampleRate)), 0.5).Samples, label, i)

			// Only queue Inter-Element space if it's NOT the last symbol
			if j < len(pattern)-1 {
				QueuePCM(SilencePCM(int(p.InterElement*float64(SampleRate))).Samples, "", i)
			}
		}

		// Only queue Inter-Character space if it's NOT the last token in the word
		if i < len(tokens)-1 {
			QueuePCM(SilencePCM(int(p.CharSpace*float64(SampleRate))).Samples, "", i)
		}
	}
}

func RunIWR(text string, iwrMan *IWRManager) {
	//WDL VerifyParisTiming(config.User.CharacterSpeed)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// ==========================================
	// 🚦 PRE-PARSE SEPARATORS ONCE FOR SPEED 🚦
	// ==========================================
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

	// --- 1. BRUTE-FORCE EXTRACTION ---
	var startMsgWords []string
	if config.User.StartMsg && config.User.StartMsgText != "" {
		numStartTokens := len(strings.Fields(config.User.StartMsgText))
		if len(words) >= numStartTokens {
			startMsgWords = words[:numStartTokens]
			words = words[numStartTokens:]
		}
	}

	var endMsgWords []string
	if config.User.EndMsg && config.User.EndMsgText != "" {
		numEndTokens := len(strings.Fields(config.User.EndMsgText))
		if len(words) >= numEndTokens {
			endMsgWords = words[len(words)-numEndTokens:]
			words = words[:len(words)-numEndTokens]
		}
	}

	// Now shuffle ONLY the core text
	if config.User.RandomOrder {
		r.Shuffle(len(words), func(i, j int) {
			words[i], words[j] = words[j], words[i]
		})
	}

	// ==========================================
	// 🚦 TEXT BUILDER LOGIC 🚦
	// ==========================================
	if config.User.TextBuilder {
		count := config.User.TextWordCount
		if count < 2 {
			count = 2 // Safety fallback
		}

		sep := strings.TrimSpace(config.User.TextSeparator)
		var textBuilderWords []string

		for start := 0; start < len(words); start += count {
			end := start + count
			if end > len(words) {
				end = len(words)
			}

			chunk := words[start:end]

			// Pyramid logic for the current chunk
			for i := 0; i < len(chunk); i++ {
				for j := 0; j <= i; j++ {
					textBuilderWords = append(textBuilderWords, chunk[j])
				}
			}

			// Append TextSeparator after the chunk, if it's not the very last chunk
			if sep != "" && end < len(words) {
				// Split by spaces just in case the user entered multiple separated prosigns like "<BT> <AR>"
				sepTokens := strings.Fields(sep)
				textBuilderWords = append(textBuilderWords, sepTokens...)
			}
		}

		// Replace the core words array with our new flattened pyramid sequence
		words = textBuilderWords
	}

	playlist := []PlayContext{}

	// Helper function to safely clean and queue words bypassing WordBuilder
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

	// 2. Queue the Start Message natively
	for _, w := range startMsgWords {
		queueRawWord(w)
	}

	// 3. Queue the Main Text (With Filters)
	for _, rawWord := range words {

		if IsStopping {
			return
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

		var finalWord = w
		var isIwrMatchFound bool

		if config.User.RandomWords && len(w) > 1 && !isProsign {
			finalWord = shuffleWord(w, r)
			isIwrMatchFound = false
		} else {
			isIwrMatchFound = isIWRMatch(w)
		}

		// ==========================================
		// 🚦 CONSECUTIVE SEPARATOR PREVENTION 🚦
		// ==========================================
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

		// If the text file itself provided a separator, AND the last item was already a separator,
		// drop this word entirely to prevent "3 delimiters in a row".
		if isCurrentSep && lastIsSep {
			continue
		}

		// ==========================================
		// 🚦 FIXED WORD BUILDER APPEND LOGIC 🚦
		// ==========================================
		if config.User.WordBuilder && len(w) > 1 && !isProsign {
			// 1. The buildup -> T, TH
			for i := 1; i < len(w); i++ {
				playlist = append(playlist, PlayContext{
					Word:     w[:i],
					IsIWR:    false,
					HideText: false, // Visible
				})
			}

			// 2. The first full standard play -> THE
			playlist = append(playlist, PlayContext{
				Word:     finalWord,
				IsIWR:    false,
				HideText: false, // Visible
			})

			// 3. The standard full repeat -> THE
			// Word Builder ALWAYS plays the final word twice at normal speed.
			playlist = append(playlist, PlayContext{
				Word:     finalWord,
				IsIWR:    false,
				HideText: false, // Visible
			})

			// 4. ONE extra full word at IWR speed -> THE
			// Special interaction: if IWR is enabled, tack on one more play really fast.
			if config.User.IWREnabled {
				playlist = append(playlist, PlayContext{
					Word:     finalWord,
					IsIWR:    true,  // Fast!
					HideText: false, // Visible
				})
			}

		} else {
			// Standard isolated append (when WordBuilder is OFF)
			playlist = append(playlist, PlayContext{
				Word:     finalWord,
				IsIWR:    isIwrMatchFound,
				HideText: false,
			})
		}

		// ==========================================
		// 🚦 DYNAMIC SEPARATOR APPEND LOGIC 🚦
		// ==========================================
		if config.User.WordBuilder && len(validSeparators) > 0 {
			// Re-verify the last item in the playlist just in case the word we just appended WAS a separator.
			lastItem := playlist[len(playlist)-1].Word
			lastIsSep = false
			for _, sep := range validSeparators {
				if lastItem == sep {
					lastIsSep = true
					break
				}
			}

			if !lastIsSep {
				selectedSep := validSeparators[r.Intn(len(validSeparators))] // Using perfectly seeded 'r'
				playlist = append(playlist, PlayContext{
					Word:     selectedSep,
					IsIWR:    false,
					HideText: false, // Visible
				})
			}
		}
	}

	// 4. Queue the End Message natively
	for _, w := range endMsgWords {
		queueRawWord(w)
	}

	// ==========================================
	// 🚦 PHASE 2: AUDIO PLAYBACK ROUTING 🚦
	// ==========================================
	if config.User.UseWave {
		if OnStatusUpdate != nil {
			OnStatusUpdate(" [yellow]Generating .wav file...")
		}

		speedBlock := getSpeedBlock(config.User.CharacterSpeed, config.User.EffectiveSpeed)
		fileName := fmt.Sprintf("yama_output_%s.wav", speedBlock)

		err := RenderWAVToFile(playlist, fileName, 11025)

		if err != nil {
			if OnStatusUpdate != nil {
				OnStatusUpdate(" [red]Error generating .wav!")
			}
		} else {
			if OnStatusUpdate != nil {
				OnStatusUpdate(fmt.Sprintf(" [#00FF00]Saved as %s", fileName))
			}
		}

		go func() {
			time.Sleep(2 * time.Second)
			if OnStatusUpdate != nil {
				OnStatusUpdate("STOP")
			}
		}()

		return
	}

	// Start the standard audio loop
	for i, ctx := range playlist {

		for IsPaused && !IsStopping {
			time.Sleep(100 * time.Millisecond)
		}

		if IsStopping {
			break
		}

		tempConf := config.User

		totalWords := len(playlist)
		if tempConf.EndSpeed > tempConf.CharacterSpeed && totalWords > 1 {
			progress := float64(i) / float64(totalWords-1)
			charSpd := tempConf.CharacterSpeed + (tempConf.EndSpeed-tempConf.CharacterSpeed)*progress
			multiplier := charSpd / tempConf.CharacterSpeed

			tempConf.CharacterSpeed = charSpd
			tempConf.EffectiveSpeed = tempConf.EffectiveSpeed * multiplier
			tempConf.IWRSpeed = tempConf.IWRSpeed * multiplier
		}

		baseProfile := GetTiming(false, tempConf)
		iwrProfile := GetTiming(true, tempConf)

		var p TimingProfile
		if ctx.IsIWR {
			p = iwrProfile
		} else {
			p = baseProfile
		}

		buildWordBuffer(ctx, p)

		wordSpaceSamples := int(p.WordSpace * float64(SampleRate))
		QueuePCM(SilencePCM(wordSpaceSamples).Samples, " ", -1)

		Flush()

		if OnWordPlayed != nil {
			isIWRMatch := iwrMan.Match(ctx.Word)
			OnWordPlayed(ctx.Word, isIWRMatch)
		}
	}

	IsPaused = false
	if OnStatusUpdate != nil {
		OnStatusUpdate("STOP")
	}
}
