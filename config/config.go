package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

var User UserSettings
var StatsTotalWords int

var Contractions = map[string]string{
	"CAN'T": "CANNOT", "WON'T": "WILL NOT", "DON'T": "DO NOT",
	"I'M": "I AM", "I'VE": "I HAVE", "I'LL": "I WILL", "I'D": "I WOULD",
	"YOU'RE": "YOU ARE", "YOU'VE": "YOU HAVE", "YOU'LL": "YOU WILL", "YOU'D": "YOU WOULD",
	"HE'S": "HE IS", "SHE'S": "SHE IS", "IT'S": "IT IS",
	"AREN'T": "ARE NOT", "COULDN'T": "COULD NOT", "DIDN'T": "DID NOT",
}

// UserSettings holds all user-adjustable features
type UserSettings struct {
	KeyerPortSpeed      int    `json:"keyer_port_speed"`
	KeyerPort           string `json:"keyer_port"`
	KeyLineMode         string `json:"key_line_mode"`
	KeyLineIdlePolarity bool   `json:"key_line_idle_polarity"`
	KeyParasiticPower   bool   `json:"key_parastic_power"`
	KeyTimePadding      int    `json:"key_time_padding"`

	LifetimePlaySeconds int `json:"lifetime_play_seconds"`
	ResponseMS          int `json:"response_MS"`
	GroupSendMS         int `json:"group_send_MS"`

	// Timing Modes
	UseStandard   bool `json:"use_standard"`
	UseFarnsworth bool `json:"use_farnsworth"`
	UseWordsworth bool `json:"use_wordsworth"`

	// Core Speed and Tone
	CharacterSpeed float64 `json:"character_speed"`
	EndSpeed       float64 `json:"end_speed"`
	EffectiveSpeed float64 `json:"effective_speed"`
	Tone           int     `json:"tone"`
	AlertTone      int     `json:"alert_tone"`
	Alert          int     `json:"alert"`

	// Initial Word Recognition (IWR) Settings
	IWREnabled bool    `json:"iwr_enabled"`
	IWRSpeed   float64 `json:"iwr_speed"`
	IWRTone    int     `json:"iwr_tone"`

	// Audio Impairments
	NoiseStaticLevel     int  `json:"noise_static_level"`
	NoiseFadingLevel     int  `json:"noise_fading_level"`
	BrownNoiseLevel      int  `json:"brown_noise_level"`
	PinkNoiseLevel       int  `json:"pink_noise_level"`
	NoiseToneDriftLevel  int  `json:"noise_tone_drift_level"`
	NoiseSpeedDriftLevel int  `json:"noise_speed_drift_level"`
	NoiseKeyClick        bool `json:"noise_key_click"`

	// Output Options
	UseWave bool `json:"UseWave"`

	// Character & Content Options
	Playprosigns           bool   `json:"play_prosigns"`
	UseExtendedPunctuation bool   `json:"use_extended_punctuation"`
	UseEuropeanChars       bool   `json:"use_european_chars"`
	EuropeanSkipList       string `json:"european_skip_list"`
	UseSkip                bool   `json:"use_skip"`
	SkipList               string `json:"skip_list"`
	WordOrder              bool   `json:"word_order"`
	RandomizeWords         bool   `json:"randomize_words"`
	WordBuilder            bool   `json:"word_builder"`
	WordSeparator          string `json:"word_separator"`
	TextBuilder            bool   `json:"text_builder"`
	WordBuilderSort        bool   `json:"word_builder_sort"`
	TextBuilderSort        bool   `json:"text_builder_sort"`
	TextSeparator          string `json:"text_separator"`
	TextWordCount          int    `json:"text_word_count"`
	SyllableExpansion      bool   `json:"syllable_expansion"`
	Flashcard              bool   `json:"flashcard"`
	FlashWordCount         int    `json:"flash_word_count"`
	FlashRandomCount       bool   `json:"flash_random_count"`
	EchoTolerance          int    `json:"echo_tolerance"`
	Echo                   bool   `json:"echo"`
	EchoWordCount          int    `json:"echo_word_count"`
	EchoRandomCount        bool   `json:"echo_random_count"`
	EchoAutoRetry          bool   `json:"echo_auto_retry"`
	SideTone               bool   `json:"sidetone"`
	ErrorTone              bool   `json:"errortone"`
	VisualFeedback         bool   `json:"visual_feedback"`
	PerfectPause           bool   `json:"perfect_pause"`
	Mute                   bool   `json:"mute"`

	// Messaging & Flow Control
	StartMsg     bool   `json:"start_msg"`
	StartMsgText string `json:"start_msg_text"`
	EndMsg       bool   `json:"end_msg"`
	EndMsgText   string `json:"end_msg_text"`
	RepeatLimit  int    `json:"repeat_limit"`
	StartDelay   int    `json:"start_delay"`
}

const (
	MinEffSpeed  int = 5
	MaxEffSpeed  int = 249
	MinCharSpeed int = 10
	MaxCharSpeed int = 250
	MinTone      int = 200
	MaxTone      int = 2000
	MinIWRTone   int = 300
	MaxIWRTone   int = 2000
	MinIWRSpeed  int = 11
	MaxIWRSpeed  int = 251
)

// Helper to reliably get the full, OS-independent path to the JSON file
func GetConfigPath() string {

	configDir, err := os.UserConfigDir()
	if err != nil {
		// Absolute fallback if the OS is completely unrecognizable
		log.Printf("Could not find OS config dir, falling back to local: %v", err)
		configDir = "."
	}

	return filepath.Join(configDir, "YAMA", "yama_config.json")
}

// SaveConfig must be called whenever the UI updates settings
func SaveConfig() error {
	path := GetConfigPath()

	// 1. THE MISSING PIECE: Force the OS to build AppData/YAMA if it's missing
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Fatal error creating config directory: %v", err)
		return err
	}

	// 2. Marshal your global 'User' struct to JSON
	data, err := json.MarshalIndent(User, "", "  ")
	if err != nil {
		log.Printf("Error marshaling JSON: %v", err)
		return err
	}

	// 3. Save the file safely
	return os.WriteFile(path, data, 0644)
}

func LoadConfig() {
	path := GetConfigPath()

	data, err := os.ReadFile(path)

	if err != nil {

		// 1. SET MANDATORY DEFAULTS SO THE ENGINE DOESN'T PANIC
		User.CharacterSpeed = 20
		User.EffectiveSpeed = 15
		User.Tone = 700
		User.ResponseMS = 5000
		User.GroupSendMS = 9000

		// Exactly ONE of these must be true!
		User.UseFarnsworth = false
		User.UseStandard = true
		User.UseWordsworth = false

		// Other safe defaults
		User.UseExtendedPunctuation = false
		User.UseEuropeanChars = false
		User.UseSkip = false
		User.SkipList = ""
		User.IWREnabled = true
		User.IWRSpeed = 30
		User.IWRTone = 700
		User.WordBuilder = false
		User.WordBuilderSort = false
		User.TextBuilderSort = false
		User.TextBuilder = false
		User.RandomizeWords = false
		User.WordOrder = false
		User.RepeatLimit = 3
		User.TextWordCount = 2
		User.StartDelay = 2
		User.SyllableExpansion = false
		User.Flashcard = false
		User.FlashWordCount = 1
		User.EchoWordCount = 1
		User.EchoTolerance = 30
		User.FlashRandomCount = false
		User.EchoRandomCount = false
		User.EchoAutoRetry = false
		User.StartDelay = 0
		User.BrownNoiseLevel = 0
		User.PinkNoiseLevel = 0
		User.KeyerPortSpeed = 9600
		User.KeyerPort = "COM3"
		User.KeyLineMode = "CTS:8-DTR:4"
		User.KeyParasiticPower = false
		User.KeyLineIdlePolarity = false
		User.KeyTimePadding = 1
		User.Alert = 0
		User.AlertTone = 500
		User.SideTone = true
		User.ErrorTone = true
		User.VisualFeedback = true
		User.PerfectPause = false
		User.Mute = false

		// 2. Save immediately. This creates the directory AND a valid JSON file.
		SaveConfig()
		return
	}

	if err := json.Unmarshal(data, &User); err != nil {
		log.Printf("Error parsing config file: %v\nRemove the file and start YAMA again", err)
	}

	if User.IWRTone == 0 {
		User.IWRTone = 600
		SaveConfig()
	}
	if User.KeyerPortSpeed == 0 {
		User.KeyerPortSpeed = 9600
		SaveConfig()
	}
	if User.KeyerPort == "" {
		User.KeyerPort = "COM3"
		SaveConfig()
	}
	if User.KeyLineMode == "" {
		User.KeyLineMode = "CTS:8-DTR:4"
		SaveConfig()
	}
}
