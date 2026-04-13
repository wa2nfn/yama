package morse

import (
	"bytes"
	"encoding/binary"
	"log"
	"math"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

var (
	otoCtx     *oto.Context
	once       sync.Once
	audioReady bool
	// The new structured queue
	queue []audioBlock
)

const SampleRate = 44100

type audioBlock struct {
	samples []byte // Swapped to raw bytes
	char    string
	index   int
}

type PCMData struct{ Samples []byte } // Swapped to raw bytes

func TonePCM(freq float64, duration int, vol float64) PCMData {
	buf := make([]byte, duration*2) // *2 because 16-bit audio needs 2 bytes per sample
	ramp := int(math.Round(0.005 * float64(SampleRate)))
	if ramp*2 > duration {
		ramp = duration / 2
	}
	for i := 0; i < duration; i++ {
		angle := 2.0 * math.Pi * freq * float64(i) / float64(SampleRate)
		amp := float32(vol)
		if i < ramp {
			amp *= float32(i) / float32(ramp)
		} else if i > duration-ramp {
			amp *= float32(duration-i) / float32(ramp)
		}

		// Calculate the float and instantly convert to 16-bit Little Endian bytes
		s := float32(math.Sin(angle)) * amp
		v := int16(s * 32767)
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}
	return PCMData{Samples: buf}
}

func SilencePCM(duration int) PCMData {
	buf := make([]byte, duration*2)
	// Low floor to prevent driver sleep/popping (0.0001 * 32767 = ~3)
	v := uint16(int16(3))
	for i := 0; i < duration; i++ {
		binary.LittleEndian.PutUint16(buf[i*2:], v)
	}
	return PCMData{Samples: buf}
}

// QueuePCM now accepts the raw bytes
func QueuePCM(samples []byte, char string, index int) {
	queue = append(queue, audioBlock{
		samples: samples,
		char:    char,
		index:   index,
	})
}

func Flush() {
	if !audioReady || otoCtx == nil || len(queue) == 0 {
		queue = nil
		return
	}

	for _, block := range queue {
		// 1. Instantly abort the queue if the user hits Stop
		if IsStopping {
			break
		}

		// Sync Trigger: Update UI immediately before playing this specific block
		if block.char != "" && OnWordChange != nil {
			OnWordChange(block.char, block.index)
		}

		// Look how much cleaner this is! Zero float-to-byte math inside the loop.
		p := otoCtx.NewPlayer(bytes.NewReader(block.samples))
		p.Play()

		// Wait for this specific sound unit to finish
		for p.IsPlaying() {
			// 2. Instantly abort mid-beep if the user hits Stop
			if IsStopping {
				break
			}
			time.Sleep(1 * time.Millisecond)
		}

		// 3. CRITICAL: Release the hardware buffer so Oto doesn't go permanently silent
		p.Close()
	}

	// Always reset the queue so the next Run starts completely fresh
	queue = nil
}

func InitAudio() error {
	var err error
	once.Do(func() {
		op := &oto.NewContextOptions{
			SampleRate:   SampleRate, // Ensure SampleRate is defined (e.g., 24000)
			ChannelCount: 1,
			Format:       oto.FormatSignedInt16LE,
		}
		ctx, ready, e := oto.NewContext(op)
		if e != nil {
			err = e
			return
		}
		<-ready
		otoCtx = ctx
		audioReady = true
	})
	return err
}

// Helper function to force Windows to lock onto the new audio endpoint
func ResetAudioDevice() error {
	if otoCtx == nil {
		return nil
	}
	log.Println("Audio hardware error detected. Kicking Windows Audio Context...")

	if err := otoCtx.Suspend(); err != nil {
		log.Printf("Suspend error: %v", err)
		// You might still want to continue to Resume even if Suspend throws a fit
	}

	time.Sleep(100 * time.Millisecond)

	if err := otoCtx.Resume(); err != nil {
		log.Printf("Resume error: %v", err)
		return err // Let the caller know the audio engine is dead
	}

	return nil
}

// ==========================================
// 🌊 WAV EXPORT AUDIO MATH (8-Bit Unsigned)
// ==========================================

// TonePCM8Bit generates 8-bit unsigned PCM to keep WAV files 75% smaller.
func TonePCM8Bit(freq float64, duration int, vol float64, targetSampleRate int) PCMData {
	buf := make([]byte, duration) // 1 byte per sample
	ramp := int(math.Round(0.005 * float64(targetSampleRate)))
	if ramp*2 > duration {
		ramp = duration / 2
	}
	for i := 0; i < duration; i++ {
		angle := 2.0 * math.Pi * freq * float64(i) / float64(targetSampleRate)
		amp := float32(vol)
		if i < ramp {
			amp *= float32(i) / float32(ramp)
		} else if i > duration-ramp {
			amp *= float32(duration-i) / float32(ramp)
		}

		// 8-bit WAV must be UNSIGNED (center 128)
		s := float32(math.Sin(angle)) * amp
		v := uint8((s * 127.0) + 128.0)
		buf[i] = v
	}
	return PCMData{Samples: buf}
}

// SilencePCM8Bit generates 8-bit unsigned silence.
func SilencePCM8Bit(duration int) PCMData {
	buf := make([]byte, duration)
	for i := 0; i < duration; i++ {
		buf[i] = 128 // Center point for unsigned 8-bit audio
	}
	return PCMData{Samples: buf}
}
