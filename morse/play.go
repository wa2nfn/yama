package morse

import (
	"log"
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
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	log.Printf("=== ENGINE SPURT | Mode: Std:%v Farns:%v Words:%v | CharSpd: %d | EffSpd: %d | Tone: %dHz | IWR:%v (%dwpm) ===",
		config.User.UseStandard, config.User.UseFarnsworth, config.User.UseWordsworth,
		config.User.CharacterSpeed, config.User.EffectiveSpeed, config.User.Tone,
		config.User.IWREnabled, config.User.IWRSpeed)

	words := strings.Fields(text)

	if config.User.RandomOrder {
		r.Shuffle(len(words), func(i, j int) {
			words[i], words[j] = words[j], words[i]
		})
	}

	playlist := []PlayContext{}

	// 2. Queue the Main Text
	for _, rawWord := range words {

		// The Pause Trap!
		for IsPaused {
			time.Sleep(100 * time.Millisecond) // Wait gracefully
			if IsStopping {
				return // Escape hatch if they hit Stop while Paused
			}
		}

		if IsStopping {
			return
		}

		// === THE FUNNEL ===
		// Wash the word token-by-token so we don't destroy prosigns!
		var cleanBuilder strings.Builder
		for _, t := range Tokenize(rawWord) {
			if strings.HasPrefix(t, "<") && strings.HasSuffix(t, ">") {
				// Prosign Check
				lookup := strings.ToUpper(t[1 : len(t)-1])
				if _, ok := ProSignTable[lookup]; ok {
					cleanBuilder.WriteString(t)
				}
			} else {
				// Character Check
				if len(t) > 0 {
					r := []rune(strings.ToUpper(t))[0]
					if _, ok := MorseTable[r]; ok {
						cleanBuilder.WriteString(t) // Keep original casing for the UI
					}
				}
			}
		}
		w := cleanBuilder.String()

		// If the word was entirely made of skipped characters, skip it.
		if len(strings.TrimSpace(w)) == 0 {
			continue
		}

		isProsign := strings.HasPrefix(w, "<") && strings.HasSuffix(w, ">")

		// Apply WordBuilder ONLY to actual words (ignore Prosigns)
		if config.User.WordBuilder && len(w) > 1 && !isProsign {
			// Now that 'w' is already clean, we just build the sequence natively.

			// The buildup -> O, OO (Hidden from UI)
			for i := 1; i < len(w); i++ {
				playlist = append(playlist, PlayContext{
					Word:     w[:i],
					IsIWR:    false,
					HideText: true,
				})
			}
			// The final standard play -> OO, (Visible on UI)
			playlist = append(playlist, PlayContext{
				Word:     w,
				IsIWR:    false,
				HideText: false,
			})
			// The fast IWR Blast -> OO (Hidden from UI)
			if config.User.IWREnabled {
				playlist = append(playlist, PlayContext{
					Word:     w,
					IsIWR:    true,
					HideText: true,
				})
			}
			continue
		}

		// Standard play for non-WordBuilder words, single chars, or Prosigns
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

	// ==========================================
	// 🚦 THE CORRECTED TRAFFIC COP GOES HERE 🚦
	// ==========================================
	if config.User.UseWave {
		log.Println("Wave flag enabled. Routing output to .wav file...")
		if OnStatusUpdate != nil {
			OnStatusUpdate(" [yellow]Generating .wav file...")
		}

		// Pass the raw playlist array directly!
		// Fixed wave SampleRate to 11025 to save size
		err := RenderWAVToFile(playlist, "yama_output.wav", 11025)

		if err != nil {
			log.Printf("WAV Generation Error: %v", err)
			if OnStatusUpdate != nil {
				OnStatusUpdate(" [red]Error generating .wav!")
			}
		} else {
			if OnStatusUpdate != nil {
				OnStatusUpdate(" [#00FF00]WAV file saved successfully!")
			}
		}

		// FIX: Give the user 2 seconds to read the message before unlocking the UI
		go func() {
			time.Sleep(2 * time.Second)
			if OnStatusUpdate != nil {
				OnStatusUpdate("STOP")
			}
		}()

		return // Exit before we hit the real-time Oto playback!
	}

	// --- PRE-CALCULATE TIMING PROFILES ---
	baseProfile := GetTiming(false, config.User)
	iwrProfile := GetTiming(true, config.User)

	for _, ctx := range playlist {
		if IsStopping {
			break
		}

		// The Pause Trap!
		wasPaused := false
		for IsPaused && !IsStopping {
			wasPaused = true
			time.Sleep(100 * time.Millisecond)
		}

		if IsStopping {
			break
		}

		// Rebuild profiles ONLY if we just woke up from a pause
		if wasPaused {
			baseProfile = GetTiming(false, config.User)
			iwrProfile = GetTiming(true, config.User)
		}

		// Select the correct pre-calculated profile instantly
		var p TimingProfile
		if ctx.IsIWR {
			p = iwrProfile
		} else {
			p = baseProfile
		}

		buildWordBuffer(ctx, p)

		// Queue the exact WordSpace
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
		OnStatusUpdate("STOP") // let UI know
	}
}
