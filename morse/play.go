package morse

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"
	"yama/config"
)

// Add IsPaused so the engine can halt in place without losing its index
var IsPaused bool
var OnWordPlayed func(word string, isIWR bool)
var OnGroupCompleted func([]string)
var OnClearFlashcardScreen func()

// OnWordChange is now triggered inside audio.go's Flush loop
var OnWordChange func(char string, index int)
var OnStatusUpdate func(msg string)

type PlayContext struct {
	Word     string
	IsIWR    bool
	HideText bool
}

var (
	// Unexported state variables (hidden from the UI)
	waitActionChan  = make(chan rune, 1)
	isWaitingForKey bool
	waitMutex       sync.RWMutex
)

// IsWaitingForUserReturn safely checks if the engine is paused waiting for a key.
func IsWaitingForUserKey() bool {
	waitMutex.RLock()
	defer waitMutex.RUnlock()
	return isWaitingForKey
}

// SignalUserReturn sends a non-blocking signal to unpause the engine.
func SignalUserKey(key rune) {
	select {
	case waitActionChan <- key:
	default: // Don't block if the channel is already full
	}
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

	var startMsgWords []string
	if config.User.StartMsg && config.User.StartMsgText != "" {
		startMsgWords = strings.Fields(config.User.StartMsgText)
	}

	var endMsgWords []string
	if config.User.EndMsg && config.User.EndMsgText != "" {
		endMsgWords = strings.Fields(config.User.EndMsgText)
	}

	// Now shuffle ONLY the core text
	if config.User.WordOrder {
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

		// ==========================
		// 🚦 (SYLLABLE EXPANSION) 🚦
		// ==========================
		// exclusive with WordBuilder & RandomizeWords
		
		if config.User.SylableExpansion && !isProsign {
			
			if expanded, exists := sylables[w]; exists {
				
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

		if isCurrentSep && lastIsSep {
			continue
		}

		// ==========================================
		// 🚦 FIXED WORD BUILDER APPEND LOGIC 🚦
		// ==========================================
		if config.User.WordBuilder && len(w) > 1 && !isProsign {
			for i := 1; i < len(w); i++ {
				playlist = append(playlist, PlayContext{
					Word:     w[:i],
					IsIWR:    false,
					HideText: false,
				})
			}

			playlist = append(playlist, PlayContext{
				Word:     finalWord,
				IsIWR:    false,
				HideText: false,
			})

			playlist = append(playlist, PlayContext{
				Word:     finalWord,
				IsIWR:    false,
				HideText: false,
			})

			if config.User.IWREnabled {
				playlist = append(playlist, PlayContext{
					Word:     finalWord,
					IsIWR:    true,
					HideText: false,
				})
			}

		} else {
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
			lastItem := playlist[len(playlist)-1].Word
			lastIsSep = false
			for _, sep := range validSeparators {
				if lastItem == sep {
					lastIsSep = true
					break
				}
			}

			if !lastIsSep {
				selectedSep := validSeparators[r.Intn(len(validSeparators))]
				playlist = append(playlist, PlayContext{
					Word:     selectedSep,
					IsIWR:    false,
					HideText: false,
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
	lastGroup := []string{} // for Flashcard

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

	// ==========================================
	// 🚦 PHASE 3: LIVE AUDIO LOOP WITH GROUPING 🚦
	// ==========================================
	pauseCounter := 0
	groupSize := config.User.FlashcardWordCount
	if groupSize <= 0 {
		groupSize = 1
	}

	for i := 0; i < len(playlist); i++ {
		ctx := playlist[i]

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

		// Play this word once
		buildWordBuffer(ctx, p)

		wordSpaceSamples := int(p.WordSpace * float64(SampleRate))
		QueuePCM(SilencePCM(wordSpaceSamples).Samples, " ", -1)

		Flush()

		if OnWordPlayed != nil {
			isIWRMatch := iwrMan.Match(ctx.Word)
			OnWordPlayed(ctx.Word, isIWRMatch)
		}

		// Track logical words for flashcard group
		pauseCounter++

		if pauseCounter == 1 {
			lastGroup = []string{}
		}

		lastGroup = append(lastGroup, ctx.Word)

		// Grouping / flashcard logic
		if config.User.RequireReturnAfterWord && (pauseCounter >= groupSize || i == len(playlist)-1) {

			// GROUP COMPLETE (even short final group)
			if OnGroupCompleted != nil {
				OnGroupCompleted(lastGroup)
			}

			// drain stale actions
			select {
			case <-waitActionChan:
			default:
			}

			waitMutex.Lock()
			isWaitingForKey = true
			OnStatusUpdate(" [yellow]Flashcard: ENTER to continue, BACKSPACE to repeat group")
			waitMutex.Unlock()

			var action rune

		WaitLoop:
			for !IsStopping && !IsPaused {
				select {
				case action = <-waitActionChan:
					break WaitLoop
				case <-time.After(50 * time.Millisecond):
				}
			}

			waitMutex.Lock()
			isWaitingForKey = false
			OnStatusUpdate(" [green]Playing")
			waitMutex.Unlock()

			// NOW reset pauseCounter — AFTER the wait
			pauseCounter = 0

			if action == 'B' {
				// Rewind to the START of the CURRENT group
				rewindLen := len(lastGroup)     // how many words in this group
				startIndex := i - rewindLen + 1 // index of first word in this group

				if startIndex < 0 {
					startIndex = 0
				}

				// for-loop will i++ next, so set to one before startIndex
				i = startIndex - 1
			} else {
				if OnClearFlashcardScreen != nil {
					OnClearFlashcardScreen()
				}
			}

			// If final word and ENTER pressed, exit
			if i == len(playlist)-1 && action != 'B' {
				break
			}
		}

	}

	IsPaused = false
	if OnStatusUpdate != nil {
		OnStatusUpdate("STOP")
	}
}
