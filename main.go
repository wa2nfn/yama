package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"yama/config"
	"yama/morse"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type AppState int

const (
	StateIdle AppState = iota
	StatePlaying
	StatePaused
	StateStopped
	Ver = "1.0.4"
)

const AppBackgroundColor = "#1B2B44"

var (
	app        *tview.Application
	pages      *tview.Pages
	header     *tview.TextView
	statusLine *tview.TextView
	inputArea  *tview.TextArea
	blueLine   *tview.TextView
	mainFlex   *tview.Flex

	currentState   AppState  = StateIdle
	lastPlayPause  time.Time // prevent PayPause mashing
	actualText     string
	fullTextToPlay string

	statsTotalWords int
	statsIWRWords   int
	statsIWRList    []string
	statsIWRMap     = make(map[string]int)
	isBlocked       bool
	finalPlayTime   time.Duration

	colorTagRegex = regexp.MustCompile(`\[.*?\]`)

	// Flag to prevent the text box from resetting the menu when the engine updates it
	isProgrammaticUpdate bool
)

func checkIWRFiles() (targetPath string) {
	localPath := morse.ResolvePath("./yamaIWR.txt")

	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	fallbackPath := morse.ResolvePath(filepath.Join(configDir, "YAMA", "yamaIWR.txt"))

	if _, err := os.Stat(localPath); err == nil {
		return localPath
	}

	if _, err := os.Stat(fallbackPath); err == nil {
		return fallbackPath
	}

	if createErr := morse.CreateDefaultIWRFile(fallbackPath); createErr == nil {
		return fallbackPath
	}

	return fallbackPath
}

func main() {
	config.LoadConfig()

	logPath := morse.ResolvePath("yama.log")
	logFile, _ := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	log.SetOutput(logFile)

	if err := morse.InitAudio(); err != nil {
		log.Fatal(err)
	}

	morse.RebuildMorseTable(config.User.UseExtendedPunctuation, config.User.UseEuropeanChars, config.User.UseSkip, config.User.SkipList, config.User.EuropeanSkipList)

	// 1. Initialize app normally so tview handles Windows terminal setup properly
	app = tview.NewApplication()
	defer func() {
		if r := recover(); r != nil {
			app.Stop()                          // Restore the terminal
			fmt.Printf("YAMA Crashed: %v\n", r) // Print the actual error safely
		}
	}()

	// 2. Hook into the draw loop to access the hidden screen and set the cursor globally
	app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		s.SetCursorStyle(tcell.CursorStyleBlinkingBlock)
		return false // MUST return false so tview continues to draw the screen!
	})

	iwrMan := &morse.IWRManager{
		App:        app,
		IWREnabled: true,
	}
	morse.SetManager(iwrMan)

	// 1. Ensure directory and files exist
	checkIWRFiles()

	// 2. Load the IWR file directly (it is guaranteed to exist now)
	if _, err := iwrMan.LoadIWRFile(); err != nil {
		log.Printf("IWR Load Error: %v", err)
	}

	bgColor := tcell.GetColor(AppBackgroundColor)
	tview.Styles.PrimitiveBackgroundColor = bgColor
	tview.Styles.PrimaryTextColor = tcell.ColorWhite
	tview.Styles.ContrastBackgroundColor = tcell.ColorDarkGreen

	header = tview.NewTextView()
	header.SetDynamicColors(true).SetTextAlign(tview.AlignCenter)

	statusLine = tview.NewTextView()
	statusLine.SetDynamicColors(true)

	blueLine = tview.NewTextView()
	blueLine.SetDynamicColors(true)
	blueLine.SetBackgroundColor(tcell.ColorSteelBlue)

	inputArea = tview.NewTextArea()
	inputArea.SetBackgroundColor(bgColor)
	inputArea.SetBorder(true).SetTitle(" [white]Text Input ")
	inputArea.SetPlaceholder("Enter text, then Ctrl-P to Play or F1 for Help; ESC closes screens.")

	inputArea.SetChangedFunc(func() {
		// If the engine typed this, ignore it so the menu doesn't reset!
		if isProgrammaticUpdate {
			return
		}
		if currentState == StateStopped || currentState == StatePaused {
			currentState = StateIdle
		}
		refreshUI(currentState)
	})

	morse.OnWordChange = func(char string, index int) {
		if char == "" || index == -1 {
			return
		}
		char = colorTagRegex.ReplaceAllString(char, "")
		char = strings.TrimSpace(char)
		if char == "" {
			return
		}

		app.QueueUpdateDraw(func() {
			_, _, width, height := inputArea.GetInnerRect()
			if width > 0 && height > 0 {
				maxChars := width * (height - 1)
				if len(actualText)+len(char) > maxChars {
					actualText = ""
				}
			}

			if index == 0 && len(actualText) > 0 {
				actualText += " " + char
			} else {
				actualText += char
			}
			updateVisibility()
		})
	}

	morse.OnWordPlayed = func(word string, isIWR bool) {
		statsTotalWords++
		if isIWR {
			statsIWRWords++
			statsIWRMap[word]++
		}
	}

	morse.OnStatusUpdate = func(msg string) {
		app.QueueUpdateDraw(func() {
			if msg == "STOP" {
				stopAudio()
				statusLine.SetText(" [#00FF00]Playback Complete")
			} else {
				statusLine.SetText(msg)
			}
		})
	}

	morse.OnFatalError = EmergencyQuit

	pages = tview.NewPages()
	tview.Styles.PrimitiveBackgroundColor = bgColor
	refreshUI(StateIdle)

	mainFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 2, 0, false).
		AddItem(inputArea, 0, 1, true).
		AddItem(statusLine, 1, 0, false).
		AddItem(blueLine, 1, 0, false)

	pages.AddPage("main", mainFlex, true, true)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlC {
			return event
		}

		if event.Key() == tcell.KeyEsc {
			frontName, _ := pages.GetFrontPage()
			if frontName != "main" && frontName != "" {
				pages.RemovePage(frontName)
				newFrontName, newFrontPrim := pages.GetFrontPage()
				if newFrontName == "main" || newFrontPrim == nil {
					app.SetFocus(inputArea)
				} else {
					app.SetFocus(newFrontPrim)
				}
				return nil
			}

			if currentState == StatePlaying {
				stopAudio()
				return nil
			}
		}

		if _, isInput := app.GetFocus().(*tview.InputField); isInput {
			return event
		}

		if event.Key() == tcell.KeyRune && event.Rune() == ' ' {
			if currentState == StatePlaying {
				isBlocked = !isBlocked
				updateVisibility()
				return nil
			}
		}

		switch event.Key() {
		case tcell.KeyCtrlH, tcell.KeyBackspace, tcell.KeyBackspace2:
			// 1. Strict lock during Play. No backspacing allowed anywhere.
			if currentState == StatePlaying {
				return nil
			}
			// 2. Relaxed lock during Pause. Allow backspace inside modal menus.
			if currentState == StatePaused && app.GetFocus() == inputArea {
				return nil
			}
			return event
		case tcell.KeyCtrlB:
			// Locked during Paused state (requires full stop)
			if currentState != StatePlaying && currentState != StatePaused {
				showAbout()
			}
			return nil
		case tcell.KeyCtrlP:
			if currentState == StateIdle || currentState == StateStopped || currentState == StatePlaying {
				handlePlayPause(iwrMan)
			}
			return nil
		case tcell.KeyCtrlR:
			if currentState == StatePaused {
				handlePlayPause(iwrMan)
			}
			return nil
		case tcell.KeyCtrlS:
			if currentState == StatePlaying || currentState == StatePaused {
				stopAudio()
				return nil
			}
			return event
		case tcell.KeyCtrlF, tcell.KeyF3:
			if currentState != StatePlaying {
				showFile()
				clearStats()
			}
			return nil
		case tcell.KeyF1:
			if currentState != StatePlaying && currentState != StatePaused {
				showHelp()
			}
			return nil
		case tcell.KeyCtrlT:
			// Unlocked during Paused state!
			if currentState != StatePlaying {
				showToneSpeed()
			}
			return nil
		case tcell.KeyCtrlO:
			// Locked during Paused state (requires full stop)
			if currentState != StatePlaying && currentState != StatePaused {
				showOptions()
			}
			return nil
		case tcell.KeyCtrlA:
			if currentState != StatePlaying {
				showImpairments()
			}
			return nil
		case tcell.KeyCtrlD:
			if statsTotalWords > 0 && (currentState == StateIdle || currentState == StateStopped) {
				showStats()
			}
			return nil
		case tcell.KeyCtrlE, tcell.KeyCtrlL:
			if currentState != StatePlaying {
				actualText = ""
				inputArea.SetText("", false)
				clearStats()
				refreshUI(currentState)
				currentInputFile = ""
			}
			return nil
		case tcell.KeyCtrlW:
			if currentState != StatePlaying && len(inputArea.GetText()) > 0 {
				target := currentFileDir
				if target == "" {
					target, _ = os.Getwd()
				}
				showWaveModal(target)
			}
			return nil
		case tcell.KeyCtrlQ:
			app.Stop()
			return nil
		}

		// 1. Strict catch-all: If PLAYING, swallow absolutely all unhandled keys. No exceptions.
		if currentState == StatePlaying {
			return nil
		}

		// 2. Relaxed catch-all: If PAUSED, swallow keys ONLY if main text box is focused,
		// allowing users to type inside modals (like Tone Speed or File screens).
		if currentState == StatePaused && app.GetFocus() == inputArea {
			return nil
		}

		return event
	})

	updateBlueLine()
	app.SetRoot(pages, true)

	app.SetFocus(inputArea)

	if err := app.Run(); err != nil {
		log.Printf("Fatal UI Error: %v", err)
	}

	logFile.Close()
	if info, err := os.Stat(logPath); err == nil && info.Size() == 0 {
		os.Remove(logPath)
	}
}

// EmergencyQuit cleanly tears down the tview TUI and forces an immediate process exit.
// Use this for unrecoverable hardware deadlocks (e.g., Audio Watchdog).
func EmergencyQuit(errorMessage string) {
	// 1. Log it to yama.log so you have a record of the crash
	log.Println("FATAL ERROR:", errorMessage)

	// 2. Cleanup: If the log is still empty (e.g., the write failed or buffered), remove it
	logPath := morse.ResolvePath("yama.log")
	if info, err := os.Stat(logPath); err == nil && info.Size() == 0 {
		os.Remove(logPath)
	}

	if app != nil {
		// 3. Suspend strips away the TUI and restores the normal terminal state instantly
		app.Suspend(func() {
			fmt.Printf("\n[YAMA WATCHDOG] FATAL ERROR: %s\n", errorMessage)
			fmt.Println("The application had to be forcefully terminated to prevent a deadlock.")
			os.Exit(1) // 4. The nuclear option.
		})
	} else {
		// Fallback just in case the app crashes before the UI even finishes initializing
		fmt.Printf("\n[YAMA WATCHDOG] FATAL ERROR: %s\n", errorMessage)
		os.Exit(1)
	}
}

// getModifierWarning checks if any active options will cause the audio
// to differ from the visual text input, returning a UI warning tag.
func getModifierWarning() string {
	// If any of these are true, the text being played is modified or out of order
	isModified := config.User.RandomWords ||
		config.User.RandomOrder ||
		config.User.UseSkip ||
		config.User.WordBuilder ||
		config.User.Playprosigns

	if isModified {
		// Using tview's color tags to make it pop in yellow on the blue line
		return " [yellow][Input Modified][-]"
	}

	// If everything is strictly WYSIWYG, return an empty string
	return ""
}

// lockPlayTime safely finalizes the session time and adds it to the user's lifetime total.
func lockPlayTime() {
	// Only calculate if we haven't already, and ensure the engine actually started
	if finalPlayTime == 0 && !morse.StartTime.IsZero() {
		totalElapsed := time.Since(morse.StartTime)
		currentTotalPaused := morse.TotalPaused

		// If they hit stop WHILE paused, we need to account for that active pause time!
		if currentState == StatePaused {
			currentTotalPaused += time.Since(morse.PauseStart)
		}

		finalPlayTime = totalElapsed - currentTotalPaused

		// Gamification: Add to lifetime and save instantly!
		config.User.LifetimePlaySeconds += int(finalPlayTime.Seconds())
		config.SaveConfig()
	}
}
