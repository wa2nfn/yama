package morse

import (
	"fmt"
	"yama/config"
)

// TimingProfile contains the calculated durations for play.go
type TimingProfile struct {
	Tone         int
	DotDuration  float64
	DashDuration float64
	InterElement float64
	CharSpace    float64
	WordSpace    float64
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

	// Logic: Must be exactly 1. 0 is a failure, 2+ is a failure.
	if count != 1 {
		panic(fmt.Sprintf(
			"FATAL: Invalid timing configuration. Exactly one mode must be true. "+
				"Current state -> Farnsworth: %v, Wordsworth: %v, Standard: %v",
			useFarnsworth, useWordsworth, useStandard,
		))
	}
}

// GetTiming handles the switch between standard and IWR speeds
func GetTiming(isIWR bool, user config.UserSettings) TimingProfile {
	// 1. Choose base WPM and Tone
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

	// 2. Fundamental unit (1.2 / WPM)
	charUnit := 1.2 / charWPM
	wordUnit := 1.2 / wordWPM

	// 3. Spacing logic
	charSpace := charUnit * 3.0
	if !isIWR && user.UseFarnsworth && !user.UseWordsworth {
		charSpace = wordUnit * 3.0
	}

	validateTimingSelection(user.UseFarnsworth, user.UseWordsworth, user.UseStandard)

	/* WDL 
	log.Printf("=== DEBUG TIMING PROFILE | Tone: %dHz | Dit: %.0fms | Dah: %.0fms | IntElem: %.0fms | CharSp: %.0fms | WordSp: %.0fms ===",
		tone,
		charUnit*1000,
		charUnit*3*1000,
		charUnit*1000,
		charSpace*1000,
		wordUnit*7*1000)
		*/

	return TimingProfile{
		Tone:         tone,
		DotDuration:  charUnit,
		DashDuration: charUnit * 3.0,
		InterElement: charUnit,
		CharSpace:    charSpace,
		WordSpace:    wordUnit * 7.0,
	}
}
