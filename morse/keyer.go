package morse

import (
	"log"
	"strings"
	"sync"
	"time"
	"yama/config"

	"go.bug.st/serial"
)

type KeyerStats struct {
	ShortDits, LongDits, PerfectDits                      int
	ShortDahs, LongDahs, PerfectDahs                      int
	ShortElementGaps, LongElementGaps, PerfectElementGaps int
	ShortCharGaps, LongCharGaps, PerfectCharGaps          int
	ShortWordGaps, LongWordGaps, PerfectWordGaps          int
	SumDitMs, SumDahMs                                    float64
	SumElementGapsMs, SumCharGapsMs, SumWordGapsMs        float64
	InvalidSymbols, Retries, TotalChars, TotalWords       int
}

type KeyerResult struct {
	Chars      []string
	RawTimings []Pulse
	WordGaps   []time.Duration
	Stats      KeyerStats
	Success    bool
	Error      string
}

// Shared logic to reduce binary size: Evaluates the next element to play
// Returns: playElement (0=None, 1=Dit, 2=Dah), newLastWasDit
func EvaluatePaddleAction(device string, rdit, rdah, mdit, mdah, lastWasDit bool, ultimaticLastDit bool) (int, bool) {
	playElement := 0

	// Merge physical reality and latched memory into a unified "Want" state
	wantDit := rdit || mdit
	wantDah := rdah || mdah

	switch device {
	case "Ultimatic":
		// Ultimatic strictly favors the LAST paddle pressed when both are requested
		if wantDit && wantDah {
			if ultimaticLastDit {
				playElement = 1
			} else {
				playElement = 2
			}
		} else if wantDit {
			playElement = 1
		} else if wantDah {
			playElement = 2
		}

	case "IambicB", "Paddle":
		// Iambic B strictly alternates. Honors the dual-latch memory even if released.
		if wantDit && wantDah {
			if lastWasDit {
				playElement = 2
			} else {
				playElement = 1
			}
		} else if wantDit {
			playElement = 1
		} else if wantDah {
			playElement = 2
		}

	case "IambicA":
		// Iambic A strictly alternates, BUT drops the dual-squeeze memory upon release
		if wantDit && wantDah {
			if !rdit && !rdah {
				// Both physical paddles have been released off a squeeze. Stop.
				playElement = 0
			} else if lastWasDit {
				playElement = 2
			} else {
				playElement = 1
			}
		} else if wantDit {
			playElement = 1
		} else if wantDah {
			playElement = 2
		}

	default:
		// Straight Key / Bug / Cootie (mdit/mdah act as direct lines)
		if wantDit {
			playElement = 1
		} else if wantDah {
			playElement = 2
		}
	}

	if playElement == 1 {
		lastWasDit = true
	} else if playElement == 2 {
		lastWasDit = false
	}

	return playElement, lastWasDit
}

func RunKeyer(device string,
	monitorPin1 string,
	powerPin string,
	monitorPin2 string,
	lastGroup []string,
	groupSendMS int,
	responseMS int,
	tp TimingProfile,
	p serial.Port,
	idleInputState bool,
	ditdahThreshold float64, // Now passed as float, e.g. 0.50
) (KeyerResult, error) {

	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL PANIC CAUGHT in RunKeyer: %v\n", r)
		}
	}()

	log.Println("=== YAMA KEYER STARTING: DEBUG BUILD V6 (STRICT SQUEEZE) ===")

	paddingArray := []float64{0.10, 0.25, 0.50, 1.00}
	var paddingFactor float64 = paddingArray[config.User.KeyTimePadding]

	groupSendMS = int(float64(groupSendMS) * (1 + paddingFactor))
	msgDur := time.Duration(groupSendMS) * time.Millisecond
	tol := tp.Tolerance

	dotSec := tp.DitDuration
	dashSec := tp.DahDuration
	dotDur := time.Duration(dotSec * float64(time.Second))
	dashDur := time.Duration(dashSec * float64(time.Second))
	interElementGap := time.Duration(tp.InterElement * float64(time.Second))

	charGap := time.Duration(tp.CharSpace * float64(time.Second))
	wordGap := time.Duration(tp.WordSpace * float64(time.Second))
	standardWordGap := time.Duration(tp.DitDuration * 7 * float64(time.Second))

	charGapEff := time.Duration(float64(charGap) * (1 - tol/2))
	wordGapEff := time.Duration(float64(standardWordGap) * (1 - tol/2))
	maxLegalWordGap := time.Duration(float64(wordGap) * (1 + tol))

	var resCharGaps []time.Duration
	var resWordGaps []time.Duration
	var currentSymbolPulses []Pulse
	var allPulses []Pulse
	var chars []string
	stats := KeyerStats{}

	oscFreq := float64(config.User.Tone)
	osc, player := StartOscillator(oscFreq)
	if osc != nil && player != nil {
		player.SetVolume(0.0)
		defer func() {
			player.SetVolume(0.0)
			time.Sleep(50 * time.Millisecond)
			player.Pause()
			player.Close()
		}()
	}

	ditPin := monitorPin1
	dahPin := monitorPin2
	if config.User.KeyerReversed && device != "Cootie" {
		ditPin = monitorPin2
		dahPin = monitorPin1
	}

	getMonitorState := func(ms *serial.ModemStatusBits, pin string) bool {
		switch pin {
		case "CD", "DCD":
			return ms.DCD
		case "DSR":
			return ms.DSR
		case "RI":
			return ms.RI
		case "CTS":
			fallthrough
		default:
			return ms.CTS
		}
	}

	isKeyDown := func(currentState bool) bool {
		return currentState != idleInputState
	}

	respDur := time.Duration(responseMS) * time.Millisecond
	minResp := 3 * wordGap
	if respDur < minResp {
		respDur = minResp
	}

	tailTimeout := maxLegalWordGap + (500 * time.Millisecond)
	if tailTimeout < (500 * time.Millisecond) {
		tailTimeout = 500 * time.Millisecond
	}
	minMsg := 1 * wordGap
	if msgDur < minMsg {
		msgDur = minMsg
	}

	if OnStatusUpdate != nil {
		OnStatusUpdate("ECHO:[white:#4CAF50:b] Key now... [-:-:-] ")
	}
	if OnEchoReadyCursor != nil && config.User.VisualFeedback {
		OnEchoReadyCursor(true)
	}

	responseDeadline := time.Now().Add(respDur)
	var messageDeadline time.Time
	lastChange := time.Now()
	started := false

	gradeGap := func(arr []time.Duration, targetSec float64, isWord bool) (int, int, int, float64) {
		s, l, p := 0, 0, 0
		var sumMs float64
		var tMin, tMax time.Duration
		if isWord {
			tMin = time.Duration(float64(standardWordGap) * (1 - tol))
			tMax = time.Duration(float64(wordGap) * (1 + tol))
		} else {
			tMin = time.Duration(targetSec * (1 - tol) * float64(time.Second))
			tMax = time.Duration(targetSec * (1 + tol) * float64(time.Second))
		}
		for _, d := range arr {
			sumMs += float64(d) / float64(time.Millisecond)
			if d < tMin {
				s++
			} else if d > tMax {
				l++
			} else {
				p++
			}
		}
		return s, l, p, sumMs
	}

	compileStats := func() KeyerStats {
		stats.ShortCharGaps, stats.LongCharGaps, stats.PerfectCharGaps, stats.SumCharGapsMs = gradeGap(resCharGaps, tp.CharSpace, false)
		stats.ShortWordGaps, stats.LongWordGaps, stats.PerfectWordGaps, stats.SumWordGapsMs = gradeGap(resWordGaps, tp.WordSpace, true)
		stats.TotalWords = stats.ShortWordGaps + stats.LongWordGaps + stats.PerfectWordGaps + 1
		stats.TotalChars = stats.ShortCharGaps + stats.LongCharGaps + stats.PerfectCharGaps + 1
		for _, c := range chars {
			if c == "*" {
				stats.InvalidSymbols++
			}
		}
		return stats
	}

	var mu sync.Mutex
	var memDit, memDah, lastWasDit, ultimaticLastDit bool
	var realtimeDit, realtimeDah bool
	var elementStartTime time.Time
	var currentElementDur time.Duration
	var isPlaying bool

	stopPolling := make(chan bool)
	defer func() { stopPolling <- true }()

	// The Background Memory Goroutine
	go func() {
		var prevDit, prevDah bool // Tracks edge transitions

		for {
			select {
			case <-stopPolling:
				return
			default:
				// FIX 1: Use the guaranteed local parameter 'p', NOT the global ActiveEchoPort!
				ms, err := p.GetModemStatusBits()
				if err == nil {
					currDit := isKeyDown(getMonitorState(ms, ditPin))
					currDah := isKeyDown(getMonitorState(ms, dahPin))

					mu.Lock()
					realtimeDit = currDit
					realtimeDah = currDah

					// Update "last pressed" only on the exact downstroke (transition)
					if currDit && !prevDit {
						ultimaticLastDit = true
					}
					if currDah && !prevDah {
						ultimaticLastDit = false
					}

					prevDit = currDit
					prevDah = currDah

					if isPlaying {
						elapsed := time.Since(elementStartTime).Seconds()
						// FIX 2: Zero-division crash protection (same as oscillator)
						if currentElementDur.Seconds() > 0 {
							threshLimit := currentElementDur.Seconds() * ditdahThreshold
							if elapsed >= threshLimit {
								if currDit {
									memDit = true
								}
								if currDah {
									memDah = true
								}
							}
						}
					} else {
						if currDit {
							memDit = true
						}
						if currDah {
							memDah = true
						}
					}
					mu.Unlock()
				}
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	playPerfectElement := func(isDit bool, duration time.Duration) {
		mu.Lock()
		isPlaying = true
		elementStartTime = time.Now()
		currentElementDur = duration
		mu.Unlock()

		gap := time.Since(lastChange)
		if !started {
			started = true
			if OnEchoReadyCursor != nil && config.User.VisualFeedback {
				OnEchoReadyCursor(false)
			}
			if OnStatusUpdate != nil {
				OnStatusUpdate("ECHO: ")
			}
			startTime = time.Now()
			lastSymbolTime = startTime
			messageDeadline = startTime.Add(msgDur + tailTimeout)
		} else {
			p := Pulse{Down: false, Duration: gap}
			currentSymbolPulses = append(currentSymbolPulses, p)
			allPulses = append(allPulses, p)
			if len(allPulses) > 1 {
				if gap >= wordGapEff {
					resWordGaps = append(resWordGaps, gap)
				} else if gap >= charGapEff {
					resCharGaps = append(resCharGaps, gap)
				} else {
					stats.PerfectElementGaps++
					stats.SumElementGapsMs += float64(gap) / float64(time.Millisecond)
				}
			}
		}

		if player != nil && config.User.SideTone {
			player.SetVolume(1.0)
		}

		time.Sleep(duration)

		mu.Lock()
		isPlaying = false
		mu.Unlock()

		p := Pulse{Down: true, Duration: duration}
		currentSymbolPulses = append(currentSymbolPulses, p)
		allPulses = append(allPulses, p)

		if isDit {
			stats.PerfectDits++
			stats.SumDitMs += float64(duration) / float64(time.Millisecond)
		} else {
			stats.PerfectDahs++
			stats.SumDahMs += float64(duration) / float64(time.Millisecond)
		}

		if player != nil {
			player.SetVolume(0.0)
		}

		time.Sleep(interElementGap)
		lastChange = time.Now()
	}

	for !IsStopping {
		if ForceEchoRetry {
			ForceEchoRetry = false
			stats.Retries = 1
			return KeyerResult{Chars: chars, RawTimings: allPulses, Success: false, Error: "retry", Stats: stats}, nil
		}
		if ForceEchoFinish || AutoNextAction == '\n' {
			ForceEchoFinish = false
			AutoNextAction = 0
			return KeyerResult{Chars: chars, RawTimings: allPulses, WordGaps: resWordGaps, Stats: compileStats(), Success: true, Error: "early_finish"}, nil
		}

		mu.Lock()
		rdit, rdah := realtimeDit, realtimeDah
		mdit, mdah := memDit, memDah
		uLastDit := ultimaticLastDit
		// Clear memory after reading it for the next element phase
		memDit, memDah = false, false
		mu.Unlock()

		if device == "Cootie" {
			if rdit || rdah {
				if player != nil {
					player.SetVolume(1.0)
				}
			} else {
				if player != nil {
					player.SetVolume(0.0)
				}
			}
			time.Sleep(1 * time.Millisecond)
			continue
		}

		if !started && time.Now().After(responseDeadline) {
			if OnEchoReadyCursor != nil && config.User.VisualFeedback {
				OnEchoReadyCursor(false)
			}
			return KeyerResult{Success: false, Error: "no start"}, nil
		}
		if started && time.Now().After(messageDeadline) {
			if config.User.EchoAutoRetry {
				return KeyerResult{Chars: chars, RawTimings: allPulses, Stats: compileStats(), Success: false, Error: "auto_retry"}, nil
			}
			return KeyerResult{Chars: chars, RawTimings: allPulses, WordGaps: resWordGaps, Stats: compileStats(), Success: false, Error: "too slow"}, nil
		}

		// Calculate Next Move using Shared Logic
		playElement, nextLastWasDit := EvaluatePaddleAction(device, rdit, rdah, mdit, mdah, lastWasDit, uLastDit)

		if playElement == 1 {
			playPerfectElement(true, dotDur)
			lastWasDit = nextLastWasDit
			continue
		} else if playElement == 2 {
			playPerfectElement(false, dashDur)
			lastWasDit = nextLastWasDit
			continue
		}

		// Decoding
		if started {
			gap := time.Since(lastChange)
			if gap > charGapEff && len(currentSymbolPulses) > 0 {
				decodedStr := decodeSymbol(currentSymbolPulses, dotDur, dashDur)
				lastSymbolTime = time.Now()
				if decodedStr == "" {
					decodedStr = "*"
				}

				if config.User.EchoAutoRetry {
					expectedString := strings.Join(lastGroup, " ")
					isMismatch := decodedStr == "*" || (len(chars) >= len(expectedString)) || (decodedStr != string(expectedString[len(chars)]))
					if isMismatch {
						chars = append(chars, decodedStr)
						if OnEchoCharDecoded != nil && config.User.VisualFeedback {
							OnEchoCharDecoded(decodedStr)
						}
						return KeyerResult{Chars: chars, RawTimings: allPulses, Stats: compileStats(), Success: false, Error: "auto_retry"}, nil
					}
				}

				if decodedStr != "" {
					chars = append(chars, decodedStr)
					if OnEchoCharDecoded != nil && config.User.VisualFeedback {
						go func(c string) {
							defer func() { _ = recover() }()
							OnEchoCharDecoded(c)
						}(decodedStr)
					}
					actualStr := strings.Join(strings.Fields(strings.Join(chars, "")), " ")
					expectedStr := strings.Join(lastGroup, " ")
					if actualStr == expectedStr {
						return KeyerResult{Chars: chars, RawTimings: allPulses, WordGaps: resWordGaps, Stats: compileStats(), Success: true, Error: ""}, nil
					}
				}
				currentSymbolPulses = currentSymbolPulses[:0]
			}

			if gap > wordGapEff {
				if len(chars) > 0 && chars[len(chars)-1] != " " {
					if config.User.EchoAutoRetry {
						expectedString := strings.Join(lastGroup, " ")
						isHesitationError := (len(chars) >= len(expectedString)) || (string(expectedString[len(chars)]) != " ")
						if isHesitationError {
							chars = append(chars, " ")
							if OnEchoCharDecoded != nil && config.User.VisualFeedback {
								go func() { defer func() { _ = recover() }(); OnEchoCharDecoded(" ") }()
							}
							return KeyerResult{Chars: chars, RawTimings: allPulses, Stats: compileStats(), Success: false, Error: "auto_retry"}, nil
						}
					}
					chars = append(chars, " ")
					if OnEchoCharDecoded != nil && config.User.VisualFeedback {
						go func() { defer func() { _ = recover() }(); OnEchoCharDecoded(" ") }()
					}
				}
				if gap > tailTimeout {
					expectedString := strings.Join(lastGroup, " ")
					actualStr := strings.Join(strings.Fields(strings.Join(chars, "")), " ")
					if config.User.EchoAutoRetry && actualStr != expectedString {
						return KeyerResult{Chars: chars, RawTimings: allPulses, Stats: compileStats(), Success: false, Error: "auto_retry"}, nil
					}
					return KeyerResult{Chars: chars, RawTimings: allPulses, WordGaps: resWordGaps, Stats: compileStats(), Success: true, Error: ""}, nil
				}
			}
		}

		time.Sleep(1 * time.Millisecond)
	}

	return KeyerResult{}, nil
}
