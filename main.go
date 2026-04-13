package main

import (
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
	Version = "Version 1.0"
)

const AppBackgroundColor = "#1B2B44"

var (
	app        *tview.Application
	pages      *tview.Pages
	header     *tview.TextView
	statusLine *tview.TextView
	stats      *tview.TextView
	inputArea  *tview.TextArea
	blueLine   *tview.TextView
	mainFlex   *tview.Flex

	currentState   AppState = StateIdle
	actualText     string
	fullTextToPlay string

	// Session Stats Tracking
	statsTotalWords int
	statsIWRWords   int
	statsIWRList    []string
	statsIWRMap     = make(map[string]int) // real-time
	isBlocked       bool
	hasStats        bool
	finalPlayTime   time.Duration
	isFirstRun      = true

	colorTagRegex = regexp.MustCompile(`\[.*?\]`)
)

// checkIWRFiles acts as the traffic cop before the engine tries to load.
func checkIWRFiles() (targetPath string, isFirstRun bool) {
	localPath := "./yamaIWR.txt"

	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	fallbackPath := filepath.Join(configDir, "YAMA", "yamaIWR.txt")

	if _, err := os.Stat(localPath); err == nil {
		return localPath, false
	}

	if _, err := os.Stat(fallbackPath); err == nil {
		return fallbackPath, false
	}

	return fallbackPath, true
}

var contractions = map[string]string{
	"CAN'T": "CANNOT", "WON'T": "WILL NOT", "DON'T": "DO NOT",
	"I'M": "I AM", "I'VE": "I HAVE", "I'LL": "I WILL", "I'D": "I WOULD",
	"YOU'RE": "YOU ARE", "YOU'VE": "YOU HAVE", "YOU'LL": "YOU WILL", "YOU'D": "YOU WOULD",
	"HE'S": "HE IS", "SHE'S": "SHE IS", "IT'S": "IT IS",
	"AREN'T": "ARE NOT", "COULDN'T": "COULD NOT", "DIDN'T": "DID NOT",
}

func main() {

	config.LoadConfig() // config.go now handles all defaults natively!

	// Tell Go to write all logs cleanly to a file
	logFile, _ := os.OpenFile("yama.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	log.SetOutput(logFile)

	if err := morse.InitAudio(); err != nil {
		log.Fatal(err)
	}

	morse.RebuildMorseTable(config.User.UseExtendedPunctuation, config.User.UseSkip, config.User.SkipList)

	app = tview.NewApplication()

	// 1. Initialize the Manager
	iwrMan := &morse.IWRManager{
		App:        app,
		IWREnabled: true,
	}
	morse.SetManager(iwrMan) // Set it globally immediately

	// 2. The Traffic Cop: check if file exists
	_, isFirstRun := checkIWRFiles() // <-- Empty parentheses, and an underscore at the start!

	if !isFirstRun {
		// Only trigger the load if we proved it already exists
		_, err := iwrMan.LoadIWRFile()
		if err != nil {
			log.Printf("IWR Load Error: %v", err)
		}
	}

	bgColor := tcell.GetColor(AppBackgroundColor)
	tview.Styles.PrimitiveBackgroundColor = bgColor

	tview.Styles.PrimaryTextColor = tcell.ColorWhite
	tview.Styles.ContrastBackgroundColor = tcell.ColorDarkGreen // Makes active buttons turn green!

	// --- UI INITIALIZATION BLOCK ---
	header = tview.NewTextView()
	header.SetDynamicColors(true).SetTextAlign(tview.AlignCenter)

	stats = tview.NewTextView()

	statusLine = tview.NewTextView()
	statusLine.SetDynamicColors(true)

	blueLine = tview.NewTextView()
	blueLine.SetDynamicColors(true)
	blueLine.SetBackgroundColor(tcell.ColorSteelBlue)

	inputArea = tview.NewTextArea()
	inputArea.SetBackgroundColor(bgColor)
	inputArea.SetBorder(true).SetTitle(" [white]Text Input ")
	inputArea.SetPlaceholder("Enter text, Ctrl-P to Play or Ctrl-H for Help, ESC closes screens without SAVE.")

	inputArea.SetChangedFunc(func() {
		if currentState == StateStopped || currentState == StatePaused {
			currentState = StateIdle
		}
		refreshUI(currentState)
	})

	// --- RESTORED: THE ENGINE LISTENERS ---
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
			// === TYPEWRITER PAGE TURN LOGIC ===
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
	// --------------------------------------

	pages = tview.NewPages()
	tview.Styles.PrimitiveBackgroundColor = bgColor

	refreshUI(StateIdle)

	mainFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 2, 0, false).
		AddItem(inputArea, 0, 1, true).
		AddItem(statusLine, 1, 0, false).
		AddItem(blueLine, 1, 0, false)

	pages.AddPage("main", mainFlex, true, true)

	// 3. THE MASTER INPUT CAPTURE
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
		case tcell.KeyCtrlA:
			showAbout()
			return nil
		case tcell.KeyCtrlX:
			err := morse.ResetAudioDevice()

			if err != nil {
				// Red alert if it failed
				statusLine.SetText(" [red]Audio reset failed! Check device.")
				log.Printf("Ctrl-X Audio Reset Failed: %v", err)
			} else {
				// Friendly yellow confirmation if it worked
				statusLine.SetText(" [yellow]Audio connection reset triggered.")
			}

			// Clear the status message back to normal after 2 seconds
			go func() {
				time.Sleep(2 * time.Second)
				app.QueueUpdateDraw(func() {
					refreshUI(currentState)
				})
			}()
			return nil
		case tcell.KeyCtrlP, tcell.KeyCtrlR:
			handlePlayPause(iwrMan)
			return nil
		case tcell.KeyCtrlS:
			if currentState == StatePlaying || currentState == StatePaused {
				stopAudio()
				return nil
			}
			return event
		case tcell.KeyCtrlF:
			if currentState != StatePlaying {
				showFile()
				clearStats()
			}
			return nil
		case tcell.KeyCtrlT:
			showToneSpeed()
			return nil
		case tcell.KeyCtrlO:
			showOptions()
			return nil
		case tcell.KeyCtrlH:
			showHelp()
			return nil
		case tcell.KeyCtrlD:
			if statsTotalWords > 0 && (currentState == StateIdle || currentState == StateStopped) {
				showStats()
			}
			return nil
		case tcell.KeyCtrlE:
			if currentState != StatePlaying {
				actualText = ""
				inputArea.SetText("", false)
				clearStats()
				refreshUI(currentState)
			}
			return nil
		case tcell.KeyCtrlW:
			// Only allow Wave export if we aren't playing and actually have text
			if currentState != StatePlaying && len(inputArea.GetText()) > 0 {

				// Bypass the dir selector and use the text file's original directory
				target := currentFileDir
				if target == "" {
					target, _ = os.UserHomeDir() // Fallback if they typed text manually
				}

				showWaveModal(target) // <-- Jumps straight to your Wave form!
			}
			return nil
		case tcell.KeyCtrlQ:
			app.Stop()
			return nil
		}
		return event
	})

	updateBlueLine()

	// 4. Divert the startup flow AFTER layout is built
	app.SetRoot(pages, true)

	// --- THE FOOLPROOF STARTUP FLOW ---

	if isFirstRun {
		// Launch a background thread to wait until the UI is fully alive
		go func() {
			time.Sleep(100 * time.Millisecond) // Wait 1/10th of a second
			app.QueueUpdateDraw(func() {
				showIWRWelcomeModal() // POP THE MODAL WITH THE PATH!
			})
		}()
	} else {
		app.SetFocus(inputArea)
	}

	// Block and run the application
	if err := app.Run(); err != nil {
		log.Printf("Fatal UI Error: %v", err)
	}

	// Clean up empty log files on exit
	logFile.Close()
	if info, err := os.Stat("yama.log"); err == nil && info.Size() == 0 {
		os.Remove("yama.log")
	}
}

func ExpandContractions(input string) string {
	for contraction, expansion := range contractions {
		input = strings.ReplaceAll(input, contraction, expansion)
	}
	return input
}
