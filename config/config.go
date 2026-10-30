package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// -----------------------------------------------------------------------------
// GLOBALS
// -----------------------------------------------------------------------------

var User UserSettings
var StatsTotalWords int

var ForceDefaults bool
var StatusLine func(msg string)

// -----------------------------------------------------------------------------
// SPEED LIMIT CONSTANTS (used by UI sliders)
// -----------------------------------------------------------------------------

const MinEffSpeed = 5
const MaxEffSpeed = 40
const MinCharSpeed = 5
const MaxCharSpeed = 60

// IWR speed limits
const MinIWRSpeed = 5
const MaxIWRSpeed = 60

// Tone frequency limits
const MinTone = 300
const MaxTone = 1200

// Tone frequency limits
const MinIWRTone = 300
const MaxIWRTone = 1200

// -----------------------------------------------------------------------------
// CONTRACTIONS (used by parser/skip.go)
// -----------------------------------------------------------------------------

var Contractions = map[string]string{
	"CAN'T": "CANNOT", "WON'T": "WILL NOT", "DON'T": "DO NOT",
	"I'M": "I AM", "I'VE": "I HAVE", "I'LL": "I WILL", "I'D": "I WOULD",
	"YOU'RE": "YOU ARE", "YOU'VE": "YOU HAVE", "YOU'LL": "YOU WILL", "YOU'D": "YOU WOULD",
	"HE'S": "HE IS", "SHE'S": "SHE IS", "IT'S": "IT IS",
	"AREN'T": "ARE NOT", "COULDN'T": "COULD NOT", "DIDN'T": "DID NOT",
}

// -----------------------------------------------------------------------------
// USER SETTINGS STRUCT
// -----------------------------------------------------------------------------

type UserSettings struct {
	CharacterSpeed float64 `json:"character_speed"`
	EffectiveSpeed float64 `json:"effective_speed"`
	EndSpeed       float64 `json:"end_speed"`
	Tone           int     `json:"tone"`
	AlertTone      int     `json:"alert_tone"`

	UseStandard   bool `json:"use_standard"`
	UseFarnsworth bool `json:"use_farnsworth"`
	UseWordsworth bool `json:"use_wordsworth"`

	IWREnabled bool    `json:"iwr_enabled"`
	IWRSpeed   float64 `json:"iwr_speed"`
	IWRTone    int     `json:"iwr_tone"`

	NoiseStaticLevel     int  `json:"noise_static_level"`
	NoiseFadingLevel     int  `json:"noise_fading_level"`
	NoiseToneDriftLevel  int  `json:"noise_tone_drift_level"`
	NoiseSpeedDriftLevel int  `json:"noise_speed_drift_level"`
	NoiseKeyClick        bool `json:"noise_key_click"`

	BrownNoiseLevel int `json:"brown_noise_level"`
	PinkNoiseLevel  int `json:"pink_noise_level"`

	Playprosigns           bool   `json:"play_prosigns"`
	UseExtendedPunctuation bool   `json:"use_extended_punctuation"`
	UseEuropeanChars       bool   `json:"use_european_chars"`
	EuropeanSkipList       string `json:"european_skip_list"`
	UseSkip                bool   `json:"use_skip"`
	SkipList               string `json:"skip_list"`

	WordOrder       bool   `json:"word_order"`
	RandomizeWords  bool   `json:"randomize_words"`
	WordBuilder     bool   `json:"word_builder"`
	WordBuilderSort bool   `json:"word_builder_sort"`
	WordSeparator   string `json:"word_separator"`

	TextBuilder     bool   `json:"text_builder"`
	TextBuilderSort bool   `json:"text_builder_sort"`
	TextSeparator   string `json:"text_separator"`
	TextWordCount   int    `json:"text_word_count"`

	SyllableExpansion bool `json:"syllable_expansion"`

	Flashcard        bool `json:"flashcard"`
	FlashWordCount   int  `json:"flash_word_count"`
	FlashRandomCount bool `json:"flash_random_count"`

	Echo            bool `json:"echo"`
	EchoWordCount   int  `json:"echo_word_count"`
	EchoRandomCount bool `json:"echo_random_count"`
	EchoAutoRetry   bool `json:"echo_auto_retry"`
	EchoTolerance   int  `json:"echo_tolerance"`

	SideTone       bool `json:"sidetone"`
	ErrorTone      bool `json:"errortone"`
	VisualFeedback bool `json:"visual_feedback"`
	PerfectPause   bool `json:"perfect_pause"`
	Mute           bool `json:"mute"`

	StartMsg     bool   `json:"start_msg"`
	StartMsgText string `json:"start_msg_text"`
	EndMsg       bool   `json:"end_msg"`
	EndMsgText   string `json:"end_msg_text"`

	RepeatLimit int `json:"repeat_limit"`
	StartDelay  int `json:"start_delay"`

	KeyerPortSpeed      int    `json:"keyer_port_speed"`
	KeyerPort           string `json:"keyer_port"`
	XmtrPort            string `json:"xmtr_port"`
	KeyLineWireMode     string `json:"key_line_wire_mode"`
	KeyLineIdlePolarity bool   `json:"key_line_idle_polarity"`
	KeyParasiticPower   bool   `json:"key_parastic_power"`
	KeyTimePadding      int    `json:"key_time_padding"`
	KeyDevice           string `json:"key_device"`
	KeyerReversed       bool   `json:"keyer_reversed"`

	UseWave bool `json:"UseWave"`

	DitDahThreshold float64 `json:"dit_dah_threshold"`

	Alert int `json:"alert"`

	LifetimePlaySeconds int `json:"lifetime_play_seconds"`
}

// -----------------------------------------------------------------------------
// PATH
// -----------------------------------------------------------------------------

func GetConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "YAMA", "yama_config2.json")
}

// -----------------------------------------------------------------------------
// LOAD CONFIG (with corruption detection)
// -----------------------------------------------------------------------------

func LoadConfig() {
	path := GetConfigPath()

	if ForceDefaults {
		os.Remove(path)
		setDefaults()
		SaveConfig()
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		setDefaults()
		SaveConfig()
		return
	}

	if isPrettyJSON(string(data)) {
		warn("CONFIG FILE CORRUPTED (pretty JSON). Using defaults.")
		os.Remove(path)
		setDefaults()
		SaveConfig()
		return
	}

	if err := decodeCompactJSON(string(data)); err != nil {
		warn("CONFIG FILE CORRUPTED (invalid compact JSON). Using defaults.")
		os.Remove(path)
		setDefaults()
		SaveConfig()
		return
	}
}

// -----------------------------------------------------------------------------
// WARN USER
// -----------------------------------------------------------------------------

func warn(msg string) {
	if StatusLine != nil {
		StatusLine("[red]" + msg + "[-]")
	} else {
		fmt.Println(msg)
	}
}

// -----------------------------------------------------------------------------
// SAVE CONFIG
// -----------------------------------------------------------------------------

func SaveConfig() error {
	path := GetConfigPath()
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0755)

	data := encodeCompactJSON()
	return os.WriteFile(path, []byte(data), 0644)
}

// -----------------------------------------------------------------------------
// DEFAULTS
// -----------------------------------------------------------------------------

func setDefaults() {
	User.CharacterSpeed = 20
	User.EffectiveSpeed = 15
	User.EndSpeed = 20
	User.Tone = 700
	User.AlertTone = 700

	User.UseStandard = true
	User.UseFarnsworth = false
	User.UseWordsworth = false

	User.IWREnabled = true
	User.IWRSpeed = 30
	User.IWRTone = 700

	User.NoiseStaticLevel = 0
	User.NoiseFadingLevel = 0
	User.NoiseToneDriftLevel = 0
	User.NoiseSpeedDriftLevel = 0
	User.NoiseKeyClick = false

	User.BrownNoiseLevel = 0
	User.PinkNoiseLevel = 0

	User.Playprosigns = false
	User.UseExtendedPunctuation = false
	User.UseEuropeanChars = false
	User.EuropeanSkipList = ""
	User.UseSkip = false
	User.SkipList = ""

	User.WordOrder = false
	User.RandomizeWords = false
	User.WordBuilder = false
	User.WordBuilderSort = false
	User.WordSeparator = ""

	User.TextBuilder = false
	User.TextBuilderSort = false
	User.TextSeparator = ""
	User.TextWordCount = 2

	User.SyllableExpansion = false

	User.Flashcard = false
	User.FlashWordCount = 1
	User.FlashRandomCount = false

	User.Echo = false
	User.EchoWordCount = 1
	User.EchoRandomCount = false
	User.EchoAutoRetry = false
	User.EchoTolerance = 30

	User.SideTone = true
	User.ErrorTone = true
	User.VisualFeedback = true
	User.PerfectPause = false
	User.Mute = false

	User.StartMsg = false
	User.StartMsgText = ""
	User.EndMsg = false
	User.EndMsgText = ""

	User.RepeatLimit = 3
	User.StartDelay = 2

	User.KeyerPortSpeed = 9600
	User.KeyerPort = ""
	User.KeyLineWireMode = "CTS"
	User.KeyLineIdlePolarity = false
	User.KeyParasiticPower = false
	User.KeyTimePadding = 0
	User.KeyDevice = ""
	User.KeyerReversed = false

	User.UseWave = false
	User.DitDahThreshold = 0.5

	User.Alert = 0
	User.LifetimePlaySeconds = 0
}

// -----------------------------------------------------------------------------
// SIMPLE HELPERS
// -----------------------------------------------------------------------------

func isPrettyJSON(data string) bool {
	return strings.Contains(data, "\n") || strings.Contains(data, "  ")
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func intToStr(i int) string {
	return strconv.Itoa(i)
}

func floatToStr(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "true" || s == "1" || s == "yes"
}

func parseInt(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// -----------------------------------------------------------------------------
// UTF‑8 ESCAPE / UNESCAPE
// -----------------------------------------------------------------------------

func escapeUTF8(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 128 {
			b.WriteRune(r)
		} else {
			b.WriteString(fmt.Sprintf("\\u%04X", r))
		}
	}
	return b.String()
}

func unescapeUTF8(s string) string {
	var out strings.Builder
	for len(s) > 0 {
		if strings.HasPrefix(s, "\\u") && len(s) >= 6 {
			hex := s[2:6]
			if r, err := strconv.ParseInt(hex, 16, 32); err == nil {
				out.WriteRune(rune(r))
				s = s[6:]
				continue
			}
		}
		out.WriteByte(s[0])
		s = s[1:]
	}
	return out.String()
}

// -----------------------------------------------------------------------------
// ENCODER (FULL)
// -----------------------------------------------------------------------------

func encodeCompactJSON() string {
	s := "{"

	s += `"character_speed":` + floatToStr(User.CharacterSpeed) + ","
	s += `"effective_speed":` + floatToStr(User.EffectiveSpeed) + ","
	s += `"end_speed":` + floatToStr(User.EndSpeed) + ","

	s += `"tone":` + intToStr(User.Tone) + ","
	s += `"alert_tone":` + intToStr(User.AlertTone) + ","

	s += `"use_standard":` + boolToStr(User.UseStandard) + ","
	s += `"use_farnsworth":` + boolToStr(User.UseFarnsworth) + ","
	s += `"use_wordsworth":` + boolToStr(User.UseWordsworth) + ","

	s += `"iwr_enabled":` + boolToStr(User.IWREnabled) + ","
	s += `"iwr_speed":` + floatToStr(User.IWRSpeed) + ","
	s += `"iwr_tone":` + intToStr(User.IWRTone) + ","

	s += `"noise_static_level":` + intToStr(User.NoiseStaticLevel) + ","
	s += `"noise_fading_level":` + intToStr(User.NoiseFadingLevel) + ","
	s += `"noise_tone_drift_level":` + intToStr(User.NoiseToneDriftLevel) + ","
	s += `"noise_speed_drift_level":` + intToStr(User.NoiseSpeedDriftLevel) + ","
	s += `"noise_key_click":` + boolToStr(User.NoiseKeyClick) + ","

	s += `"brown_noise_level":` + intToStr(User.BrownNoiseLevel) + ","
	s += `"pink_noise_level":` + intToStr(User.PinkNoiseLevel) + ","

	s += `"play_prosigns":` + boolToStr(User.Playprosigns) + ","
	s += `"use_extended_punctuation":` + boolToStr(User.UseExtendedPunctuation) + ","
	s += `"use_european_chars":` + boolToStr(User.UseEuropeanChars) + ","

	s += `"european_skip_list":"` + escapeUTF8(User.EuropeanSkipList) + `",`
	s += `"use_skip":` + boolToStr(User.UseSkip) + ","
	s += `"skip_list":"` + escapeUTF8(User.SkipList) + `",`

	s += `"word_order":` + boolToStr(User.WordOrder) + ","
	s += `"randomize_words":` + boolToStr(User.RandomizeWords) + ","

	s += `"word_builder":` + boolToStr(User.WordBuilder) + ","
	s += `"word_builder_sort":` + boolToStr(User.WordBuilderSort) + ","
	s += `"word_separator":"` + escapeUTF8(User.WordSeparator) + ","

	s += `"text_builder":` + boolToStr(User.TextBuilder) + ","
	s += `"text_builder_sort":` + boolToStr(User.TextBuilderSort) + ","
	s += `"text_separator":"` + escapeUTF8(User.TextSeparator) + ","
	s += `"text_word_count":` + intToStr(User.TextWordCount) + ","

	s += `"syllable_expansion":` + boolToStr(User.SyllableExpansion) + ","

	s += `"flashcard":` + boolToStr(User.Flashcard) + ","
	s += `"flash_word_count":` + intToStr(User.FlashWordCount) + ","
	s += `"flash_random_count":` + boolToStr(User.FlashRandomCount) + ","

	s += `"echo":` + boolToStr(User.Echo) + ","
	s += `"echo_word_count":` + intToStr(User.EchoWordCount) + ","
	s += `"echo_random_count":` + boolToStr(User.EchoRandomCount) + ","
	s += `"echo_auto_retry":` + boolToStr(User.EchoAutoRetry) + ","
	s += `"echo_tolerance":` + intToStr(User.EchoTolerance) + ","

	s += `"sidetone":` + boolToStr(User.SideTone) + ","
	s += `"errortone":` + boolToStr(User.ErrorTone) + ","
	s += `"visual_feedback":` + boolToStr(User.VisualFeedback) + ","
	s += `"perfect_pause":` + boolToStr(User.PerfectPause) + ","
	s += `"mute":` + boolToStr(User.Mute) + ","

	s += `"start_msg":` + boolToStr(User.StartMsg) + ","
	s += `"start_msg_text":"` + escapeUTF8(User.StartMsgText) + `",`
	s += `"end_msg":` + boolToStr(User.EndMsg) + ","
	s += `"end_msg_text":"` + escapeUTF8(User.EndMsgText) + `",`

	s += `"repeat_limit":` + intToStr(User.RepeatLimit) + ","
	s += `"start_delay":` + intToStr(User.StartDelay) + ","

	s += `"keyer_port_speed":` + intToStr(User.KeyerPortSpeed) + ","
	s += `"keyer_port":"` + escapeUTF8(User.KeyerPort) + `",`
	s += `"xmtr_port":"` + escapeUTF8(User.XmtrPort) + `",`
	s += `"key_line_wire_mode":"` + escapeUTF8(User.KeyLineWireMode) + `",`
	s += `"key_line_idle_polarity":` + boolToStr(User.KeyLineIdlePolarity) + ","
	s += `"key_parastic_power":` + boolToStr(User.KeyParasiticPower) + ","
	s += `"key_time_padding":` + intToStr(User.KeyTimePadding) + ","
	s += `"key_device":"` + escapeUTF8(User.KeyDevice) + `",`
	s += `"keyer_reversed":` + boolToStr(User.KeyerReversed) + ","

	s += `"UseWave":` + boolToStr(User.UseWave) + ","

	s += `"dit_dah_threshold":` + floatToStr(User.DitDahThreshold) + ","

	s += `"alert":` + intToStr(User.Alert) + ","

	s += `"lifetime_play_seconds":` + intToStr(User.LifetimePlaySeconds)

	s += "}"
	return s
}

// -----------------------------------------------------------------------------
// DECODER (FULL)
// -----------------------------------------------------------------------------

func decodeCompactJSON(data string) error {
	data = strings.TrimSpace(data)
	data = strings.TrimPrefix(data, "{")
	data = strings.TrimSuffix(data, "}")

	fields := strings.Split(data, ",")
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}

		parts := strings.SplitN(f, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid field: %s", f)
		}

		key := strings.Trim(parts[0], `" `)
		val := strings.Trim(parts[1], `" `)

		switch key {

		case "character_speed":
			User.CharacterSpeed = parseFloat(val)
		case "effective_speed":
			User.EffectiveSpeed = parseFloat(val)
		case "end_speed":
			User.EndSpeed = parseFloat(val)

		case "tone":
			User.Tone = parseInt(val)
		case "alert_tone":
			User.AlertTone = parseInt(val)

		case "use_standard":
			User.UseStandard = parseBool(val)
		case "use_farnsworth":
			User.UseFarnsworth = parseBool(val)
		case "use_wordsworth":
			User.UseWordsworth = parseBool(val)

		case "iwr_enabled":
			User.IWREnabled = parseBool(val)
		case "iwr_speed":
			User.IWRSpeed = parseFloat(val)
		case "iwr_tone":
			User.IWRTone = parseInt(val)

		case "noise_static_level":
			User.NoiseStaticLevel = parseInt(val)
		case "noise_fading_level":
			User.NoiseFadingLevel = parseInt(val)
		case "noise_tone_drift_level":
			User.NoiseToneDriftLevel = parseInt(val)
		case "noise_speed_drift_level":
			User.NoiseSpeedDriftLevel = parseInt(val)
		case "noise_key_click":
			User.NoiseKeyClick = parseBool(val)

		case "brown_noise_level":
			User.BrownNoiseLevel = parseInt(val)
		case "pink_noise_level":
			User.PinkNoiseLevel = parseInt(val)

		case "play_prosigns":
			User.Playprosigns = parseBool(val)
		case "use_extended_punctuation":
			User.UseExtendedPunctuation = parseBool(val)
		case "use_european_chars":
			User.UseEuropeanChars = parseBool(val)

		case "european_skip_list":
			User.EuropeanSkipList = unescapeUTF8(val)
		case "use_skip":
			User.UseSkip = parseBool(val)
		case "skip_list":
			User.SkipList = unescapeUTF8(val)

		case "word_order":
			User.WordOrder = parseBool(val)
		case "randomize_words":
			User.RandomizeWords = parseBool(val)

		case "word_builder":
			User.WordBuilder = parseBool(val)
		case "word_builder_sort":
			User.WordBuilderSort = parseBool(val)
		case "word_separator":
			User.WordSeparator = unescapeUTF8(val)

		case "text_builder":
			User.TextBuilder = parseBool(val)
		case "text_builder_sort":
			User.TextBuilderSort = parseBool(val)
		case "text_separator":
			User.TextSeparator = unescapeUTF8(val)
		case "text_word_count":
			User.TextWordCount = parseInt(val)

		case "syllable_expansion":
			User.SyllableExpansion = parseBool(val)

		case "flashcard":
			User.Flashcard = parseBool(val)
		case "flash_word_count":
			User.FlashWordCount = parseInt(val)
		case "flash_random_count":
			User.FlashRandomCount = parseBool(val)

		case "echo":
			User.Echo = parseBool(val)
		case "echo_word_count":
			User.EchoWordCount = parseInt(val)
		case "echo_random_count":
			User.EchoRandomCount = parseBool(val)
		case "echo_auto_retry":
			User.EchoAutoRetry = parseBool(val)
		case "echo_tolerance":
			User.EchoTolerance = parseInt(val)

		case "sidetone":
			User.SideTone = parseBool(val)
		case "errortone":
			User.ErrorTone = parseBool(val)
		case "visual_feedback":
			User.VisualFeedback = parseBool(val)
		case "perfect_pause":
			User.PerfectPause = parseBool(val)
		case "mute":
			User.Mute = parseBool(val)

		case "start_msg":
			User.StartMsg = parseBool(val)
		case "start_msg_text":
			User.StartMsgText = unescapeUTF8(val)
		case "end_msg":
			User.EndMsg = parseBool(val)
		case "end_msg_text":
			User.EndMsgText = unescapeUTF8(val)

		case "repeat_limit":
			User.RepeatLimit = parseInt(val)
		case "start_delay":
			User.StartDelay = parseInt(val)

		case "keyer_port_speed":
			User.KeyerPortSpeed = parseInt(val)
		case "keyer_port":
			User.KeyerPort = unescapeUTF8(val)
		case "key_line_wire_mode":
			User.KeyLineWireMode = unescapeUTF8(val)
		case "key_line_idle_polarity":
			User.KeyLineIdlePolarity = parseBool(val)
		case "key_parastic_power":
			User.KeyParasiticPower = parseBool(val)
		case "key_time_padding":
			User.KeyTimePadding = parseInt(val)
		case "key_device":
			User.KeyDevice = unescapeUTF8(val)
		case "keyer_reversed":
			User.KeyerReversed = parseBool(val)

		case "UseWave":
			User.UseWave = parseBool(val)

		case "dit_dah_threshold":
			User.DitDahThreshold = parseFloat(val)

		case "alert":
			User.Alert = parseInt(val)

		case "lifetime_play_seconds":
			User.LifetimePlaySeconds = parseInt(val)

		default:
			// ignore unknown keys
		}
	}

	return nil
}
