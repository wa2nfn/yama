package morse

import (
	"log"
	"strings"
	"time"
	"yama/config"

	"go.bug.st/serial"
)

var ForceEchoRetry bool
var ForceEchoFinish bool
var AutoNextAction rune

type EchoStats struct {
	// Elements
	ShortDits, LongDits, PerfectDits int
	ShortDahs, LongDahs, PerfectDahs int

	// Spaces
	ShortElementGaps, LongElementGaps, PerfectElementGaps int
	ShortCharGaps, LongCharGaps, PerfectCharGaps          int
	ShortWordGaps, LongWordGaps, PerfectWordGaps          int

	// Duration sums for calculating Actual Average ms
	SumDitMs, SumDahMs                             float64
	SumElementGapsMs, SumCharGapsMs, SumWordGapsMs float64

	// Overall
	InvalidSymbols int
	Retries        int
	TotalChars     int
	TotalWords     int
}

type EchoResult struct {
	Chars      []string
	RawTimings []Pulse
	WordGaps   []time.Duration
	Stats      EchoStats
	Success    bool
	Error      string
}

type Pulse struct {
	Down     bool
	Duration time.Duration
}

var startTime time.Time
var lastSymbolTime time.Time

func GetSerialPorts() ([]string, error) {
	return serial.GetPortsList()
}

func decodeSymbol(pulses []Pulse, dot, dash time.Duration) string {
	var pattern string
	for _, p := range pulses {
		if p.Down {
			if p.Duration < (dot+dash)/2 {
				pattern += "."
			} else {
				pattern += "-"
			}
		}
	}
	if ch, ok := EchoMap[pattern]; ok {
		return ch
	}
	return ""
}

// Takes the already-open port `p` and `idleInputState` baseline.
func RunEcho(
	lastGroup []string,
	groupSendMS int,
	responseMS int,
	tp TimingProfile,
	p serial.Port,
	idleInputState bool,
) (EchoResult, error) {

	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL PANIC CAUGHT in RunEcho: %v\n", r)
		}
	}()

	paddingArray := []float64{0.10, 0.25, 0.50, 1.00}
	var paddingFactor float64 = paddingArray[config.User.KeyTimePadding] // added to entire message duration

	groupSendMS = int(float64(groupSendMS) * (1 + paddingFactor))
	msgDur := time.Duration(groupSendMS) * time.Millisecond
	tol := tp.Tolerance
	dot := time.Duration(tp.DitDuration*1000) * time.Millisecond
	dash := time.Duration(tp.DahDuration*1000) * time.Millisecond
	charGap := time.Duration(tp.CharSpace*1000) * time.Millisecond
	wordGap := time.Duration(tp.WordSpace*1000) * time.Millisecond
	standardWordGap := time.Duration(tp.DitDuration*7*1000) * time.Millisecond

	charGapEff := time.Duration(float64(charGap) * (1 - tol/2))
	wordGapEff := time.Duration(float64(standardWordGap) * (1 - tol/2))
	oscFreq := float64(tp.Tone)
	osc, player := StartOscillator(oscFreq)
	if osc == nil || player == nil {
		log.Println("CRITICAL FAIL - osc or player is nil inside RunEcho!")
	} else {
		// FORCE SILENCE IMMEDIATELY UPON CREATION (Every Group)
		player.SetVolume(0.0)

		defer func() {
			if player != nil {
				player.SetVolume(0.0)
				time.Sleep(50 * time.Millisecond)
				player.Pause()
				player.Close()
			}
		}()
	}

	if config.User.UseFarnsworth {
		return EchoResult{Success: false, Error: "farnsworth option error"}, nil
	}

	respDur := time.Duration(responseMS) * time.Millisecond
	minResp := 3 * wordGap // incase input is just single char words
	if respDur < minResp {
		respDur = minResp
	}

	// how long to wait for silence at the END to declare group finished
	// must be > 1 wordGap so doesn't cut off multi word
	// The longest gap legally allowed by Wordsworth + your UI Tolerance
	maxLegalWordGap := time.Duration(float64(tp.WordSpace*1000)*(1+tol)) * time.Millisecond

	// Give the timeout that full duration plus a 500ms safety cushion
	tailTimeout := maxLegalWordGap + (500 * time.Millisecond)
	if tailTimeout < (500 * time.Millisecond) {
		tailTimeout = 500 * time.Millisecond
	}

	if OnStatusUpdate != nil {
		OnStatusUpdate("ECHO:[white:#4CAF50:b] Key now... [-:-:-] ")
	}

	responseDeadline := time.Now().Add(respDur) // CLOCK IS RUNNING

	minMsg := 1 * wordGap
	if msgDur < minMsg {
		msgDur = minMsg
	}

	hesitationTimeout := time.Duration(float64(wordGap)) * 4
	if hesitationTimeout < (500 * time.Millisecond) {
		hesitationTimeout = 500 * time.Millisecond
	}

	var pulses []Pulse
	var chars []string

	// TRACKER ARRAYS FOR COACHING
	var resDits []time.Duration
	var resDahs []time.Duration
	var resEleGaps []time.Duration
	var resCharGaps []time.Duration
	var resWordGaps []time.Duration

	// --- DYNAMIC PIN CONFIGURATION ---
	// Extract the monitor pin from config (e.g., "CD" from "CD:1-DTR:4")
	pinParts := strings.Split(config.User.KeyLineMode, "-")
	monitorPin := "CTS" // fallback default
	if len(pinParts) > 0 {
		monitorPin = strings.Split(pinParts[0], ":")[0]
	}

	getMonitorState := func(ms *serial.ModemStatusBits) bool {
		switch monitorPin {
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
	// ---------------------------------

	isKeyDown := func(currentState bool) bool {
		return currentState != idleInputState
	}

	ms, err := p.GetModemStatusBits()
	if err != nil {
		log.Println("EXITING: Initial modem read error:", err)
		return EchoResult{Success: false, Error: "modem read error"}, err
	}

	// Force baseline synchronization for every group to prevent cross-group state bleeding
	idleInputState = config.User.KeyLineIdlePolarity
	lastState := false

	// Prime to UP loop with Auto-Correct
	primeStart := time.Now()
	for lastState {
		ms, err = p.GetModemStatusBits()
		if err != nil {
			return EchoResult{Success: false, Error: "modem read error"}, err
		}

		currentState := getMonitorState(ms) // Replaced ms.CTS
		lastState = isKeyDown(currentState)

		if time.Since(primeStart) > 200*time.Millisecond {
			idleInputState = currentState
			lastState = false
		}
		time.Sleep(1 * time.Millisecond)
	}
	lastChange := time.Now()
	started := false
	var messageDeadline time.Time
	minPulseFilter := 15 * time.Millisecond

	// EXTRACTED GRADER FUNCTION
	grade := func(arr []time.Duration, targetSec float64) (int, int, int, float64) {
		s, l, p := 0, 0, 0
		var sumMs float64
		tMin := time.Duration(targetSec*(1-tol)*1000) * time.Millisecond
		tMax := time.Duration(targetSec*(1+tol)*1000) * time.Millisecond
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

	compileStats := func() EchoStats {
		stats := EchoStats{}
		stats.ShortDits, stats.LongDits, stats.PerfectDits, stats.SumDitMs = grade(resDits, tp.DitDuration)
		stats.ShortDahs, stats.LongDahs, stats.PerfectDahs, stats.SumDahMs = grade(resDahs, tp.DahDuration)
		stats.ShortElementGaps, stats.LongElementGaps, stats.PerfectElementGaps, stats.SumElementGapsMs = grade(resEleGaps, tp.InterElement)
		stats.ShortCharGaps, stats.LongCharGaps, stats.PerfectCharGaps, stats.SumCharGapsMs = grade(resCharGaps, tp.CharSpace)

		var sumWordMs float64
		// Floor is based on Standard gap (e.g., 420ms - tolerance)
		wMin := time.Duration(float64(standardWordGap) * (1 - tol))

		// Ceiling is based on the massive Wordsworth gap (e.g., 840ms + tolerance)
		wMax := time.Duration(float64(wordGap) * (1 + tol))

		stats.ShortWordGaps, stats.LongWordGaps, stats.PerfectWordGaps = 0, 0, 0
		for _, d := range resWordGaps {
			sumWordMs += float64(d) / float64(time.Millisecond)
			if d < wMin {
				stats.ShortWordGaps++
			} else if d > wMax {
				stats.LongWordGaps++
			} else {
				stats.PerfectWordGaps++
			}
		}
		stats.SumWordGapsMs = sumWordMs
		stats.TotalWords = stats.ShortWordGaps + stats.LongWordGaps + stats.PerfectWordGaps + 1 // 1 added since no last WS
		stats.TotalChars = stats.ShortCharGaps + stats.LongCharGaps + stats.PerfectCharGaps + 1

		for _, c := range chars {
			if c == "*" {
				stats.InvalidSymbols++
			}
		}
		return stats
	}
	for {
		// UI INTERRUPT CHECKS (Proactively catch BS and ENTER)
		if ForceEchoRetry {
			ForceEchoRetry = false
			return EchoResult{Chars: chars, RawTimings: pulses, Success: false, Error: "retry", Stats: EchoStats{Retries: 1}}, nil
		}

		if ForceEchoFinish || AutoNextAction == '\n' {
			ForceEchoFinish = false
			AutoNextAction = 0
			return EchoResult{Chars: chars, RawTimings: pulses, WordGaps: resWordGaps, Stats: compileStats(), Success: true, Error: "early_finish"}, nil
		}

		ms, err = p.GetModemStatusBits()
		if err != nil {
			return EchoResult{Success: false, Error: "modem read error"}, err
		}

		currentState := getMonitorState(ms)
		keyDown := isKeyDown(currentState)

		if !started {
			if keyDown {
				started = true
				startTime = time.Now()
				lastSymbolTime = startTime
				messageDeadline = startTime.Add(msgDur + tailTimeout)
				lastChange = time.Now()
				pulses = pulses[:0]
			} else if time.Now().After(responseDeadline) {
				return EchoResult{Success: false, Error: "no start"}, nil
			}
		}

		if started && time.Now().After(messageDeadline) {
			if config.User.EchoAutoRetry {
				return EchoResult{
					Chars:      chars,
					RawTimings: pulses,
					Stats:      compileStats(),
					Success:    false,
					Error:      "auto_retry",
				}, nil
			}
			return EchoResult{Chars: chars, RawTimings: pulses, WordGaps: resWordGaps, Stats: compileStats(), Success: false, Error: "too slow"}, nil
		}

		if keyDown != lastState {
			now := time.Now()
			dur := now.Sub(lastChange)

			if dur < minPulseFilter {
				time.Sleep(1 * time.Millisecond)
				continue
			}

			lastChange = now
			pulses = append(pulses, Pulse{Down: lastState, Duration: dur})

			if keyDown {
				if started && len(chars) > 0 {
					if len(pulses) == 1 {

						if dur >= wordGapEff {
							resWordGaps = append(resWordGaps, dur)
						} else if dur >= charGapEff {
							resCharGaps = append(resCharGaps, dur)
						}
					}
				}

				if player != nil {
					if config.User.SideTone {
						player.SetVolume(1.0)
					} else {
						player.SetVolume(0.0)
					}
				}
			} else {
				if player != nil {
					player.SetVolume(0.0)
				}
			}

			lastState = keyDown
		}

		if !keyDown && started {
			gap := time.Since(lastChange)

			if gap > charGapEff && len(pulses) > 0 {

				for i, p := range pulses {
					if p.Down {
						if p.Duration < (dot+dash)/2 {
							resDits = append(resDits, p.Duration)
						} else {
							resDahs = append(resDahs, p.Duration)
						}
					} else {
						if i != 0 {
							resEleGaps = append(resEleGaps, p.Duration)
						}
					}
				}

				decodedStr := decodeSymbol(pulses, dot, dash)
				lastSymbolTime = time.Now()

				if decodedStr == "" {
					decodedStr = "*"
				}

				// --- NEW AUTORETRY LOGIC ---
				if config.User.EchoAutoRetry {
					isInvalid := decodedStr == "*"
					isMismatch := false

					expectedString := strings.Join(lastGroup, " ")
					// Compare against expected character in lastGroup if available
					if len(expectedString) > len(chars) && decodedStr != "*" && decodedStr != " " {
						expectedChar := string(expectedString[len(chars)])
						if decodedStr != expectedChar {
							isMismatch = true
						}
					}

					if isInvalid || isMismatch {
						// Kill the sidetone immediately
						if player != nil {
							player.SetVolume(0.0)
						}

						// Append the bad character so stats/UI reflect the failure before the wipe
						chars = append(chars, decodedStr)
						if OnEchoCharDecoded != nil {
							OnEchoCharDecoded(decodedStr)
						}
						return EchoResult{
							Chars:      chars,
							RawTimings: pulses,
							Stats:      compileStats(),
							Success:    false,
							Error:      "auto_retry",
						}, nil
					}
				}
				// --- END AUTORETRY LOGIC ---

				if decodedStr != "" {
					// 1. Append the decoded character
					chars = append(chars, decodedStr)

					// 2. Update the UI
					if OnEchoCharDecoded != nil {
						go func(c string) {
							defer func() { _ = recover() }()
							OnEchoCharDecoded(c)
						}(decodedStr)
					}

					// 3. --- SMART QUICK-EXIT ---
					actualStr := strings.Join(strings.Fields(strings.Join(chars, "")), " ")
					expectedStr := strings.Join(lastGroup, " ")
					if actualStr == expectedStr {
						return EchoResult{Chars: chars, RawTimings: pulses, WordGaps: resWordGaps, Stats: compileStats(), Success: true, Error: ""}, nil
					}
					// ---------------------------
				}
				pulses = pulses[:0]
			}

			if gap > wordGapEff {
				if len(chars) > 0 && chars[len(chars)-1] != " " {
					chars = append(chars, " ")

					if OnEchoCharDecoded != nil {
						go func() {
							defer func() { _ = recover() }()
							OnEchoCharDecoded(" ")
						}()
					}
				}

				if gap > tailTimeout {
					// Safely check how many characters were expected
					expectedString := strings.Join(lastGroup, " ")

					// If they stopped early and didn't finish the word, force a retry!
					if config.User.EchoAutoRetry && len(chars) < len(expectedString) {
						return EchoResult{
							Chars:      chars,
							RawTimings: pulses,
							Stats:      compileStats(),
							Success:    false,
							Error:      "auto_retry",
						}, nil
					}

					return EchoResult{Chars: chars, RawTimings: pulses, WordGaps: resWordGaps, Stats: compileStats(), Success: true, Error: ""}, nil
				}
			}
		}
		time.Sleep(1 * time.Millisecond)
	}
}

// EchoPortDecision describes the result of scanning serial ports for CTS support.
type EchoPortDecision struct {
	SelectedPort string
	NeedModal    bool
	Err          error
	CTSPorts     []string
}
