package morse

import (
	"encoding/binary"
	"os"
	"strings"
	"yama/config"
)

// createWavHeader builds a standard 44-byte RIFF/WAVE header for 16-bit Mono PCM.
func createWavHeader(dataSize int, sampleRate int) []byte {
	header := make([]byte, 44)

	// RIFF chunk descriptor
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataSize)) // File size - 8

	copy(header[8:12], "WAVE")

	// fmt sub-chunk
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)                 // Subchunk1Size (16 for PCM)
	binary.LittleEndian.PutUint16(header[20:22], 1)                  // AudioFormat (1 = PCM)
	binary.LittleEndian.PutUint16(header[22:24], 1)                  // NumChannels (1 = Mono)
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate)) // SampleRate

	// 16-BIT SPECIFIC MATH
	bitsPerSample := 16
	byteRate := sampleRate * 1 * bitsPerSample / 8
	blockAlign := 1 * bitsPerSample / 8

	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(header[34:36], uint16(bitsPerSample))

	// data sub-chunk
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))

	return header
}

// RenderWAVToFile translates text to Morse and saves it as a 16-bit WAV file.
func RenderWAVToFile(playlist []PlayContext, outputPath string, sampleRate int) error {
	var audioBuffer []byte
	totalWords := len(playlist)

	for i, ctx := range playlist {

		// 1. FRESH MATH: Grab the user config for the WAV file
		tempConf := config.User

		// --- SPEED RAMPING LOGIC (Now applied to WAV files!) ---
		if tempConf.EndSpeed > tempConf.CharacterSpeed && totalWords > 1 {
			progress := float64(i) / float64(totalWords-1)
			charSpd := tempConf.CharacterSpeed + (tempConf.EndSpeed-tempConf.CharacterSpeed)*progress
			multiplier := charSpd / tempConf.CharacterSpeed

			tempConf.CharacterSpeed = charSpd
			tempConf.EffectiveSpeed = tempConf.EffectiveSpeed * multiplier
			tempConf.IWRSpeed = tempConf.IWRSpeed * multiplier
		}
		// -------------------------------------------------------

		// Build profiles dynamically per word
		baseProfile := GetTiming(false, tempConf)
		iwrProfile := GetTiming(true, tempConf)

		var p TimingProfile
		if ctx.IsIWR {
			p = iwrProfile
		} else {
			p = baseProfile
		}

		tokens := Tokenize(ctx.Word)
		for j, token := range tokens {
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

			for k, symbol := range pattern {
				dur := p.DotDuration
				if symbol == '-' {
					dur = p.DashDuration
				}

				// 1. USE 16-BIT TONE
				durationSamples := int(dur * float64(sampleRate))
				audioBuffer = append(audioBuffer, TonePCM16Bit(float64(p.Tone), durationSamples, 0.5, sampleRate).Samples...)

				// 2. Inter-Element space
				if k < len(pattern)-1 {
					silenceSamples := int(p.InterElement * float64(sampleRate))
					audioBuffer = append(audioBuffer, SilencePCM16Bit(silenceSamples).Samples...)
				}
			}

			// 3. Inter-Character space
			if j < len(tokens)-1 {
				silenceSamples := int(p.CharSpace * float64(sampleRate))
				audioBuffer = append(audioBuffer, SilencePCM16Bit(silenceSamples).Samples...)
			}
		}

		// 4. Word Space
		if i < len(playlist)-1 {
			silenceSamples := int(p.WordSpace * float64(sampleRate))
			audioBuffer = append(audioBuffer, SilencePCM16Bit(silenceSamples).Samples...)
		}
	}

	// 5. THE TAIL PAD: Add 0.5 seconds of flat silence to the very end of the file.
	tailPadding := int(0.5 * float64(sampleRate))
	audioBuffer = append(audioBuffer, SilencePCM16Bit(tailPadding).Samples...)

	header := createWavHeader(len(audioBuffer), sampleRate)
	fileData := append(header, audioBuffer...)
	return os.WriteFile(outputPath, fileData, 0644)
}
