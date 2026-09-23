package main

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"yama/config"
	"yama/morse"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type AppState int

const (
	AppBackgroundColor = "#000000" // black
	playPauseDelay     = 200
	Ver                = "1.4"

	StateIdle AppState = iota
	StatePlaying
	StatePaused
	StateStopped
)

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

	statsIWRWords int
	statsIWRList  []string
	statsIWRMap   = make(map[string]int)
	isBlocked     bool
	finalPlayTime time.Duration

	colorTagRegex = regexp.MustCompile(`\[.*?\]`)

	isProgrammaticUpdate bool
	lastFlashcardGroup   []string
	echoActive           bool
	currentEchoView      tview.Primitive
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
	// handle cmdline
	config.LoadConfig()

	logPath := morse.ResolvePath("yama.log")
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0666)
	log.SetOutput(logFile)

	if err := morse.InitAudio(); err != nil {
		log.Fatal(err)
	}

	// CLEAR OPTIONS
	if len(os.Args[1:]) == 1 && os.Args[1] == "SetDefaultOptions" {
		path := config.GetConfigPath()
		if _, err := os.Stat(path); err == nil {
			os.Remove(path)
			config.LoadConfig()
		} else {
			log.Printf("Failed to remove options file to get Default Options <%s>: %v\n", path, err)
		}
	}

	morse.RebuildMorseTable(config.User.UseExtendedPunctuation, config.User.UseEuropeanChars, config.User.UseSkip, config.User.SkipList, config.User.EuropeanSkipList)

	// ======================
	// EXPLICIT TCELL COLORS
	// =====================
	tview.Styles.PrimitiveBackgroundColor = tcell.ColorBlack
	tview.Styles.ContrastBackgroundColor = tcell.ColorNavy
	tview.Styles.BorderColor = tcell.ColorGray
	tview.Styles.GraphicsColor = tcell.ColorGray
	tview.Styles.PrimaryTextColor = tcell.ColorWhite

	app = tview.NewApplication()
	defer func() {
		if r := recover(); r != nil {
			if app != nil {
				app.Stop()
			}
			log.Fatalf("YAMA Crashed: %v\n", r)
		}
	}()

	app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		s.SetStyle(tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite))
		s.Clear()
		s.SetCursorStyle(tcell.CursorStyleBlinkingBlock)
		return false
	})

	app.SetAfterDrawFunc(func(s tcell.Screen) {
		if currentState == StatePlaying {
			s.HideCursor()
		}
	})

	iwrMan := &morse.IWRManager{
		App:        app,
		IWREnabled: true,
	}
	morse.SetManager(iwrMan)

	checkIWRFiles()

	if _, err := iwrMan.LoadIWRFile(); err != nil {
		log.Printf("IWR Load Error: %v", err)
	}

	tview.Borders.HorizontalFocus = tview.BoxDrawingsLightHorizontal
	tview.Borders.VerticalFocus = tview.BoxDrawingsLightVertical
	tview.Borders.TopLeftFocus = tview.BoxDrawingsLightDownAndRight
	tview.Borders.TopRightFocus = tview.BoxDrawingsLightDownAndLeft
	tview.Borders.BottomLeftFocus = tview.BoxDrawingsLightUpAndRight
	tview.Borders.BottomRightFocus = tview.BoxDrawingsLightUpAndLeft

	header = tview.NewTextView()
	header.SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	header.SetBackgroundColor(tcell.ColorBlack)

	statusLine = tview.NewTextView()
	statusLine.SetDynamicColors(true)
	statusLine.SetBackgroundColor(tcell.ColorBlack)

	blueLine = tview.NewTextView()
	blueLine.SetDynamicColors(true)
	blueLine.SetBackgroundColor(tcell.ColorSteelBlue)

	inputArea = tview.NewTextArea()
	inputArea.SetBackgroundColor(tcell.ColorBlack)
	inputArea.SetBorder(true).SetTitle(" Text Input/Output ")
	inputArea.SetPlaceholder("Enter text (or Ctrl-F select a file), then Ctrl-P to Play;\nor use function key F1 for full Help.")

	inputArea.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyDelete || event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2 {
			txt := inputArea.GetText()
			_, col, _, _ := inputArea.GetCursor()

			if event.Key() == tcell.KeyDelete && col < len(txt) {
				newTxt := txt[:col] + txt[col+1:]
				inputArea.SetText(newTxt, false)
			}

			// Wait for deletion to process, then refresh
			go func() {
				time.Sleep(20 * time.Millisecond)
				app.QueueUpdateDraw(func() {
					refreshUI(currentState)
				})
			}()

			if event.Key() == tcell.KeyDelete {
				return nil
			}
			return event
		}

		if event.Key() == tcell.KeyRune {
			r := event.Rune()

			// compress space
			if r == ' ' {
				current := inputArea.GetText()
				if len(current) > 0 && current[len(current)-1] == ' ' {
					return nil
				}
			}

			// DEFER THE REFRESH: Let the widget process the key first!
			go func() {
				time.Sleep(20 * time.Millisecond)
				app.QueueUpdateDraw(func() {
					refreshUI(currentState)
				})
			}()

			upper := unicode.ToUpper(r)
			if upper != r {
				return tcell.NewEventKey(tcell.KeyRune, upper, event.Modifiers())
			}
		}

		return event
	})

	// Explicitly map the placeholder to Gray to stop PowerShell from guessing Green
	inputArea.SetPlaceholderStyle(tcell.StyleDefault.Foreground(tcell.ColorGray).Background(tcell.ColorBlack))

	inputArea.SetChangedFunc(func() {
		// do nothing
	})

	morse.OnClearFlashcardScreen = clearFlashcardScreen

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
		config.StatsTotalWords++
		if isIWR {
			statsIWRWords++
			statsIWRMap[word]++
		}
		updateBlueLine() // for word cnt
	}

	morse.ShowBlueLineTolerance = func() { updateBlueLine() }
	morse.OnEchoStart = func() { echoActive = true }
	morse.OnEchoEnd = func() { echoActive = false }

	morse.OnStatusUpdate = func(msg string) {
		// ONE queue to lock the main UI thread for all the updates inside
		app.QueueUpdateDraw(func() {

			if echoActive {
				if strings.HasPrefix(msg, "ECHO:") {
					statusLine.SetText(msg[5:])
				}
				app.SetFocus(inputArea)
				return // Exits early so the rest of the block doesn't run
			}

			if msg == "STOP" {
				stopAudio()
				statusLine.SetText(" [#00FF00]Playback Complete")
			} else {
				statusLine.SetText(msg)
			}

			// We are still safely inside the first QueueUpdateDraw,
			// so we can just set the focus directly here at the end.
			app.SetFocus(inputArea)
		})
	}

	morse.OnEchoReadyCursor = func(visible bool) {
		app.QueueUpdateDraw(func() {
			// Drop to the next line with a plain block. No color tags allowed in InputFields!
			cursorTag := "\n█"

			currentText := inputArea.GetText()

			if visible {
				if !strings.HasSuffix(currentText, cursorTag) {
					inputArea.SetText(currentText+cursorTag, false)
				}
			} else {
				if strings.HasSuffix(currentText, cursorTag) {
					newText := strings.TrimSuffix(currentText, cursorTag)
					inputArea.SetText(newText, false)
				}
			}
		})
	}

	morse.OnEchoCharDecoded = func(char string) {
		if config.User.VisualFeedback {
			app.QueueUpdateDraw(func() {
				// Grab the current text from your 2nd line TextView, append the char, and set it back.
				text := inputArea.GetText()
				lines := strings.Split(text, "\n")

				for len(lines) < 2 {
					lines = append(lines, "")
				}

				if len(lines) >= 2 {
					lines[1] += char
					inputArea.SetText(strings.Join(lines, "\n"), true)
				}

			})
		}
	}

	morse.OnEchoStatsUpdated = func(group morse.EchoStats, session morse.EchoStats) {
		currentGroupStats = group
		currentSessionStats = session

		// We only check and update the SINGLE wide text view now
		if echoStatsTextView != nil {
			app.QueueUpdateDraw(func() {
				// 1. Get the exact timing targets dynamically
				currentProfile := morse.GetTiming(false, config.User)

				// 2. Build the text (passing the profile in so the math works!)
				statsText := buildEchoStatsText(currentGroupStats, currentSessionStats, currentProfile)

				// 3. Update your single tview text view
				echoStatsTextView.SetText(statsText)
			})
		}
	}

	morse.OnGroupCompleted = func(words []string) {
		// Store the exact logical words of the last group
		lastFlashcardGroup = append([]string{}, words...)
	}
	morse.OnFatalError = EmergencyQuit

	// Inside your main setup, where you handle the UI callbacks:
	pages = tview.NewPages()
	pages.SetBackgroundColor(tcell.ColorBlack)
	refreshUI(StateIdle)

	mainFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 3, 0, false).
		AddItem(inputArea, 0, 1, true).
		AddItem(statusLine, 1, 0, false).
		AddItem(blueLine, 1, 0, false)

	mainFlex.SetBackgroundColor(tcell.ColorBlack)

	pages.AddPage("main", mainFlex, true, true)

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {

		if event.Key() == tcell.KeyCtrlC {
			return event
		}

		if event.Key() == tcell.KeyEsc {
			frontName, _ := pages.GetFrontPage()

			if frontName == "iwredit" {
				return event
			}

			// ⚡ If your echo stats window is front-most or open, close it cleanly
			if frontName == "echoStats" || frontName == "stats" {
				closeEchoStatsWindow()
				app.SetFocus(inputArea)
				return nil
			}

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
			if currentState == StatePlaying {
				if currentState == StatePlaying {
					if morse.IsWaitingForUserKey() {
						morse.SignalUserKey('B')
						eraseLastGroupFromScreen()
					}
				}
				return nil
			}
			if currentState == StatePaused && app.GetFocus() == inputArea {
				return nil
			}
			return event
		case tcell.KeyCtrlB:
			text := inputArea.GetText()
			hasText := len(strings.Fields(text)) > 0
			if currentState != StatePlaying && currentState != StatePaused && !hasText {
				showAbout()
			}
			return nil
		case tcell.KeyCtrlN:
			if currentState != StatePlaying && len(inputArea.GetText()) > 0 {
				showNumWordsModal()
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
				showFile(app, pages, inputArea)
				clearStats()
			}
			return nil
		case tcell.KeyF1:
			if currentState != StatePlaying && currentState != StatePaused {
				showHelp()
			}
			return nil
		case tcell.KeyCtrlT:
			if currentState != StatePlaying {
				showToneSpeed()
			}
			return nil
		case tcell.KeyCtrlO:
			if currentState != StatePlaying && currentState != StatePaused {
				showOptions()
			}
			return nil
		case tcell.KeyCtrlK:
			if currentState != StatePlaying && currentState != StatePaused {
				showKeyEchoOptions()
			}
			return nil
		case tcell.KeyCtrlA:
			if config.User.Echo {
				config.User.EchoAutoRetry = !config.User.EchoAutoRetry
				updateBlueLine()
				return nil
			}
			showImpairments()
			return nil
		case tcell.KeyCtrlD:
			if config.User.Echo {
				if currentEchoView != nil {
					// ECHO TOGGLE: It's already open, so destroy it!
					mainFlex.RemoveItem(currentEchoView)
					currentEchoView = nil
					app.SetFocus(inputArea)
					return nil
				}

				// It's closed, so open it!
				currentEchoView = showStatsEcho()
				mainFlex.AddItem(currentEchoView, 18, 1, true)
				app.SetFocus(currentEchoView)
			} else {
				if config.StatsTotalWords > 0 {
					if currentState == StateIdle || currentState == StateStopped {
						if pages.HasPage("stats") {
							// STANDARD TOGGLE: It's open, so destroy it!
							pages.RemovePage("stats")
							app.SetFocus(inputArea)
						} else {
							// It's closed, so open it!
							showStats()
						}
					}
				}
			}
			return nil
		case tcell.KeyCtrlE, tcell.KeyCtrlL:
			if currentState != StatePlaying {
				actualText = ""
				inputArea.SetText("", false)
				clearStats()
				isResized = false
				preResizeSnapshot = ""
				refreshUI(currentState)
				currentInputFile = ""
			}
			return nil
		case tcell.KeyCtrlM:
			config.User.Mute = !config.User.Mute
			updateBlueLine()
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
			if app != nil {
				app.Stop()
			}
			return nil
		}

		if currentState == StatePlaying {
			frontPageName, _ := pages.GetFrontPage()

			if frontPageName == "impairments" {
				return event
			}

			if event.Key() == tcell.KeyEnter {

				if morse.IsWaitingForUserKey() {
					morse.SignalUserKey('E')
					return nil
				}
				if config.User.Echo {
					morse.ForceEchoFinish = true
					morse.AutoNextAction = 'E'
					return nil
				}
			}

			if event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2 {

				if morse.IsWaitingForUserKey() {
					morse.SignalUserKey('B')
					return nil
				}
				if config.User.Echo {
					morse.ForceEchoRetry = true
					return nil
				}
			}

			return nil
		}

		if currentState == StatePaused && app.GetFocus() == inputArea {
			return nil
		}

		return event
	})

	updateBlueLine()
	app.SetRoot(pages, true)

	app.SetFocus(inputArea)

	if err := app.Run(); err != nil {
		if app != nil {
			app.Stop()
		}
		log.Fatalf("Fatal UI Error: %v", err)
	}

	logFile.Close()
	if info, err := os.Stat(logPath); err == nil && info.Size() == 0 {
		os.Remove(logPath)
	}
}

func EmergencyQuit(errorMessage string) {
	if app != nil {
		app.Stop()
	}

	log.Fatalf("\n[YAMA WATCHDOG] FATAL ERROR: %s\n", errorMessage)
}

func getModifierWarning() string {
	if config.User.Echo {
		return "" // no need, need space for other echo statis
	}

	isModified := config.User.RandomizeWords ||
		config.User.WordOrder ||
		config.User.UseSkip ||
		config.User.WordBuilder ||
		config.User.Playprosigns

	if isModified {
		return " [yellow][Input Modified][-]"
	}

	return ""
}

func lockPlayTime() {
	if finalPlayTime == 0 && !morse.StartTime.IsZero() {
		totalElapsed := time.Since(morse.StartTime)
		currentTotalPaused := morse.TotalPaused

		if currentState == StatePaused {
			currentTotalPaused += time.Since(morse.PauseStart)
		}

		finalPlayTime = totalElapsed - currentTotalPaused

		config.User.LifetimePlaySeconds += int(finalPlayTime.Seconds())
		config.SaveConfig()
	}
}

// used by Flashcard only
func eraseLastGroupFromScreen() {
	if len(lastFlashcardGroup) == 0 {
		return
	}

	// Use the same buffer the user actually sees
	words := strings.Fields(actualText)

	// Remove EXACT logical words from the end
	for i := len(lastFlashcardGroup) - 1; i >= 0; i-- {
		if len(words) == 0 {
			break
		}
		if words[len(words)-1] == lastFlashcardGroup[i] {
			words = words[:len(words)-1]
		}
	}

	actualText = strings.Join(words, " ")
	inputArea.SetText(actualText, false)
}

func clearFlashcardScreen() {
	actualText = ""
	inputArea.SetText("", false)
}
