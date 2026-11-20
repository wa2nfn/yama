package morse

import (
	"fmt"
	"strings"
	"time"
	"yama/config"

	"go.bug.st/serial"
)

func RunIWR(text string, iwrMan *IWRManager) {
	defer CloseHardwarePort()
	var idleInputState bool

	// ----------------------------------------------------
	// 1. HARDWARE & ECHO SETUP
	// ----------------------------------------------------
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

		if config.User.KeyerPort == "" || config.User.KeyerPort == "None" {
			config.User.Echo = false
			config.SaveConfig()

			if OnStatusUpdate != nil {
				OnStatusUpdate(" [red]Echo requires a selected COM port! Echo disabled. [yellow]Press ENTER.")
			}

			waitMutex.Lock()
			isWaitingForKey = true
			waitMutex.Unlock()
			select {
			case <-waitActionChan:
			default:
			}

		WaitErr1:
			for !IsStopping {
				select {
				case <-waitActionChan:
					break WaitErr1
				case <-time.After(50 * time.Millisecond):
				}
			}
			waitMutex.Lock()
			isWaitingForKey = false
			waitMutex.Unlock()

			if OnStatusUpdate != nil {
				OnStatusUpdate("STOP")
			}
			return
		}

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
			config.User.Echo = false
			config.SaveConfig()

			if OnStatusUpdate != nil {
				OnStatusUpdate(fmt.Sprintf(" [red]Port Error (%s) - Echo disabled. [yellow] Insert a cable, renable Echo in Ctrl-O screen. Press ENTER.", config.User.KeyerPort))
			}

			waitMutex.Lock()
			isWaitingForKey = true
			waitMutex.Unlock()
			select {
			case <-waitActionChan:
			default:
			}

		WaitErr2:
			for !IsStopping {
				select {
				case <-waitActionChan:
					break WaitErr2
				case <-time.After(50 * time.Millisecond):
				}
			}
			waitMutex.Lock()
			isWaitingForKey = false
			waitMutex.Unlock()

			if OnStatusUpdate != nil {
				OnStatusUpdate("STOP")
			}
			if OnEchoRuntimeFailure != nil {
				OnEchoRuntimeFailure("port-failure")
			}
			return
		}

		var useDTR, useRTS bool
		if openErr == nil {
			_ = ActiveEchoPort.ResetInputBuffer()
			_ = ActiveEchoPort.ResetOutputBuffer()
			if config.User.KeyParasiticPower {
				useDTR = true
				useRTS = true
			}
			_ = ActiveEchoPort.SetDTR(useDTR)
			_ = ActiveEchoPort.SetRTS(useRTS)
			time.Sleep(500 * time.Millisecond)
		} else {
			if OnStatusUpdate != nil {
				OnStatusUpdate(fmt.Sprintf(" [red]Keyer Port Error: %v", openErr))
			}
		}
	}

	// ----------------------------------------------------
	// 2. BUILD THE PLAYLIST
	// ----------------------------------------------------
	playlist := buildIWRPlaylist(text)
	if len(playlist) == 0 {
		return
	}

	// ----------------------------------------------------
	// 3. EXPORT WAV OR PROCEED TO PLAYBACK
	// ----------------------------------------------------
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

	// ----------------------------------------------------
	// 4. THE PLAYBACK ENGINE
	// ----------------------------------------------------
	pin1, power, pin2 := "CTS", "DTR", "DSR"
	if config.User.KeyDevice != "none" {
		parts := strings.Split(config.User.KeyLineWireMode, "-")
		if len(parts) == 3 {
			pin1 = strings.Split(parts[0], ":")[0]
			power = strings.Split(parts[1], ":")[0]
			pin2 = strings.Split(parts[2], ":")[0]
		}
	}

	var groupStats EchoStats
	var messageDuration int

	pauseCounter := 0
	var groupSize int
	var lastGroup []string

	for i := 0; i < len(playlist); i++ {
		ctx := playlist[i]

		for IsPaused && !IsStopping {
			time.Sleep(100 * time.Millisecond)
		}
		if IsStopping {
			break
		}

		// Re-roll the dice at the absolute start of every new group!
		if pauseCounter == 0 {
			groupSize = 1
			if config.User.Flashcard {
				if config.User.FlashRandomCount {
					groupSize = fastRand(config.User.FlashWordCount) + 1
				} else {
					groupSize = config.User.FlashWordCount
				}
			} else if config.User.Echo {
				if config.User.EchoRandomCount {
					groupSize = fastRand(config.User.EchoWordCount) + 1
				} else {
					groupSize = config.User.EchoWordCount
				}
			}
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

		if config.User.Echo {
			audioConf := tempConf
			audioConf.UseFarnsworth = false
			audioConf.UseWordsworth = false
			audioConf.UseStandard = true
			p = GetTiming(false, audioConf)
		} else {
			if ctx.IsIWR {
				p = iwrProfile
			} else {
				p = baseProfile
			}
		}

		messageDuration += buildWordBuffer(ctx, p)
		nextPauseCounter := pauseCounter + 1
		isGoingToEcho := config.User.Echo && (nextPauseCounter >= groupSize || i == len(playlist)-1)
		wordSpaceSamples := int(p.WordSpace * float64(SampleRate))

		if !isGoingToEcho {
			QueuePCM(SilencePCM(wordSpaceSamples).Samples, " ", -1)
		}

		Flush()

		// ----------------------------------------------------
		// THE EARLY ABORT TRAP
		// ----------------------------------------------------
		if ForceEchoRetry || ForceEchoFinish || ForceEchoRest {
			originalPauseCounter := pauseCounter

			if ForceEchoRetry {
				AutoNextAction = 'B'
				ForceEchoRetry = false
			} else if ForceEchoFinish {
				AutoNextAction = 'E'
				ForceEchoFinish = false

				wordsToSkip := groupSize - originalPauseCounter - 1
				if wordsToSkip > 0 {
					i += wordsToSkip
					if i >= len(playlist) {
						i = len(playlist) - 1
					}
				}
			} else if ForceEchoRest {
				AutoNextAction = 'R' // <--- R for Rest!
				ForceEchoRest = false
			}

			isGoingToEcho = true
			pauseCounter = groupSize
		}

		effectiveResponseMS := ResponseMS
		if config.User.Mute {
			effectiveResponseMS += 1000
		}

		if OnWordPlayed != nil {
			isIWRMatch := iwrMan.Match(ctx.Word)
			OnWordPlayed(ctx.Word, isIWRMatch)
		}

		pauseCounter++
		lastGroup = append(lastGroup, ctx.Word)

		// -----------------------------
		// ECHO LOGIC
		// -----------------------------
		if config.User.Echo && (pauseCounter >= groupSize || i == len(playlist)-1) {
			if OnEchoStart != nil {
				OnEchoStart()
				ShowBlueLineTolerance()
			}

			var res EchoResult
			var err error
			idleInputState = config.User.KeyLineIdlePolarity

			// ---------------------------------------------------------
			// THE BYPASS
			// If we aborted or requested a rest during audio playback,
			// we skip the serial port entirely and fake the result!
			// ---------------------------------------------------------
			if AutoNextAction == 0 {
				if ActiveEchoPort != nil {
					if config.User.KeyDevice == "Paddle" || config.User.KeyDevice == "IambicA" || config.User.KeyDevice == "Ultimatic" || config.User.KeyDevice == "IambicB" {
						kRes, kErr := RunKeyer(
							config.User.KeyDevice, pin1, power, pin2, lastGroup,
							messageDuration, effectiveResponseMS, baseProfile, ActiveEchoPort,
							idleInputState, config.User.DitDahThreshold,
						)
						err = kErr
						res = EchoResult{
							Chars:      kRes.Chars,
							RawTimings: kRes.RawTimings,
							WordGaps:   kRes.WordGaps,
							Success:    kRes.Success,
							Error:      kRes.Error,
							Stats: EchoStats{
								ShortDits: kRes.Stats.ShortDits, LongDits: kRes.Stats.LongDits, PerfectDits: kRes.Stats.PerfectDits,
								ShortDahs: kRes.Stats.ShortDahs, LongDahs: kRes.Stats.LongDahs, PerfectDahs: kRes.Stats.PerfectDahs,
								ShortElementGaps: kRes.Stats.ShortElementGaps, LongElementGaps: kRes.Stats.LongElementGaps, PerfectElementGaps: kRes.Stats.PerfectElementGaps,
								ShortCharGaps: kRes.Stats.ShortCharGaps, LongCharGaps: kRes.Stats.LongCharGaps, PerfectCharGaps: kRes.Stats.PerfectCharGaps,
								ShortWordGaps: kRes.Stats.ShortWordGaps, LongWordGaps: kRes.Stats.LongWordGaps, PerfectWordGaps: kRes.Stats.PerfectWordGaps,
								SumDitMs: kRes.Stats.SumDitMs, SumDahMs: kRes.Stats.SumDahMs, SumElementGapsMs: kRes.Stats.SumElementGapsMs,
								SumCharGapsMs: kRes.Stats.SumCharGapsMs, SumWordGapsMs: kRes.Stats.SumWordGapsMs,
								InvalidSymbols: kRes.Stats.InvalidSymbols, Retries: kRes.Stats.Retries, TotalChars: kRes.Stats.TotalChars, TotalWords: kRes.Stats.TotalWords,
							},
						}
					} else {
						res, err = RunEcho(
							pin1, power, pin2, lastGroup, messageDuration, effectiveResponseMS,
							baseProfile, ActiveEchoPort, idleInputState, config.User.KeyDevice,
						)
					}
				} else {
					err = fmt.Errorf("serial port not available")
					res = EchoResult{Success: false, Error: "no keyer"}
				}
			} else if AutoNextAction == 'R' {
				err = fmt.Errorf("rest")
				res = EchoResult{Success: false, Error: "rest"}
			} else {
				err = nil
				res = EchoResult{Success: true, Error: "aborted"}
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
				actualCharCount := len(strings.ReplaceAll(actualStr, " ", ""))
				actualWordCount := len(strings.Fields(actualStr))

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
				groupStats.TotalWords += actualWordCount
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
				SessionStats.TotalWords += actualWordCount
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
					if config.User.PerfectPause {
						if OnStatusUpdate != nil {
							OnStatusUpdate(" [green]Perfect Match![-:-:-]")
						}
						time.Sleep(1500 * time.Millisecond)
						if OnStatusUpdate != nil {
							OnStatusUpdate("")
						}
					}
					if OnClearFlashcardScreen != nil {
						OnClearFlashcardScreen()
					}
					groupStats = EchoStats{}
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
			} else if res.Error == "rest" {
				if OnStatusUpdate != nil {
					OnStatusUpdate(" [yellow]RESTING MODE...       ENTER to continue, BACKSPACE to repeat.")
				}
			} else if res.Error == "no keyer" {
				if OnStatusUpdate != nil {
					OnStatusUpdate(" [red]NO COM PORT CABLE DETECTED!       [yellow]ENTER to continue, BACKSPACE to repeat.")
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
				}
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

			// Apply our bypassed action if one was set!
			if AutoNextAction != 0 {
				action = AutoNextAction
				AutoNextAction = 0
			}

			// If it's not E (Enter) or B (Backspace), wait for the user to press one of them
			if action != 'E' && action != 'B' {
				waitMutex.Lock()
				isWaitingForKey = true
				waitMutex.Unlock()

				select {
				case <-waitActionChan:
				default:
				}

			FallbackWait:
				for !IsStopping && !IsPaused {
					select {
					case action = <-waitActionChan:
						break FallbackWait
					case <-time.After(50 * time.Millisecond):
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

		// -----------------------------
		// FLASHCARD LOGIC
		// -----------------------------
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
		if !IsStopping {
			if config.User.Echo {
				OnStatusUpdate(" [yellow]Echo Input Completed[-]")
				time.Sleep(2000 * time.Millisecond)
			} else {
				OnStatusUpdate("STOP")
			}
		}
	}
}
