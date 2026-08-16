package morse

import (
	"encoding/binary"
	"math"
)

// generateDoneBlip creates a 16-bit PCM audio blip and returns it as a byte slice
func generateDoneBlip(sampleRate int, ditDurationMs float64) []byte {

	blipDuration := (ditDurationMs * 0.7)
	blipFreq := 500.0 // Low, distinct tone

	numSamples := int(float64(sampleRate) * blipDuration)

	// 16-bit audio requires 2 bytes per sample
	blipBytes := make([]byte, numSamples*2)

	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)

		// Standard sine wave
		sample := math.Sin(2.0 * math.Pi * blipFreq * t)

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
		binary.LittleEndian.PutUint16(blipBytes[i*2:], uint16(intSample))
	}

	return blipBytes
}
