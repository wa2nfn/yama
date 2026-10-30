package morse

import (
	"fmt"
	"yama/config"
)

// TimingProfile contains the calculated durations for play.go,
// oscillator.go, wave.go, keyer.go, echo.go, and IWR.
type TimingProfile struct {
	Tone         int
	DitDuration  float64
	DahDuration  float64
	InterElement float64
	CharSpace    float64
	WordSpace    float64
	Tolerance    float64

	WordSpaceMin float64
	WordSpaceMax float64
	DahLenMin    float64
	DahLenMax    float64
	DitLenMin    float64
	DitLenMax    float64
}

func validateTimingSelection(useFarnsworth, useWordsworth, useStandard bool) {
	count := 0
	if useFarnsworth {
		count++
	}
	if useWordsworth {
		count++
	}
	if useStandard {
		count++
	}

	if count != 1 {
		panic(fmt.Sprintf(
			"FATAL: Invalid timing configuration. Exactly one mode must be true. "+
				"Current state -> Farnsworth: %v, Wordsworth: %v, Standard: %v",
			useFarnsworth, useWordsworth, useStandard,
		))
	}
}

// GetTiming handles the switch between Standard, Farnsworth, Wordsworth,
// and IWR speeds. All values are returned in SECONDS.
func GetTiming(isIWR bool, user config.UserSettings) TimingProfile {

	var charWPM, wordWPM float64
	tone := user.Tone

	if isIWR && user.IWREnabled {
		charWPM = float64(user.IWRSpeed)
		wordWPM = float64(user.IWRSpeed)
		tone = user.IWRTone
	} else {
		charWPM = float64(user.CharacterSpeed)
		wordWPM = float64(user.CharacterSpeed)

		if user.UseWordsworth || user.UseFarnsworth {
			wordWPM = float64(user.EffectiveSpeed)
		}
	}

	// PARIS timing formula
	charUnit := 1.2 / charWPM
	wordUnit := 1.2 / wordWPM

	// Character spacing logic
	charSpace := charUnit * 3.0
	if !isIWR && user.UseFarnsworth && !user.UseWordsworth {
		charSpace = wordUnit * 3.0
	}

	validateTimingSelection(user.UseFarnsworth, user.UseWordsworth, user.UseStandard)

	tp := TimingProfile{
		Tone:         tone,
		DitDuration:  charUnit,
		DahDuration:  charUnit * 3.0,
		InterElement: charUnit,
		CharSpace:    charSpace,
		WordSpace:    wordUnit * 7.0,
		Tolerance:    float64(user.EchoTolerance) / 100.0,
	}

	// Derived thresholds used by oscillator.go, wave.go, keyer.go
	tp.WordSpaceMin = tp.WordSpace * (1 - tp.Tolerance)
	tp.WordSpaceMax = tp.WordSpace * (1 + tp.Tolerance)

	tp.DahLenMin = tp.DahDuration * (1 - tp.Tolerance)
	tp.DahLenMax = tp.DahDuration * (1 + tp.Tolerance)

	tp.DitLenMin = tp.DitDuration * (1 - tp.Tolerance)
	tp.DitLenMax = tp.DitDuration * (1 + tp.Tolerance)

	return tp
}
