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
var ResponseMS int = 1000 // WDL MAGIC move to UI 
var ShowBlueLineTolerance func()
var OnVisibilityToggle func()
var OnEchoCharDecoded func(char string)
var OnWordChange func(char string, index int)
var ActiveEchoPort serial.Port
var SessionStats EchoStats
var OnEchoStatsUpdated func(groupStats EchoStats, sessionStats EchoStats)

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

			if j < len(pattern)-1 {
				QueuePCM(SilencePCM(int(p.InterElement*float64(SampleRate))).Samples, "", i)
			}
		}

		if i < len(tokens)-1 {
			QueuePCM(SilencePCM(int(p.CharSpace*float64(SampleRate))).Samples, "", i)
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
			dur := p.DotDuration
			if symbol == '-' {
				dur = p.DashDuration
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
	var echoDoOnce bool

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	if config.User.Echo {
		EchoMap = make(map[string]string)
		for k, v := range EchoMapBase {
			EchoMap[k] = v
		}
		if config.User.UseEuropeanChars {
			for k, v := range EchoMapEuropean {
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

	groupSize := config.User.FlashWordCount
	if config.User.Flashcard && config.User.FlashRandomCount {
		groupSize = rand.Intn(groupSize) + 1
	}
	if groupSize <= 0 {
		groupSize = 1
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

		baseProfile := GetTiming(false, tempConf)
		iwrProfile := GetTiming(true, tempConf)

		var p TimingProfile
		if ctx.IsIWR {
			p = iwrProfile
		} else {
			p = baseProfile
		}

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

		// FLASHCARD ECHO
		if config.User.Flashcard && (pauseCounter >= groupSize || i == len(playlist)-1) {

			if config.User.EchoTolerance > 0 {

				// ⚡ 1. WAKE UP HARDWARE IMMEDIATELY BEFORE KEYING
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
				var idleCTS bool
				ActiveEchoPort, openErr = serial.Open(config.User.KeyerPort, mode)

				if openErr == nil {
					// ⚡ ROBUST BUFFER CLEARING
					_ = ActiveEchoPort.ResetInputBuffer()
					_ = ActiveEchoPort.ResetOutputBuffer()

					_ = ActiveEchoPort.SetDTR(true)
					_ = ActiveEchoPort.SetRTS(true)
					time.Sleep(500 * time.Millisecond) // Let power stabilize
					ms, err := ActiveEchoPort.GetModemStatusBits()
					if err == nil {
						idleCTS = ms.CTS
					}
				} else {
					if OnStatusUpdate != nil {
						OnStatusUpdate(fmt.Sprintf(" [red]Keyer Port Error: %v", openErr))
					}
				}

				if OnEchoStart != nil {
					OnEchoStart()
					ShowBlueLineTolerance()
				}

				// ⚡ 2. RUN THE DECODER
				var res EchoResult
				var err error
				if ActiveEchoPort != nil {
					// test if blip wanted
					if !echoDoOnce {
						echoDoOnce = true

						// Generate the blip bytes
						blipBytes := generateDoneBlip(SampleRate, float64(p.DotDuration))

						QueuePCM(SilencePCM(wordSpaceSamples).Samples, " ", -1)
						QueuePCM(blipBytes, " ", -1)
						Flush()
					}
					res, err = RunFlashEcho(
						lastGroup,
						messageDuration,
						ResponseMS,
						baseProfile,
						ActiveEchoPort,
						idleCTS,
					)
				} else {
					err = fmt.Errorf("serial port not available")
					res = EchoResult{Success: false, Error: "no keyer"}
				}

				// ⚡ 3. INSTANTLY CLOSE IT SO IT CAN'T ZOMBIE
				if ActiveEchoPort != nil {
					ActiveEchoPort.Close()
					ActiveEchoPort = nil
				}

				if OnEchoEnd != nil {
					OnEchoEnd()
				}
				messageDuration = 0

				var action rune
if err == nil && res.Success {
					expectedRaw := strings.ToUpper(strings.Join(lastGroup, " "))
					actualRaw := strings.ToUpper(strings.Join(res.Chars, ""))

					expectedStr := strings.Join(strings.Fields(expectedRaw), " ")
					actualStr := strings.Join(strings.Fields(actualRaw), " ")

					// ⚡ MASSIVE DATA ACCUMULATOR
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
                    
					// ⚡ Add new timing duration sums
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

					// ⚡ Add new timing duration sums to Session
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
						OnStatusUpdate(" [red]Mismatch! [yellow]ENTER to continue, BACKSPACE to repeat.")
					}

				} else if res.Error == "no start" {
					if OnStatusUpdate != nil {
						OnStatusUpdate(" [red]No input detected. [yellow]ENTER to continue, BACKSPACE to repeat.")
					}
				} else if res.Error == "too slow" {
					if OnStatusUpdate != nil {
						OnStatusUpdate(" [red]Timeout! [yellow]ENTER to continue, BACKSPACE to repeat.")
					}
				} else if res.Error == "no keyer" {
					if OnStatusUpdate != nil {
						OnStatusUpdate(" [red]COM Port Locked! [yellow]ENTER/BACKSPACE to retry.")
					}
				} else {
					if OnStatusUpdate != nil {
						OnStatusUpdate(" [red]Error! [yellow]ENTER/BACKSPACE to retry.")
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
		OnStatusUpdate("STOP")
	}
}
