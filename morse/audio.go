package morse

import (
	"encoding/binary"
	"io"
	"math"
	"math/rand"
	"os"
	"sync"
	"time"
	"yama/config"

	"github.com/ebitengine/oto/v3"
)

var (
	otoCtx               *oto.Context
	once                 sync.Once
	audioReady           bool
	AudioHardwareDead    bool
	SimTime              float64
	activeCrashSamples   int     // Tracks remaining samples in a lightning crash
	activeCrashIntensity float32 // Tracks the volume of the current crash
	queue                []audioBlock
	lastBrown            float32 // Tracks the brown noise filter state across gapless elements
	pink0                float32
	pink1                float32
	pink2                float32

	// Inside your morse package
	OnFatalError func(errorMessage string)
)

const SampleRate = 48000
const bytesPerFrame = 4 // 2 channels * 2 bytes (16-bit)

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

// --- THE GAPLESS AUDIO STREAMER ---
type gaplessReader struct {
	blocks       []audioBlock
	currentBlock int
	offset       int
}

func (g *gaplessReader) Read(p []byte) (n int, err error) {
	if IsStopping {
		return 0, io.EOF
	}

	if g.currentBlock >= len(g.blocks) {
		return 0, io.EOF
	}

	block := g.blocks[g.currentBlock]

	copied := copy(p, block.samples[g.offset:])
	g.offset += copied

	if g.offset >= len(block.samples) {
		g.currentBlock++
		g.offset = 0
	}

	return copied, nil
}

func Flush() {
	if !audioReady || otoCtx == nil || len(queue) == 0 {
		return
	}

	// Take a snapshot of the current queue and clear the global
	blocks := queue

	// 1. Calculate the exact mathematical duration of the ENTIRE word
	totalFrames := 0
	for i := 0; i < len(blocks); i++ {
		totalFrames += len(blocks[i].samples) / bytesPerFrame
	}
	expectedDuration := time.Duration(totalFrames) * time.Second / time.Duration(SampleRate)
	timeout := time.Now().Add(expectedDuration + 5*time.Second)

	// 2. Wrap our snapshot in the gapless streamer
	reader := &gaplessReader{blocks: blocks}
	p := otoCtx.NewPlayer(reader)

	if config.User.Mute && !config.User.Echo {
		p.SetVolume(0)
	}

	p.Play() // after vol set else get a blip

	// 3. UI watchdog uses the same snapshot
	go func(uiQueue []audioBlock) {
		startTime := time.Now()
		var elapsed time.Duration

		for _, block := range uiQueue {
			if IsStopping {
				break
			}

			if block.char != "" && OnWordChange != nil {
				OnWordChange(block.char, block.index)
			}

			frameCount := len(block.samples) / bytesPerFrame
			elapsed += time.Duration(frameCount) * time.Second / time.Duration(SampleRate)
			targetTime := startTime.Add(elapsed)

			sleepDur := time.Until(targetTime)
			if sleepDur > 0 {
				time.Sleep(sleepDur)
			}
		}
	}(blocks)

	// 4. Hardware watchdog
	for p.IsPlaying() {
		if IsStopping {
			break
		}

		if time.Now().After(timeout) {
			if OnFatalError != nil {
				OnFatalError("Audio hardware dead! The OS audio bridge stopped responding.")
			} else {
				os.Exit(1)
			}
		}

		time.Sleep(5 * time.Millisecond)
	}

	if time.Now().Before(timeout) {
		p.Close()
	}
	queue = nil
}

// ==========================================
// WAV EXPORT AUDIO MATH (8-Bit Unsigned)
// ==========================================
func TonePCM16Bit(freq float64, duration int, vol float64, targetSampleRate int) PCMData {
	buf := make([]byte, duration*bytesPerFrame)

	// 1. Calculate our ideal maximum 5ms ramp
	maxRamp := int(math.Round(0.005 * float64(targetSampleRate)))

	// 2. THE QRQ MAGIC: Dynamically scale the ramp so it never consumes
	// more than 25% of the total element length, preserving a 50% flat-top.
	ramp := maxRamp
	if duration/4 < maxRamp {
		ramp = duration / 4
	}

	for i := 0; i < duration; i++ {
		angle := 2.0 * math.Pi * freq * float64(i) / float64(targetSampleRate)
		amp := float64(vol) // Use float64 for smooth math

		// 3. The Raised Cosine Envelope
		if i < ramp {
			// Attack Phase: 0.0 to 1.0
			progress := float64(i) / float64(ramp)
			amp *= (1.0 - math.Cos(progress*math.Pi)) / 2.0
		} else if i > duration-ramp {
			// Release Phase: 1.0 to 0.0
			progress := float64(duration-i) / float64(ramp)
			amp *= (1.0 - math.Cos(progress*math.Pi)) / 2.0
		}

		s := math.Sin(angle) * amp
		v := int16(s * 32767) // Scale to 16-bit signed integer

		// Write to Left and Right channels
		binary.LittleEndian.PutUint16(buf[i*bytesPerFrame:], uint16(v))   // Left
		binary.LittleEndian.PutUint16(buf[i*bytesPerFrame+2:], uint16(v)) // Right
	}
	return PCMData{Samples: buf}
}

func SilencePCM16Bit(duration int) PCMData {
	buf := make([]byte, duration*bytesPerFrame)
	return PCMData{Samples: buf}
}

// ==========================================
// LIVE PLAYBACK ENGINE WITH IMPAIRMENTS
// ==========================================

func TonePCM(freq float64, duration int, vol float64) PCMData {
	if config.User.Echo {
		buf := make([]byte, duration*bytesPerFrame)
		maxRamp := int(math.Round(0.005 * float64(SampleRate)))
		ramp := maxRamp

		if duration/4 < maxRamp {
			ramp = duration / 4
		}

		for i := 0; i < duration; i++ {
			angle := 2.0 * math.Pi * freq * float64(i) / float64(SampleRate)
			amp := vol

			if i < ramp {
				progress := float64(i) / float64(ramp)
				amp *= (1.0 - math.Cos(progress*math.Pi)) / 2.0
			} else if i > duration-ramp {
				progress := float64(duration-i) / float64(ramp)
				amp *= (1.0 - math.Cos(progress*math.Pi)) / 2.0
			}

			s := math.Sin(angle) * amp
			v := int16(s * 32767)

			binary.LittleEndian.PutUint16(buf[i*bytesPerFrame:], uint16(v))
			binary.LittleEndian.PutUint16(buf[i*bytesPerFrame+2:], uint16(v))
		}

		SimTime += float64(duration) / float64(SampleRate)
		return PCMData{Samples: buf}
	}

	// IMPAIRMENT LOGIC
	var speedMod float64 = 0.0
	switch config.User.NoiseSpeedDriftLevel {
	case 1:
		speedMod = 0.05
	case 2:
		speedMod = 0.15
	case 3:
		speedMod = 0.30
	}
	if speedMod > 0 {
		speedFactor := 1.0 + (speedMod * math.Sin(SimTime*2.0*math.Pi/19.0))
		duration = int(float64(duration) * speedFactor)
	}

	var toneMod float64 = 0.0
	switch config.User.NoiseToneDriftLevel {
	case 1:
		toneMod = 10.0
	case 2:
		toneMod = 30.0
	case 3:
		toneMod = 60.0
	}
	if toneMod > 0 {
		freq += toneMod * math.Sin(SimTime*2.0*math.Pi/25.0)
	}

	switch config.User.NoiseFadingLevel {
	case 1:
		vol *= 0.80 + (0.20 * math.Sin(SimTime*2.0*math.Pi/15.0))
	case 2:
		vol *= 0.60 + (0.40 * math.Sin(SimTime*2.0*math.Pi/15.0))
	case 3:
		vol *= 0.525 + (0.475 * math.Sin(SimTime*2.0*math.Pi/15.0))
	}

	var staticVol float32 = 0.0
	switch config.User.NoiseStaticLevel {
	case 1:
		staticVol = 0.05
	case 2:
		staticVol = 0.15
	case 3:
		staticVol = 0.40
	}

	var brownVol float32 = 0.0
	switch config.User.BrownNoiseLevel {
	case 1:
		brownVol = 0.47
	case 2:
		brownVol = 0.15
	case 3:
		brownVol = 0.30
	case 4:
		brownVol = 0.50
	}

	var pinkVol float32 = 0.0
	switch config.User.PinkNoiseLevel {
	case 1:
		pinkVol = 0.047
	case 2:
		pinkVol = 0.15
	case 3:
		pinkVol = 0.30
	case 4:
		pinkVol = 0.50
	}

	buf := make([]byte, duration*bytesPerFrame)

	maxRamp := int(math.Round(0.005 * float64(SampleRate)))
	ramp := maxRamp

	if duration/4 < maxRamp {
		ramp = duration / 4
	}
	if config.User.NoiseKeyClick {
		ramp = 0
	}

	for i := 0; i < duration; i++ {
		angle := 2.0 * math.Pi * freq * float64(i) / float64(SampleRate)
		amp := float64(vol)

		if ramp > 0 {
			if i < ramp {
				progress := float64(i) / float64(ramp)
				amp *= (1.0 - math.Cos(progress*math.Pi)) / 2.0
			} else if i > duration-ramp {
				progress := float64(duration-i) / float64(ramp)
				amp *= (1.0 - math.Cos(progress*math.Pi)) / 2.0
			}
		}

		s := float32(math.Sin(angle)) * float32(amp)

		if config.User.NoiseKeyClick {
			if i < 132 || i > duration-132 {
				s += 0.6 + (rand.Float32() * 0.4)
			}
		}

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

		if brownVol > 0 {
			white := (rand.Float32() * 2.0) - 1.0
			lastBrown = (lastBrown * 0.95) + (white * 0.05)
			s += lastBrown * brownVol
		}

		if pinkVol > 0 {
			white := (rand.Float32() * 2.0) - 1.0
			pink0 = (0.99765 * pink0) + (white * 0.0990460)
			pink1 = (0.96300 * pink2) + (white * 0.2965164)
			pink2 = (0.57000 * pink2) + (white * 1.0526913)
			pink := pink0 + pink1 + pink2 + (white * 0.1848)
			s += (pink * 0.15) * pinkVol
		}

		s64 := math.Tanh(float64(s))
		v := int16(s64 * 32767)

		binary.LittleEndian.PutUint16(buf[i*bytesPerFrame:], uint16(v))
		binary.LittleEndian.PutUint16(buf[i*bytesPerFrame+2:], uint16(v))
	}

	SimTime += float64(duration) / float64(SampleRate)
	return PCMData{Samples: buf}
}

func SilencePCM(duration int) PCMData {
	if config.User.Echo {
		buf := make([]byte, duration*bytesPerFrame)
		SimTime += float64(duration) / float64(SampleRate)
		return PCMData{Samples: buf}
	}

	var speedMod float64 = 0.0
	switch config.User.NoiseSpeedDriftLevel {
	case 1:
		speedMod = 0.05
	case 2:
		speedMod = 0.15
	case 3:
		speedMod = 0.30
	}
	if speedMod > 0 {
		speedFactor := 1.0 + (speedMod * math.Sin(SimTime*2.0*math.Pi/19.0))
		duration = int(float64(duration) * speedFactor)
	}

	var staticVol float32 = 0.0
	switch config.User.NoiseStaticLevel {
	case 1:
		staticVol = 0.05
	case 2:
		staticVol = 0.15
	case 3:
		staticVol = 0.40
	}

	var brownVol float32 = 0.0
	switch config.User.BrownNoiseLevel {
	case 1:
		brownVol = 0.15
	case 2:
		brownVol = 0.30
	case 3:
		brownVol = 0.50
	}

	var pinkVol float32 = 0.0
	switch config.User.PinkNoiseLevel {
	case 1:
		pinkVol = 0.15
	case 2:
		pinkVol = 0.30
	case 3:
		pinkVol = 0.50
	}

	buf := make([]byte, duration*bytesPerFrame)

	for i := 0; i < duration; i++ {
		var v int16 = 3
		var s float32 = 0.0

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

		if brownVol > 0 {
			white := (rand.Float32() * 2.0) - 1.0
			lastBrown = (lastBrown * 0.95) + (white * 0.05)
			s += lastBrown * brownVol
		}

		if pinkVol > 0 {
			white := (rand.Float32() * 2.0) - 1.0
			pink0 = (0.99765 * pink0) + (white * 0.0990460)
			pink1 = (0.96300 * pink1) + (white * 0.2965164)
			pink2 = (0.57000 * pink2) + (white * 1.0526913)
			pink := pink0 + pink1 + pink2 + (white * 0.1848)
			s += (pink * 0.15) * pinkVol
		}

		s64 := math.Tanh(float64(s))
		v = int16(s64 * 32767)

		binary.LittleEndian.PutUint16(buf[i*bytesPerFrame:], uint16(v))
		binary.LittleEndian.PutUint16(buf[i*bytesPerFrame+2:], uint16(v))
	}

	SimTime += float64(duration) / float64(SampleRate)
	return PCMData{Samples: buf}
}

// ==========================================
// LIVE OSCILLATOR (For Interactive Keying)
// ==========================================

type LiveOscillator struct {
	Freq       float64
	SampleRate float64
	phase      float64
	debugCount int
}

func (o *LiveOscillator) Read(p []byte) (n int, err error) {
	if o.debugCount == 0 {
		o.debugCount++
	}

	// 4 bytes per frame (2 channels * 2 bytes)
	frames := len(p) / bytesPerFrame

	for i := 0; i < frames; i++ {
		s := math.Sin(o.phase * 2.0 * math.Pi)
		val := int16(s * 0.5 * 32767)

		binary.LittleEndian.PutUint16(p[i*bytesPerFrame:], uint16(val))
		binary.LittleEndian.PutUint16(p[i*bytesPerFrame+2:], uint16(val))

		o.phase += o.Freq / o.SampleRate
		if o.phase > 1.0 {
			o.phase -= 1.0
		}
	}

	return len(p), nil
}

func StartOscillator(freq float64) (*LiveOscillator, *oto.Player) {
	if !audioReady || otoCtx == nil {
		return nil, nil
	}

	osc := &LiveOscillator{
		Freq:       freq,
		SampleRate: float64(SampleRate),
	}

	player := otoCtx.NewPlayer(osc)
	player.SetVolume(0)
	player.Play()

	return osc, player
}

// ==========================================
// AUDIO ENGINE INITIALIZATION
// ==========================================

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
		ChannelCount: 2,
		Format:       oto.FormatSignedInt16LE,
		BufferSize:   time.Millisecond * 15,
	}

	var readyChan chan struct{}
	var err error

	otoCtx, readyChan, err = oto.NewContext(op)
	if err != nil {
		return err
	}
	<-readyChan

	audioReady = true
	return nil
}
