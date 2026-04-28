package morse

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rivo/tview"
)

type IWRWord struct {
	TargetWord  string
	MatchAny    bool
	StrokeCount int
}

type IWRManager struct {
	WordMap       map[string]*IWRWord
	App           *tview.Application
	StatusMessage string
	IWREnabled    bool
}

var (
	sharedManager *IWRManager
	NonIWRCount   int
	StartTime     time.Time
	PauseStart    time.Time
	TotalPaused   time.Duration
	isFirstRun    bool = true
	IsStopping    bool
)

func SetManager(m *IWRManager) {
	sharedManager = m
}

func GetManager() *IWRManager {
	return sharedManager
}

func (m *IWRManager) LoadIWRFile() (bool, error) {
	var maxIwrLen = 20
	var maxIwrCount = 500
	var file *os.File
	var err error
	var foundPath string
	var fallbackPath string

	// 1. Setup the AppData fallback path
	if docDir, errDir := os.UserConfigDir(); errDir == nil {
		fallbackPath = filepath.Join(docDir, "YAMA", "yamaIWR.txt")
	}

	// 2. Try the local file first (Highest Priority)
	localPath := "./yamaIWR.txt"
	file, err = os.Open(localPath)

	if err == nil {
		foundPath = localPath
	} else if fallbackPath != "" {
		// Failed
		// 3. If local fails, try opening the AppData version
		file, err = os.Open(fallbackPath)
		if err == nil {
			foundPath = fallbackPath
			isFirstRun = true
		} else {
			// 4. If AppData also fails (missing), generate the default!
			if createErr := CreateDefaultIWRFile(fallbackPath); createErr == nil {
				file, err = os.Open(fallbackPath)
				if err == nil {
					foundPath = fallbackPath
				}
			}
		}
	}

	// 5. Final safety check before continuing
	if err != nil || file == nil {
		m.StatusMessage = "yamaIWR.txt not found and could not be generated"
		time.Sleep(700 * time.Millisecond)
		if err != nil {
			return false, err
		}
		return false, fmt.Errorf("file handle is nil")
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	tempMap := make(map[string]*IWRWord)
	cnt := 0

	for scanner.Scan() {
		lineText := strings.TrimSpace(scanner.Text())
		if lineText == "" || strings.HasPrefix(lineText, "#") {
			continue
		}

		for _, word := range strings.Fields(lineText) {
			if len(word) > maxIwrLen {
				continue
			}
			word = strings.ToUpper(word)

			// 1. Check for the wildcard
			matchAny := strings.HasSuffix(word, "*")
			word = strings.TrimSuffix(word, "*")

			// 2. Wash through the token-aware funnel
			word = ProcessMorseString(word)
			word = strings.TrimSpace(word)
			isValid := len(word) > 0

			// 3. Populate the Map
			if isValid {
				// Always add the base word (e.g., "QSL")
				tempMap[word] = &IWRWord{TargetWord: word}
				cnt++

				// If it had an asterisk, generate and store all variants immediately!
				if matchAny {
					punctuations := []string{".", ",", "?", ":"}
					for _, punc := range punctuations {
						if cnt >= maxIwrCount {
							break // Respect the hard cap
						}
						variant := word + punc
						tempMap[variant] = &IWRWord{TargetWord: variant}
						cnt++
					}
				}
			}

			if cnt >= maxIwrCount {
				break
			}
		}
	} // <--- THIS WAS THE MISSING BRACE!

	m.WordMap = tempMap
	m.StatusMessage = fmt.Sprintf("Loaded %d words from %s", cnt, foundPath)
	return isFirstRun, nil
}

func checkIWRFiles() string {
	localPath := ResolvePath("./yamaIWR.txt")

	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}

	// The Foundation: The exact path to the YAMA folder
	yamaDir := filepath.Join(configDir, "YAMA")
	fallbackPath := ResolvePath(filepath.Join(yamaDir, "yamaIWR.txt"))

	// 1. GUARANTEE THE DIRECTORY EXISTS (Safe to run every time)
	os.MkdirAll(yamaDir, 0755)

	// 2. Check local path
	if _, err := os.Stat(localPath); err == nil {
		return localPath
	}

	// 3. Check fallback path
	if _, err := os.Stat(fallbackPath); err == nil {
		return fallbackPath
	}

	// 4. If neither exists, use the correct builder from the morse package
	// Note: Make sure it is capitalized in your morse package so main can see it!
	if createErr := CreateDefaultIWRFile(fallbackPath); createErr != nil {
		log.Printf("Failed to create default IWR file: %v", createErr)
	}

	return fallbackPath
}

func ResolvePath(inputPath string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}

	upperPath := strings.ToUpper(inputPath)
	if strings.HasPrefix(inputPath, "~") {
		inputPath = homeDir + inputPath[1:]
	} else if strings.HasPrefix(upperPath, "$HOME") {
		inputPath = homeDir + inputPath[5:]
	} else if strings.HasPrefix(upperPath, "%HOME%") {
		inputPath = homeDir + inputPath[6:]
	} else if strings.HasPrefix(upperPath, "%USERPROFILE%") {
		inputPath = homeDir + inputPath[13:]
	}

	parsedPath := os.ExpandEnv(inputPath)
	return filepath.Clean(parsedPath)
}

// CreateDefaultIWRFile generates a fresh IWR configuration file with instructions
func CreateDefaultIWRFile(targetPath string) error {
	defaultText := `# Add IWR words, one per line.
# Lines beginning with # are ignored.
# Words ending with * (i.e. QRZ*) match: QRZ QRZ? QRZ. QRZ, QRZ:
# To edit, use cursor keys, Backspace, Insert/Delete; Hit Tab to access buttons.
# words start below
the
and
qrz?
`
	// Write the file to the specified path
	return os.WriteFile(targetPath, []byte(defaultText), 0644)
}

// runtime must be efficient
// Match executes a single, blindingly fast O(1) map lookup.
func (m *IWRManager) Match(word string) bool {
	if !m.IWREnabled {
		return false
	}

	// Because we pre-computed wildcards at load time (e.g., QSL? is its own key),
	// this one check handles absolutely everything.
	return m.WordMap[word] != nil
}
