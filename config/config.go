package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

var User UserSettings

var Contractions = map[string]string{
	"CAN'T": "CANNOT", "WON'T": "WILL NOT", "DON'T": "DO NOT",
	"I'M": "I AM", "I'VE": "I HAVE", "I'LL": "I WILL", "I'D": "I WOULD",
	"YOU'RE": "YOU ARE", "YOU'VE": "YOU HAVE", "YOU'LL": "YOU WILL", "YOU'D": "YOU WOULD",
	"HE'S": "HE IS", "SHE'S": "SHE IS", "IT'S": "IT IS",
	"AREN'T": "ARE NOT", "COULDN'T": "COULD NOT", "DIDN'T": "DID NOT",
}

// UserSettings holds all user-adjustable features for the YAMA morse code generator.
type UserSettings struct {
	// Timing Modes
	UseStandard   bool `json:"use_standard"`
	UseFarnsworth bool `json:"use_farnsworth"`
	UseWordsworth bool `json:"use_wordsworth"`

	// Core Speed and Tone
	CharacterSpeed int `json:"character_speed"`
	EffectiveSpeed int `json:"effective_speed"`
	Tone           int `json:"tone"`

	// Initial Word Recognition (IWR) Settings
	IWREnabled bool `json:"iwr_enabled"`
	IWRSpeed   int  `json:"iwr_speed"`
	IWRTone    int  `json:"iwr_tone"`

	// Audio Impairments

	NoiseStaticLevel     int  `json:"noise_static_level"`
	NoiseFadingLevel     int  `json:"noise_fading_level"`
	NoiseToneDriftLevel  int  `json:"noise_tone_drift_level"`
	NoiseSpeedDriftLevel int  `json:"noise_speed_drift_level"`
	NoiseKeyClick        bool `json:"noise_key_click"`

	// Output Options
	UseWave 		bool `json:"UseWave"` // Note: Matches the exact capitalization from your JSON

	// Character & Content Options
	UseProsigns            bool   `json:"use_prosigns"`
	UseExtendedPunctuation bool   `json:"use_extended_punctuation"`
	UseSkip                bool   `json:"use_skip"`
	SkipList               string `json:"skip_list"`
	RandomOrder            bool   `json:"random_order"`
	RandomWords            bool   `json:"random_words"`
	WordBuilder            bool   `json:"word_builder"`

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
	MaxEffSpeed  int = 99
	MinCharSpeed int = 10
	MaxCharSpeed int = 100
	MinTone      int = 300
	MaxTone      int = 1400
	MinIWRTone   int = 300
	MaxIWRTone   int = 1400
	MinIWRSpeed  int = 11
	MaxIWRSpeed  int = 110
)

// Helper to reliably get the full, OS-independent path to the JSON file
func getConfigPath() string {

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
	path := getConfigPath()

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
	path := getConfigPath()

	data, err := os.ReadFile(path)

	if err != nil {
		//log.Printf("No config found. Generating defaults at %s", path)

		// 1. SET MANDATORY DEFAULTS SO THE ENGINE DOESN'T PANIC
		User.CharacterSpeed = 20
		User.EffectiveSpeed = 15
		User.Tone = 600

		// Exactly ONE of these must be true!
		User.UseFarnsworth = false
		User.UseStandard = true
		User.UseWordsworth = false

		// Other safe defaults
		User.UseExtendedPunctuation = false
		User.UseSkip = false
		User.SkipList = ""
		User.IWREnabled = true
		User.IWRSpeed = 30
		User.IWRTone = 600
		User.WordBuilder = false
		User.RandomWords = false
		User.RandomOrder = false
		User.RepeatLimit  = 3
		User.StartDelay  = 0

		// 2. Save immediately. This creates the directory AND a valid JSON file.
		SaveConfig()
		return
	}

	if err := json.Unmarshal(data, &User); err != nil {
		log.Printf("Error parsing config file: %v", err)
	}

	if User.IWRTone == 0 {
		User.IWRTone = 600
		SaveConfig()
	}
}
