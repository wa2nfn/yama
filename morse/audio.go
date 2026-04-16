package morse

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"sync"
	"time"
	"yama/config"

	"github.com/ebitengine/oto/v3"
)

var (
	otoCtx               *oto.Context
	once                 sync.Once
	audioReady           bool
	queue                []audioBlock
	AudioHardwareDead    bool
	SimTime              float64
	activeCrashSamples   int     // Tracks remaining samples in a lightning crash
	activeCrashIntensity float32 // Tracks the volume of the current crash
)

const SampleRate = 44100

type audioBlock struct {
	samples []byte
	char    string
	index   int
}

type PCMData struct{ Samples []byte }

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

	for i := 0; i < len(queue); i++ {
		block := queue[i]

		if IsStopping {
			break
		}

		if block.char != "" && OnWordChange != nil {
			OnWordChange(block.char, block.index)
		}

		p := otoCtx.NewPlayer(bytes.NewReader(block.samples))
		p.Play()

		sampleCount := len(block.samples) / 2
		expectedDuration := time.Duration(sampleCount) * time.Second / time.Duration(SampleRate)

		timeout := time.Now().Add(expectedDuration + 3*time.Second)

		for p.IsPlaying() {
			if IsStopping {
				break
			}

			if time.Now().After(timeout) {
				go func() { _ = p.Close() }()
				IsStopping = true
				AudioHardwareDead = true
				break
			}

			time.Sleep(1 * time.Millisecond)
		}

		if IsStopping {
			break
		}

		if time.Now().Before(timeout) {
			p.Close()
		}
	}

	queue = nil
}

func InitAudio() error {
	var err error
	once.Do(func() {
		err = startAudioEngine()
	})
	return err
}

func startAudioEngine() error {
	op := &oto.NewContextOptions{
		SampleRate:   SampleRate,
		ChannelCount: 1,
		Format:       oto.FormatSignedInt16LE,
	}
	ctx, ready, e := oto.NewContext(op)
	if e != nil {
		return e
	}
	<-ready
	otoCtx = ctx
	audioReady = true
	return nil
}

/* WDL
func ResetAudioDevice() error {
	audioReady = false

	if otoCtx != nil {
		done := make(chan struct{})
		go func() {
			otoCtx.Suspend()
			close(done)
		}()
		
		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
		}
	}

	time.Sleep(100 * time.Millisecond)

	if err := startAudioEngine(); err != nil {
		log.Printf("Failed to re-initialize audio engine: %v", err)
		return err
	}

	return nil
}
*/

// ==========================================
// WAV EXPORT AUDIO MATH (8-Bit Unsigned)
// ==========================================

func TonePCM8Bit(freq float64, duration int, vol float64, targetSampleRate int) PCMData {
	buf := make([]byte, duration)
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

		s := float32(math.Sin(angle)) * amp
		v := uint8((s * 127.0) + 128.0)
		buf[i] = v
	}
	return PCMData{Samples: buf}
}

func SilencePCM8Bit(duration int) PCMData {
	buf := make([]byte, duration)
	for i := 0; i < duration; i++ {
		buf[i] = 128
	}
	return PCMData{Samples: buf}
}

// ==========================================
// 🎧 LIVE PLAYBACK ENGINE WITH IMPAIRMENTS
// ==========================================

func TonePCM(freq float64, duration int, vol float64) PCMData {
	// SPEED DRIFT
	var speedMod float64 = 0.0
	switch config.User.NoiseSpeedDriftLevel {
	case 1: speedMod = 0.05
	case 2: speedMod = 0.15
	case 3: speedMod = 0.30
	}
	if speedMod > 0 {
		speedFactor := 1.0 + (speedMod * math.Sin(SimTime*2.0*math.Pi/19.0))
		duration = int(float64(duration) * speedFactor)
	}

	// TONE DRIFT
	var toneMod float64 = 0.0
	switch config.User.NoiseToneDriftLevel {
	case 1: toneMod = 10.0
	case 2: toneMod = 30.0
	case 3: toneMod = 60.0
	}
	if toneMod > 0 {
		freq += toneMod * math.Sin(SimTime*2.0*math.Pi/25.0)
	}

	// FADING / QSB
	switch config.User.NoiseFadingLevel {
	case 1: vol *= 0.80 + (0.20 * math.Sin(SimTime*2.0*math.Pi/15.0))
	case 2: vol *= 0.60 + (0.40 * math.Sin(SimTime*2.0*math.Pi/15.0))
	case 3: vol *= 0.525 + (0.475 * math.Sin(SimTime*2.0*math.Pi/15.0))
	}

	// STATIC / QRN BASE HISS
	var staticVol float32 = 0.0
	switch config.User.NoiseStaticLevel {
	case 1: staticVol = 0.05
	case 2: staticVol = 0.15
	case 3: staticVol = 0.40
	}

	buf := make([]byte, duration*2)
	ramp := int(math.Round(0.005 * float64(SampleRate)))

	// KEY CLICKS (Bypass the smooth ramp)
	if config.User.NoiseKeyClick {
		ramp = 0
	}

	if ramp*2 > duration {
		ramp = duration / 2
	}

	for i := 0; i < duration; i++ {
		angle := 2.0 * math.Pi * freq * float64(i) / float64(SampleRate)
		amp := float32(vol)

		if ramp > 0 {
			if i < ramp {
				amp *= float32(i) / float32(ramp)
			} else if i > duration-ramp {
				amp *= float32(duration-i) / float32(ramp)
			}
		}

		s := float32(math.Sin(angle)) * amp

		// THE DIRTY RELAY: Key Click transient (3ms spark)
		if config.User.NoiseKeyClick {
			if i < 132 || i > duration-132 {
				s += 0.6 + (rand.Float32() * 0.4)
			}
		}

		// STATIC HISS & LIGHTNING CRASHES
		if staticVol > 0 {
			if activeCrashSamples == 0 && rand.Float32() < 0.00001 {
				activeCrashSamples = rand.Intn(int(SampleRate / 2)) 
				activeCrashIntensity = (rand.Float32() * 0.6) + (float32(config.User.NoiseStaticLevel) * 0.1)
			}

			noise := (rand.Float32() * 2.0) - 1.0
			currentNoise := noise * staticVol

			if activeCrashSamples > 0 {
				currentNoise += noise * activeCrashIntensity
				activeCrashSamples--
			}

			s += currentNoise
		}

		if s > 1.0 { s = 1.0 }
		if s < -1.0 { s = -1.0 }

		v := int16(s * 32767)
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}

	SimTime += float64(duration) / float64(SampleRate)
	return PCMData{Samples: buf}
}

func SilencePCM(duration int) PCMData {
	var speedMod float64 = 0.0
	switch config.User.NoiseSpeedDriftLevel {
	case 1: speedMod = 0.05
	case 2: speedMod = 0.15
	case 3: speedMod = 0.30
	}
	if speedMod > 0 {
		speedFactor := 1.0 + (speedMod * math.Sin(SimTime*2.0*math.Pi/19.0))
		duration = int(float64(duration) * speedFactor)
	}

	var staticVol float32 = 0.0
	switch config.User.NoiseStaticLevel {
	case 1: staticVol = 0.05
	case 2: staticVol = 0.15
	case 3: staticVol = 0.40
	}

	buf := make([]byte, duration*2)

	for i := 0; i < duration; i++ {
		var v int16 = 3

		if staticVol > 0 {
			if activeCrashSamples == 0 && rand.Float32() < 0.00001 {
				activeCrashSamples = rand.Intn(int(SampleRate / 2))
				activeCrashIntensity = (rand.Float32() * 0.6) + (float32(config.User.NoiseStaticLevel) * 0.1)
			}

			noise := (rand.Float32() * 2.0) - 1.0
			currentNoise := noise * staticVol

			if activeCrashSamples > 0 {
				currentNoise += noise * activeCrashIntensity
				activeCrashSamples--
			}

			s := currentNoise
			if s > 1.0 { s = 1.0 }
			if s < -1.0 { s = -1.0 }

			v = int16(s * 32767)
		}

		binary.LittleEndian.PutUint16(buf[i*2:], uint16(v))
	}

	SimTime += float64(duration) / float64(SampleRate)
	return PCMData{Samples: buf}
}
