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

	words := strings.Fields(text)

	// --- 1. BRUTE-FORCE EXTRACTION ---
	// Unconditionally slice the Start/End messages off the array based on
	// their config length so WordBuilder and RandomOrder cannot touch them.
	var startMsgWords []string
	if config.User.StartMsg && config.User.StartMsgText != "" {
		numStartTokens := len(strings.Fields(config.User.StartMsgText))
		if len(words) >= numStartTokens {
			startMsgWords = words[:numStartTokens]
			words = words[numStartTokens:] // Remove from main processing
		}
	}

	var endMsgWords []string
	if config.User.EndMsg && config.User.EndMsgText != "" {
		numEndTokens := len(strings.Fields(config.User.EndMsgText))
		if len(words) >= numEndTokens {
			endMsgWords = words[len(words)-numEndTokens:]
			words = words[:len(words)-numEndTokens] // Remove from main processing
		}
	}

	// Now shuffle ONLY the core text
	if config.User.RandomOrder {
		r.Shuffle(len(words), func(i, j int) {
			words[i], words[j] = words[j], words[i]
		})
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
				IsIWR:    false, // Control messages never trigger IWR speed bursts
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
					r := []rune(strings.ToUpper(t))[0]
					if _, ok := MorseTable[r]; ok {
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

		// Apply WordBuilder ONLY to actual words (ignore Prosigns)
		if config.User.WordBuilder && len(w) > 1 && !isProsign {

			// 1. The buildup -> T, TH (Hidden from UI)
			for i := 1; i < len(w); i++ {
				playlist = append(playlist, PlayContext{
					Word:     w[:i],
					IsIWR:    false,
					HideText: true,
				})
			}

			// 2. The first full standard play -> THE (Visible on UI)
			playlist = append(playlist, PlayContext{
				Word:     w,
				IsIWR:    false,
				HideText: false,
			})

			// 3. The purposeful repeat!
			// If IWR is on, it plays fast. If off, it plays standard.
			playlist = append(playlist, PlayContext{
				Word:     w,
				IsIWR:    config.User.IWREnabled, // Automatically toggles speed!
				HideText: true,                   // Keeps the UI from printing it twice
			})

			continue
		}

		var finalWord = w
		var isIwrMatchFound bool

		if config.User.RandomWords && len(w) > 1 && !isProsign {
			finalWord = shuffleWord(w, r)
			isIwrMatchFound = false
		} else {
			isIwrMatchFound = isIWRMatch(w)
		}

		playlist = append(playlist, PlayContext{
			Word:     finalWord,
			IsIWR:    isIwrMatchFound,
			HideText: false,
		})
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

		// Pass the speeds and ramping boolean directly to our helper
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

		// 1. SLEEP LOCK: If paused, just hang out here.
		for IsPaused && !IsStopping {
			time.Sleep(100 * time.Millisecond)
		}

		// 2. STOP LOCK: If they hit stop (or closed the app), bail out immediately.
		if IsStopping {
			break
		}

		// 3. FRESH MATH: Grab the user config
		tempConf := config.User

		// --- SPEED RAMPING LOGIC ---
		totalWords := len(playlist)
		if tempConf.EndSpeed > tempConf.CharacterSpeed && totalWords > 1 {
			// Calculate progress from 0.0 (first word) to 1.0 (last word)
			progress := float64(i) / float64(totalWords-1)

			// Calculate the new instantaneous Character Speed
			charSpd := tempConf.CharacterSpeed + (tempConf.EndSpeed-tempConf.CharacterSpeed)*progress

			// Calculate the exact multiplier to keep the other speeds proportional
			multiplier := charSpd / tempConf.CharacterSpeed

			// Apply the multiplier to Effective and IWR speeds
			tempConf.CharacterSpeed = charSpd
			tempConf.EffectiveSpeed = tempConf.EffectiveSpeed * multiplier
			tempConf.IWRSpeed = tempConf.IWRSpeed * multiplier
		}
		// ---------------------------

		// Build the profiles using our freshly calculated speeds!
		baseProfile := GetTiming(false, tempConf)
		iwrProfile := GetTiming(true, tempConf)

		var p TimingProfile
		if ctx.IsIWR {
			p = iwrProfile
		} else {
			p = baseProfile
		}

		// 4. PLAY THE WORD
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
