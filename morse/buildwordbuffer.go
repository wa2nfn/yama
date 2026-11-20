package morse

import (
	"strings"
	"sync"
	"yama/config"

	"go.bug.st/serial"
)

// --- FAST RNG
var fastSeed uint64 = 88172645463325252

func fastRand(n int) int {
	fastSeed = fastSeed*6364136223846793005 + 1
	return int(fastSeed>>33) % n
}

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

func fastShuffleRunes(s string) string {
	runes := []rune(s)
	for i := len(runes) - 1; i > 0; i-- {
		j := fastRand(i + 1)
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func fastShuffleSlice(words []string) {
	for i := len(words) - 1; i > 0; i-- {
		j := fastRand(i + 1)
		words[i], words[j] = words[j], words[i]
	}
}

func shuffleWord(w string) string {
	return fastShuffleRunes(w)
}

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

			if config.User.Mute && config.User.Echo {
				// INSTANT UI UPDATE for Echo mode
				if label != "" {
					QueuePCM(SilencePCM(1).Samples, label, i)
				}
			} else {
				if config.User.Mute {
					QueuePCM(SilencePCM(int(dur*float64(SampleRate))).Samples, label, i)
				} else {
					QueuePCM(TonePCM(float64(p.Tone), int(dur*float64(SampleRate)), 0.5).Samples, label, i)
				}

				if j < len(pattern)-1 {
					QueuePCM(SilencePCM(int(p.InterElement*float64(SampleRate))).Samples, "", i)
				}
			}
		}

		if i < len(tokens)-1 {
			if !(config.User.Mute && config.User.Echo) {
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
