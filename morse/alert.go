package morse

import (
	"encoding/binary"
	"math"
	"yama/config"
)

// generateDoneAlert creates a 16-bit PCM audio alert and returns it as a byte slice
func generateDoneAlert(sampleRate int, ditDurationMs float64) []byte {

	alertDuration := (ditDurationMs * 0.6)
	alertFreq := float64(config.User.AlertTone)

	numSamples := int(float64(sampleRate) * alertDuration)

	// 16-bit audio requires 2 bytes per sample
	alertBytes := make([]byte, numSamples*2)

	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)

		// Standard sine wave
		sample := math.Sin(2.0 * math.Pi * alertFreq * t)

		// 10ms fade-in/fade-out to prevent audio pops
		fadeSamples := float64(sampleRate) * 0.01
		if float64(i) < fadeSamples {
			sample *= float64(i) / fadeSamples
		} else if float64(numSamples-i) < fadeSamples {
			sample *= float64(numSamples-i) / fadeSamples
		}

		// Convert float (-1.0 to 1.0) to 16-bit signed integer
		intSample := int16(sample * 32767.0)

		// Pack the 16-bit integer into 2 bytes (Little Endian is standard for WAV/PCM)
		binary.LittleEndian.PutUint16(alertBytes[i*2:], uint16(intSample))
	}

	return alertBytes
}
