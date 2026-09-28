package morse

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
	"yama/config"

	"go.bug.st/serial"
)

var IsPaused bool
var OnWordPlayed func(word string, isIWR bool)
var OnGroupCompleted func([]string)
var OnClearFlashcardScreen func()
var OnEchoStart func()
var OnEchoEnd func()
var OnStatusUpdate func(msg string)
var ResponseMS int = 2000 // the user time to hear the starters gun and actually key
var ShowBlueLineTolerance func()
var OnEchoCharDecoded func(char string)
var OnWordChange func(char string, index int)
var ActiveEchoPort serial.Port
var SessionStats EchoStats
var OnEchoStatsUpdated func(groupStats EchoStats, sessionStats EchoStats)
var OnEchoRuntimeFailure func(reason string)

func CloseHardwarePort() {
	if ActiveEchoPort != nil {
		ActiveEchoPort.Close()
		ActiveEchoPort = nil
	}
}

type PlayContext struct {
	Word     string
	IsIWR    bool
	HideText bool
}

var (
	waitActionChan  = make(chan rune, 1)
	isWaitingForKey bool
	waitMutex       sync.RWMutex
)

func IsWaitingForUserKey() bool {
	waitMutex.RLock()
	defer waitMutex.RUnlock()
	return isWaitingForKey
}

func SignalUserKey(key rune) {
	select {
	case waitActionChan <- key:
	default:
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
		if mgr.Match(word) {
			return true
		}
	}

	if strings.HasPrefix(word, "<") && strings.HasSuffix(word, ">") {
		return false
	}

	return false
}

func buildWordBuffer(ctx PlayContext, p TimingProfile) int {
	tokens := Tokenize(ctx.Word)

	for i, token := range tokens {
		if IsStopping {
			return 0
		}

		var pattern string
		var ok bool

		if strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">") {
			lookup := strings.ToUpper(token[1 : len(token)-1])
			pattern, ok = ProSignTable[lookup]
		} else {
			r := []rune(token)[0]
			pattern, ok = MorseTable[r]
		}

		if !ok {
			continue
		}

		isFirstElement := true
		for j, symbol := range pattern {
			dur := p.DitDuration
			if symbol == '-' {
				dur = p.DahDuration
			}

			label := ""
			if isFirstElement {
				label = token
				isFirstElement = false
			}

			if config.User.Mute { // Your new UI checkbox flag
				// INSTANT UI UPDATE: Send 1 sample of silence just to carry the text label to the UI instantly
				if label != "" {
					QueuePCM(SilencePCM(1).Samples, label, i)
				}
			} else {
				// NORMAL YAMA AUDIO
				QueuePCM(TonePCM(float64(p.Tone), int(dur*float64(SampleRate)), 0.5).Samples, label, i)

				if j < len(pattern)-1 {
					QueuePCM(SilencePCM(int(p.InterElement*float64(SampleRate))).Samples, "", i)
				}
			}
		}

		if i < len(tokens)-1 {
			if !config.User.Mute {
				QueuePCM(SilencePCM(int(p.CharSpace*float64(SampleRate))).Samples, "", i)
			}
		}

	}

	totalDurationSec := 0.0
	for i, token := range tokens {
		var pattern string
		var ok bool

		if strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">") {
			lookup := strings.ToUpper(token[1 : len(token)-1])
			pattern, ok = ProSignTable[lookup]
		} else {
			r := []rune(token)[0]
			pattern, ok = MorseTable[r]
		}

		if !ok {
			continue
		}

		for j, symbol := range pattern {
			dur := p.DitDuration
			if symbol == '-' {
				dur = p.DahDuration
			}
			totalDurationSec += dur

			if j < len(pattern)-1 {
				totalDurationSec += p.InterElement
			}
		}

		if i < len(tokens)-1 {
			totalDurationSec += p.CharSpace
		}
	}

	totalDurationSec += p.WordSpace
	return int(totalDurationSec * 1000.0)
}

func RunIWR(text string, iwrMan *IWRManager) {
	defer CloseHardwarePort() // Safety net
	var idleInputState bool
	var isFirstEchoGroup bool = true

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	if config.User.Echo {
		EchoMap = make(map[string]string)
		for k, v := range EchoMapBase {
			EchoMap[k] = v
		}

		if config.User.UseEuropeanChars {
			for k, v := range EchoMapEuropean {
				//EuropeanSkipList  is a string runes
				// if v is in the Map delete that key
				// unless the VALUE is
				EchoMap[k] = v
			}
		}

		if config.User.Playprosigns {
			for k, v := range EchoMapProsigns {
				EchoMap[k] = v
			}
		}
		if config.User.UseExtendedPunctuation {
			for k, v := range EchoMapExtended {
				EchoMap[k] = v
			}
		}

		//// PORT SETUP START
		if config.User.KeyerPort == "" {
			config.User.Echo = false
			config.SaveConfig()
			if OnStatusUpdate != nil {
				OnStatusUpdate("[red] KeyEcho requires a connected device - KeyEcho now disabled.")
			}
			return
		}
		// 1. WAKE UP HARDWARE IMMEDIATELY BEFORE KEYING
		if ActiveEchoPort != nil {
			ActiveEchoPort.Close()
		}

		mode := &serial.Mode{
			BaudRate: config.User.KeyerPortSpeed,
			Parity:   serial.NoParity,
			StopBits: serial.OneStopBit,
			DataBits: 8,
		}

		var openErr error
		ActiveEchoPort, openErr = serial.Open(config.User.KeyerPort, mode)

		if openErr != nil {
			if OnEchoRuntimeFailure != nil {
				OnEchoRuntimeFailure("port-failure")
			}
			return
		}

		if openErr == nil {

			// ROBUST BUFFER CLEARING
			_ = ActiveEchoPort.ResetInputBuffer()
			_ = ActiveEchoPort.ResetOutputBuffer()

			// 1. Determine active output based on the 8-option dropdown
			var useDTR, useRTS bool
			switch config.User.KeyLineMode {
			case "CTS:8-DTR:4", "DSR:6-DTR:4", "CD:1-DTR:4", "RI:9-DTR:4":
				useDTR = true
				useRTS = false
			case "CTS:8-RTS:7", "DSR:6-RTS:7", "CD:1-RTS:7", "RI:9-RTS:7":
				useDTR = false
				useRTS = true
			default: // Fallback safety
				useDTR = true
				useRTS = false
			}

			// 2. Apply Parasitic Power if checked
			if config.User.KeyParasiticPower {
				useDTR = true
				useRTS = true
			}

			// 3. Set pins explicitly
			_ = ActiveEchoPort.SetDTR(useDTR)
			_ = ActiveEchoPort.SetRTS(useRTS)

			time.Sleep(500 * time.Millisecond) // Let power stabilize

		} else {
			if OnStatusUpdate != nil {
				OnStatusUpdate(fmt.Sprintf(" [red]Keyer Port Error: %v", openErr))
			}
		}
	}

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
		r.Shuffle(len(words), func(i, j int) {
			words[i], words[j] = words[j], words[i]
		})
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
			finalWord = shuffleWord(w, r)
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
				selectedSep := validSeparators[r.Intn(len(validSeparators))]
				playlist = append(playlist, PlayContext{Word: selectedSep, IsIWR: false, HideText: false})
			}
		}
	}

	for _, w := range endMsgWords {
		queueRawWord(w)
	}

	lastGroup := []string{}

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

	pauseCounter := 0

	groupSize := 1
	if config.User.Flashcard {
		if config.User.FlashRandomCount {
			groupSize = rand.Intn(config.User.FlashWordCount) + 1
		} else {
			groupSize = config.User.FlashWordCount
		}
	} else if config.User.Echo {
		if config.User.EchoRandomCount {
			groupSize = rand.Intn(config.User.EchoWordCount) + 1
		} else {
			groupSize = config.User.EchoWordCount
		}
	}

	var groupStats EchoStats
	var messageDuration int

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

		// 1. Get the actual user profiles (preserves Wordsworth and IWR)
		baseProfile := GetTiming(false, tempConf)
		iwrProfile := GetTiming(true, tempConf)

		var p TimingProfile

		if config.User.Echo {
			// ECHO MODE: Force Standard audio, ignore IWR
			audioConf := tempConf
			audioConf.UseFarnsworth = false
			audioConf.UseWordsworth = false
			audioConf.UseStandard = true

			p = GetTiming(false, audioConf)
		} else {
			// NORMAL PLAYBACK: Respect the user's settings and IWR!
			if ctx.IsIWR {
				p = iwrProfile
			} else {
				p = baseProfile
			}
		}

		// 3. Generate the audio (Snappy for Echo, Dynamic for Normal)
		messageDuration += buildWordBuffer(ctx, p)

		nextPauseCounter := pauseCounter + 1
		isGoingToEcho := config.User.Echo &&
			(nextPauseCounter >= groupSize || i == len(playlist)-1)

		wordSpaceSamples := int(p.WordSpace * float64(SampleRate))
		if !isGoingToEcho {
			QueuePCM(SilencePCM(wordSpaceSamples).Samples, " ", -1)

		}

		Flush()

		if OnWordPlayed != nil {
			isIWRMatch := iwrMan.Match(ctx.Word)
			OnWordPlayed(ctx.Word, isIWRMatch)
		}

		pauseCounter++

		if pauseCounter == 1 {
			lastGroup = []string{}
		}

		lastGroup = append(lastGroup, ctx.Word)

		if config.User.Echo && (pauseCounter >= groupSize || i == len(playlist)-1) {
			if OnEchoStart != nil {
				OnEchoStart()
				ShowBlueLineTolerance()
			}

			// 2. RUN THE DECODER
			var res EchoResult
			var err error

			idleInputState = config.User.KeyLineIdlePolarity

			if ActiveEchoPort != nil {
				// 1. GUARANTEE standard word spacing before listening, for ALL groups
				//QueuePCM(SilencePCM(wordSpaceSamples).Samples, " ", -1)
				alertSamples := int(p.CharSpace * 2.0 * float64(SampleRate))
				//QueuePCM(SilencePCM(alertSamples).Samples, " ", -1)

				// 2. Conditionally queue the alert sound based on UI: First(0), None(1), All(2)
				switch config.User.Alert {
				case 0: // First Group Only
					if isFirstEchoGroup {
						alertBytes := generateDoneAlert(alertSamples, float64(p.DitDuration*0.6))
						QueuePCM(alertBytes, " ", -1)
						isFirstEchoGroup = false
					}

				case 1: // None
					// Do nothing extra

				case 2: // All Groups
					alertBytes := generateDoneAlert(SampleRate, float64(p.DitDuration))
					QueuePCM(alertBytes, " ", -1)
				}

				// 3. FLUSH universally so the padding (and any alert) actually plays before RunEcho starts!
				Flush()

				effectiveResponseMS := ResponseMS
				if config.User.Mute {
					effectiveResponseMS += 1000 // since no audible que give user more time
				}

				// But pass the relaxed user profile (which has Wordsworth) to the grader!
				res, err = RunEcho(
					lastGroup,
					messageDuration,
					effectiveResponseMS,
					baseProfile, // <-- Still contains Wordsworth!
					ActiveEchoPort,
					idleInputState,
				)
			} else {
				err = fmt.Errorf("serial port not available")
				res = EchoResult{Success: false, Error: "no keyer"}
			}

			if OnEchoEnd != nil {
				OnEchoEnd()
			}
			messageDuration = 0

			var action rune
			if err == nil && res.Success {
				if config.User.PerfectPause {
					// moment to bask in glory
					if OnStatusUpdate != nil {
						OnStatusUpdate(" [green]Perfect Match!       [yellow]ENTER to continue, BACKSPACE to repeat.")
					}
					time.Sleep(2000 * time.Millisecond)
				}

				expectedRaw := strings.ToUpper(strings.Join(lastGroup, " "))
				actualRaw := strings.ToUpper(strings.Join(res.Chars, ""))

				expectedStr := strings.Join(strings.Fields(expectedRaw), " ")
				actualStr := strings.Join(strings.Fields(actualRaw), " ")
				actualCharCount := len(strings.ReplaceAll(actualStr, " ", ""))

				// MASSIVE DATA ACCUMULATOR
				groupStats.ShortDits += res.Stats.ShortDits
				groupStats.LongDits += res.Stats.LongDits
				groupStats.PerfectDits += res.Stats.PerfectDits
				groupStats.ShortDahs += res.Stats.ShortDahs
				groupStats.LongDahs += res.Stats.LongDahs
				groupStats.PerfectDahs += res.Stats.PerfectDahs
				groupStats.ShortElementGaps += res.Stats.ShortElementGaps
				groupStats.LongElementGaps += res.Stats.LongElementGaps
				groupStats.PerfectElementGaps += res.Stats.PerfectElementGaps
				groupStats.ShortCharGaps += res.Stats.ShortCharGaps
				groupStats.LongCharGaps += res.Stats.LongCharGaps
				groupStats.PerfectCharGaps += res.Stats.PerfectCharGaps
				groupStats.ShortWordGaps += res.Stats.ShortWordGaps
				groupStats.LongWordGaps += res.Stats.LongWordGaps
				groupStats.PerfectWordGaps += res.Stats.PerfectWordGaps
				groupStats.InvalidSymbols += res.Stats.InvalidSymbols

				groupStats.TotalChars += actualCharCount
				groupStats.TotalWords += res.Stats.TotalWords

				groupStats.SumDitMs += res.Stats.SumDitMs
				groupStats.SumDahMs += res.Stats.SumDahMs
				groupStats.SumElementGapsMs += res.Stats.SumElementGapsMs
				groupStats.SumCharGapsMs += res.Stats.SumCharGapsMs
				groupStats.SumWordGapsMs += res.Stats.SumWordGapsMs

				SessionStats.ShortDits += res.Stats.ShortDits
				SessionStats.LongDits += res.Stats.LongDits
				SessionStats.PerfectDits += res.Stats.PerfectDits
				SessionStats.ShortDahs += res.Stats.ShortDahs
				SessionStats.LongDahs += res.Stats.LongDahs
				SessionStats.PerfectDahs += res.Stats.PerfectDahs
				SessionStats.ShortElementGaps += res.Stats.ShortElementGaps
				SessionStats.LongElementGaps += res.Stats.LongElementGaps
				SessionStats.PerfectElementGaps += res.Stats.PerfectElementGaps
				SessionStats.ShortCharGaps += res.Stats.ShortCharGaps
				SessionStats.LongCharGaps += res.Stats.LongCharGaps
				SessionStats.PerfectCharGaps += res.Stats.PerfectCharGaps
				SessionStats.ShortWordGaps += res.Stats.ShortWordGaps
				SessionStats.LongWordGaps += res.Stats.LongWordGaps
				SessionStats.PerfectWordGaps += res.Stats.PerfectWordGaps
				SessionStats.InvalidSymbols += res.Stats.InvalidSymbols

				SessionStats.TotalChars += actualCharCount
				SessionStats.TotalWords += res.Stats.TotalWords

				SessionStats.SumDitMs += res.Stats.SumDitMs
				SessionStats.SumDahMs += res.Stats.SumDahMs
				SessionStats.SumElementGapsMs += res.Stats.SumElementGapsMs
				SessionStats.SumCharGapsMs += res.Stats.SumCharGapsMs
				SessionStats.SumWordGapsMs += res.Stats.SumWordGapsMs

				if OnEchoStatsUpdated != nil {
					OnEchoStatsUpdated(groupStats, SessionStats)
				}

				if expectedStr != actualStr {
					groupStats.Retries++
					SessionStats.Retries++
				}

				if expectedStr == actualStr {
					if OnClearFlashcardScreen != nil {
						OnClearFlashcardScreen()
					}
					groupStats = EchoStats{} // Reset for the next group
					pauseCounter = 0
					lastGroup = lastGroup[:0]
					continue
				}

				if OnStatusUpdate != nil {
					OnStatusUpdate(" [red]MISMATCH!       [yellow]ENTER to continue, BACKSPACE to repeat.")
				}

			} else if res.Error == "no start" {
				if OnStatusUpdate != nil {
					OnStatusUpdate(" [red]NO INPUT DETECTED.       [yellow]ENTER to continue, BACKSPACE to repeat.")
				}
			} else if res.Error == "too slow" {
				if OnStatusUpdate != nil {
					OnStatusUpdate(" [red]TIMED OUT!        [yellow]ENTER to continue, BACKSPACE to repeat.")
				}
			} else if res.Error == "no keyer" {
				if OnStatusUpdate != nil {
					OnStatusUpdate(" [red]NO COM PORT CABLE DETECTED!       [yellow]ENTER/BACKSPACE to repeat.")
				}
			} else if res.Error == "farnsworth option error" {
				if OnStatusUpdate != nil {
					OnStatusUpdate(" [red]Farnsworth not supported.[-:-:-] ")
				}
			} else if res.Error == "auto_retry" {
				if OnStatusUpdate != nil {
					OnStatusUpdate(" [red]MISMATCH!       [yellow]Auto-restarting...")
				}
				time.Sleep(300 * time.Millisecond)

				errOsc, errPlayer := StartOscillator(float64(config.User.AlertTone))
				if errPlayer != nil && errOsc != nil && config.User.ErrorTone {
					errPlayer.SetVolume(0.75)
					time.Sleep(time.Duration(0.5 * p.DahDuration * float64(time.Second)))
					errPlayer.SetVolume(0.0)
					errPlayer.Pause()
					errPlayer.Close()
				}

				// Tweak between 700ms and as needed
				time.Sleep(700 * time.Millisecond)

				action = 'B'

				if OnStatusUpdate != nil {
					OnStatusUpdate(" [red]AUTO-RETRY...       [yellow]Restarting group...")
				}

			} else {
				if OnStatusUpdate != nil {
					OnStatusUpdate(" [red]ERROR!       [yellow]ENTER to continue, BACKSPACE to repeat.")
				}
			}

			// 1. Grab the bypass flag if the hotkey set it
			if AutoNextAction != 0 {
				action = AutoNextAction
				AutoNextAction = 0 // Reset it so it only fires once
			}

			// 2. ONLY enter the wait loop if we don't already have an action
			if action != 'E' && action != 'B' {
				waitMutex.Lock()
				isWaitingForKey = true
				waitMutex.Unlock()

				select {
				case <-waitActionChan:
				default: // Drains the channel
				}

			FallbackWait:
				for !IsStopping && !IsPaused {
					select {
					case action = <-waitActionChan:
						break FallbackWait
					case <-time.After(50 * time.Millisecond):
						// UI Tick
					}
				}

				waitMutex.Lock()
				isWaitingForKey = false
				waitMutex.Unlock()
			}

			if action == 'B' {
				rewindLen := len(lastGroup)
				startIndex := i - rewindLen + 1
				if startIndex < 0 {
					startIndex = 0
				}
				i = startIndex - 1
			}

			if OnClearFlashcardScreen != nil {
				OnClearFlashcardScreen()
			}

			pauseCounter = 0
			lastGroup = lastGroup[:0]
			continue
		}

		// FLASHCARD
		if config.User.Flashcard && (pauseCounter >= groupSize || i == len(playlist)-1) {

			if OnGroupCompleted != nil {
				OnGroupCompleted(lastGroup)
			}

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
					// UI Tick
				}
			}

			waitMutex.Lock()
			isWaitingForKey = false
			OnStatusUpdate(" [yellow]Playing")
			waitMutex.Unlock()

			pauseCounter = 0

			if action == 'B' {
				groupStats.Retries++
				SessionStats.Retries++

				rewindLen := len(lastGroup)
				startIndex := i - rewindLen + 1
				if startIndex < 0 {
					startIndex = 0
				}
				i = startIndex - 1

				if OnClearFlashcardScreen != nil {
					OnClearFlashcardScreen()
				}
			}

			if i == len(playlist)-1 && action != 'B' {
				break
			}
		}
	}

	IsPaused = false
	if OnStatusUpdate != nil {
		if config.User.Echo {
			OnStatusUpdate(" [yellow]KeyEcho Input Completed[-]")
			time.Sleep(2000 * time.Millisecond)
		} else {
			OnStatusUpdate("STOP")
		}
	}
}
