package morse

import (
	"fmt"
	"math"
	"time"

	"go.bug.st/serial"
)

var CoachingStrictness float64 = 0.3 // Coach requires twice the accuracy of the decoder
// ⚡ Flags for UI Hotkeys to interrupt the echo loop
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

	// ⚡ Duration sums for calculating Actual Average ms
	SumDitMs, SumDahMs                          float64
	SumElementGapsMs, SumCharGapsMs, SumWordGapsMs float64

	// Overall
	InvalidSymbols int
	Retries        int
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

// Takes the already-open port `p` and `idleCTS` baseline.
func RunFlashEcho(
	lastGroup []string,
	groupSendMS int,
	responseMS int,
	tp TimingProfile,
	p serial.Port,
	idleCTS bool,
) (EchoResult, error) {

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("CRITICAL PANIC CAUGHT in RunFlashEcho: %v\n", r)
		}
	}()

	var graceFactor = 0.15 // added to entire message duration
	groupSendMS = int(math.Round(float64(groupSendMS) * (1 + graceFactor)))
	msgDur := time.Duration(groupSendMS) * time.Millisecond

	dot := time.Duration(tp.DotDuration*1000) * time.Millisecond
	dash := time.Duration(tp.DashDuration*1000) * time.Millisecond
	charGap := time.Duration(tp.CharSpace*1000) * time.Millisecond
	wordGap := time.Duration(tp.WordSpace*1000) * time.Millisecond
	tol := tp.Tolerance

	time.Sleep(4 * dot)

	oscFreq := 700.0
	if tp.Tone > 0 {
		oscFreq = float64(tp.Tone)
	}

	osc, player := StartOscillator(oscFreq)
	if osc == nil || player == nil {
		fmt.Println("CRITICAL FAIL - osc or player is nil inside RunFlashEcho!")
	} else {
		defer func() {
			if player != nil {
				player.SetVolume(0.0)
				time.Sleep(200 * time.Millisecond)
				player.Pause()
				player.Close()
			}
		}()
	}

	respDur := time.Duration(responseMS) * time.Millisecond
	minResp := 4 * wordGap
	if respDur < minResp {
		respDur = minResp
	}

	if OnStatusUpdate != nil {
		OnStatusUpdate("ECHO:[white:#4CAF50:b] Key now... [-:-:-] ")
	}

	responseDeadline := time.Now().Add(respDur)

	minMsg := 3 * wordGap
	if msgDur < minMsg {
		msgDur = minMsg
	}
	msgTol := time.Duration(float64(msgDur) * tol)

	hesitationTimeout := time.Duration(float64(wordGap)*(1+tol)) * 3
	if hesitationTimeout < (2000 * time.Millisecond) {
		hesitationTimeout = 2000 * time.Millisecond
	}

	var pulses []Pulse
	var chars []string

	// ⚡ TRACKER ARRAYS FOR COACHING
	var resDits []time.Duration
	var resDahs []time.Duration
	var resEleGaps []time.Duration
	var resCharGaps []time.Duration
	var resWordGaps []time.Duration

	isKeyDown := func(currentCTS bool) bool {
		return currentCTS != idleCTS
	}

	ms, err := p.GetModemStatusBits()
	if err != nil {
		fmt.Println("EXITING: Initial modem read error:", err)
		return EchoResult{Success: false, Error: "modem read error"}, err
	}

	lastState := isKeyDown(ms.CTS)

	// Prime to UP loop with Auto-Correct
	primeStart := time.Now()
	for lastState {
		ms, err = p.GetModemStatusBits()
		if err != nil {
			return EchoResult{Success: false, Error: "modem read error"}, err
		}
		lastState = isKeyDown(ms.CTS)

		if time.Since(primeStart) > 200*time.Millisecond {
			idleCTS = ms.CTS
			lastState = false
		}
		time.Sleep(1 * time.Millisecond)
	}
	lastChange := time.Now()

	started := false
	var messageDeadline time.Time
	minPulseFilter := 15 * time.Millisecond

	for {
		ms, err = p.GetModemStatusBits()
		if err != nil {
			return EchoResult{Success: false, Error: "modem read error"}, err
		}

		keyDown := isKeyDown(ms.CTS)

		if !started {
			if keyDown {
				started = true
				startTime = time.Now()
				lastSymbolTime = startTime

				messageDeadline = startTime.Add(msgDur + msgTol + hesitationTimeout)
				lastState = keyDown
				lastChange = time.Now()

				pulses = pulses[:0]

				if player != nil {
					player.SetVolume(1.0)
				}
			} else if time.Now().After(responseDeadline) {
				return EchoResult{Success: false, Error: "no start"}, nil
			}
		}

		if started && time.Now().After(messageDeadline) {
			return EchoResult{Chars: chars, RawTimings: pulses, Success: false, Error: "too slow"}, nil
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
				// ⚡ Tracks Char and Word gaps
				if started && len(chars) > 0 {
					if len(pulses) == 1 {
						wgEff := time.Duration(float64(wordGap) * (1 - tol/2))
						cgEff := time.Duration(float64(charGap) * (1 - tol/2))

						if dur >= wgEff {
							resWordGaps = append(resWordGaps, dur)
						} else if dur >= cgEff {
							resCharGaps = append(resCharGaps, dur)
						}
					}
				}

				if player != nil {
					player.SetVolume(1.0)
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

			charGapEff := time.Duration(float64(charGap) * (1 - tol/2))
			if gap > charGapEff && len(pulses) > 0 {

				// ⚡ EXTRACT RAW TIMINGS BEFORE CLEARING PULSES
				for i, p := range pulses {
					if p.Down {
						if p.Duration < (dot+dash)/2 {
							resDits = append(resDits, p.Duration)
						} else {
							resDahs = append(resDahs, p.Duration)
						}
					} else {
						// ⚡ FIX: Prevent the initial silence from being graded as a character gap
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

				if decodedStr != "" {
					chars = append(chars, decodedStr)

					if OnEchoCharDecoded != nil {
						go func(c string) {
							defer func() { _ = recover() }()
							OnEchoCharDecoded(c)
						}(decodedStr)
					}
				}
				pulses = pulses[:0] // ⚡ Safely flushes array
			}

			wordGapEff := time.Duration(float64(wordGap) * (1 - tol/2))
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

				if gap > hesitationTimeout {

					// ⚡ GRADER FUNCTION (Using CoachingStrictness)
					grade := func(arr []time.Duration, targetSec float64) (int, int, int, float64) {
						s, l, p := 0, 0, 0
						var sumMs float64

						tMin := time.Duration(targetSec*(1-CoachingStrictness)*1000) * time.Millisecond
						tMax := time.Duration(targetSec*(1+CoachingStrictness)*1000) * time.Millisecond

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

					stats := EchoStats{}

					// Grade Elements
					stats.ShortDits, stats.LongDits, stats.PerfectDits, stats.SumDitMs = grade(resDits, tp.DotDuration)
					stats.ShortDahs, stats.LongDahs, stats.PerfectDahs, stats.SumDahMs = grade(resDahs, tp.DashDuration)

					// Grade Spaces
					stats.ShortElementGaps, stats.LongElementGaps, stats.PerfectElementGaps, stats.SumElementGapsMs = grade(resEleGaps, tp.InterElement)
					stats.ShortCharGaps, stats.LongCharGaps, stats.PerfectCharGaps, stats.SumCharGapsMs = grade(resCharGaps, tp.CharSpace)
					stats.ShortWordGaps, stats.LongWordGaps, stats.PerfectWordGaps, stats.SumWordGapsMs = grade(resWordGaps, tp.WordSpace)

					// Count Invalid Symbols
					for _, c := range chars {
						if c == "*" {
							stats.InvalidSymbols++
						}
					}

					return EchoResult{Chars: chars, RawTimings: pulses, WordGaps: resWordGaps, Stats: stats, Success: true, Error: ""}, nil
				}
			}
		}
		time.Sleep(1 * time.Millisecond)
	}
}
