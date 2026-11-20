package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
	"yama/config"
	"yama/morse"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type AppState int

const (
	AppBackgroundColor = "#000000"
	playPauseDelay     = 200
	Ver                = "1.7"

	StateIdle AppState = iota
	StatePlaying
	StatePaused
	StateStopped
	StatePromptingTx
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

	lastFlashcardGroup []string
	echoActive         bool
	currentEchoView    tview.Primitive

	lastSkipTime time.Time
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
	exe := filepath.Base(os.Args[0])
	lower := strings.ToLower(exe)

	if !strings.HasPrefix(lower, "yama") {
		fmt.Println("App's name changed, it must start with \"yama\"", exe)
		os.Exit(3)
	}

	logPath := morse.ResolvePath("yama.log")
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0666)
	log.SetOutput(logFile)

	defer func() {
		logFile.Close()
		if info, err := os.Stat(logPath); err == nil && info.Size() == 0 {
			os.Remove(logPath)
		}
	}()

	config.LoadConfig()

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

			log.Fatalf("YAMA Crashed: %v\nStack Trace:\n%s\n", r, string(debug.Stack()))
		}
	}()

	app.EnableMouse(true)
	app.SetBeforeDrawFunc(func(s tcell.Screen) bool {
		s.SetStyle(tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite))
		s.Clear()
		s.SetCursorStyle(tcell.CursorStyleBlinkingBlock)
		return false
	})

	app.SetAfterDrawFunc(func(s tcell.Screen) {
		if currentState == StatePlaying {
			// Don't hide the cursor if a modal is open!
			frontName, _ := pages.GetFrontPage()
			if frontName == "main" || frontName == "" {
				s.HideCursor()
			}
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
	blueLine.SetRegions(true)

	inputArea = tview.NewTextArea()
	inputArea.SetBackgroundColor(tcell.ColorBlack)
	inputArea.SetBorder(true).SetTitle(" Text Input/Output ")
	inputArea.SetPlaceholder("Enter text (or Ctrl-F select a file), then Ctrl-P to Play;\nor use function key F1 for full Help.")

	inputArea.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// 1. SWALLOW CARRIAGE RETURNS FROM WINDOWS PASTES
		if event.Key() == tcell.KeyRune && event.Rune() == '\r' {
			return nil
		}

		// 2. MASSIVE FILE CRASH GUARDRAIL
		if event.Key() == tcell.KeyDelete || event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2 {
			if len(inputArea.GetText()) > 500 {
				return nil
			}
			return event
		}

		// 3. LET EVERYTHING ELSE PASS THROUGH NATIVELY
		return event
	})

	// Explicitly map the placeholder to Gray to stop PowerShell from guessing Green
	inputArea.SetPlaceholderStyle(tcell.StyleDefault.Foreground(tcell.ColorGray).Background(tcell.ColorBlack))

	inputArea.SetChangedFunc(onInputAreaChanged)

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
		updateBlueLine()
	}

	morse.ShowBlueLineTolerance = func() { updateBlueLine() }
	morse.OnEchoStart = func() { echoActive = true }
	morse.OnEchoEnd = func() { echoActive = false }

	morse.OnStatusUpdate = func(msg string) {
		app.QueueUpdateDraw(func() {

			if echoActive {
				if strings.HasPrefix(msg, "ECHO:") {
					statusLine.SetText(msg[5:])
				}
				app.SetFocus(inputArea)
				return
			}

			if msg == "STOP" {
				stopAudio()
				statusLine.SetText(" [#00FF00]Playback Complete")
			} else {
				statusLine.SetText(msg)
			}

			app.SetFocus(inputArea)
		})
	}

	morse.OnEchoReadyCursor = func(visible bool) {
		app.QueueUpdateDraw(func() {
			cursorTag := "\n\u2588"
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

		if echoStatsTextView != nil {
			app.QueueUpdateDraw(func() {
				currentProfile := morse.GetTiming(false, config.User)
				statsText := buildEchoStatsText(currentGroupStats, currentSessionStats, currentProfile)
				echoStatsTextView.SetText(statsText)
			})
		}
	}

	morse.OnGroupCompleted = func(words []string) {
		lastFlashcardGroup = append([]string{}, words...)
	}
	morse.OnFatalError = EmergencyQuit

	pages = tview.NewPages()
	pages.SetBackgroundColor(tcell.ColorBlack)
	refreshUI(StateIdle)

	mainFlex = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(header, 3, 0, false).
		AddItem(inputArea, 0, 1, true).
		AddItem(statusLine, 1, 0, false).
		AddItem(blueLine, 1, 0, false)

	mainFlex.SetBackgroundColor(tcell.ColorBlack)

	centeredAppLayout := tview.NewGrid().
		SetColumns(0, 120, 0).
		SetRows(0, 30, 0).
		AddItem(mainFlex, 1, 1, 1, 1, 0, 0, true)

	pages.AddPage("main", centeredAppLayout, true, true)

	launchOscillator := func(enableTx bool) {
		closeEchoStatsWindow()
		currentState = StatePlaying
		morse.IsStopping = false
		morse.IsManualOscModeActive = true

		if enableTx {
			header.SetText("[red] YAMA: LIVE TRANSMITTER[-]")
		} else {
			header.SetText("[red] YAMA: PRACTICE KEYING[-]")
		}
		updateBlueLine()

		go morse.RunManualOscillator(func(statusMsg, mainMsg string) {
			app.QueueUpdateDraw(func() {
				if statusMsg == "STOP" {
					morse.IsManualOscModeActive = false
					currentState = StateIdle

					inputArea.SetText("", false)
					statusLine.SetText(" [green]Ready.[-]")

					refreshUI(currentState)
					updateBlueLine()
				} else {
					if statusMsg != "" {
						statusLine.SetText(statusMsg)
					}
					if mainMsg != "" {
						inputArea.SetText(mainMsg, false)
					}
				}
			})
		}, enableTx)
	}

	showTransmitSafetyLock := func() {
		warningText := " WARNING: LIVE TRANSMITTER CONTROL \n\n" +
			"By enabling this mode, you are connectting your key to a transmitting radio.\n" +
			"\nYou are actng as the Transmitter Control Operator and\n" +
			"assume legal responsibility for station operation, \n" +
			"and licensing compliance.\n\n" +
			"DO NOT LEAVE YOUR KEY DEVICE UNATTENDED.\n\n" +
			"ESC disables the connection and exits.\n\n"

		inputForm := tview.NewForm()
		inputForm.SetBorder(true).
			SetTitle(" On-Air Confirmaton ").
			SetTitleColor(tcell.ColorRed).
			SetBorderColor(tcell.ColorRed)

		inputForm.AddTextView("Warning", warningText, 65, 11, true, false)

		inputField := tview.NewInputField().
			SetLabel("Type yes to enable: ").
			SetFieldWidth(10).
			SetAcceptanceFunc(func(textToCheck string, lastChar rune) bool {
				return len(textToCheck) <= 3
			})

		inputForm.AddFormItem(inputField)

		onSave := func() {
			if inputField.GetText() == "yes" {
				pages.RemovePage("TxWarning")

				inputArea.SetText(fmt.Sprintf("\n\n *** LIVE TRANSMITTER MODE ***\n\n WARNING: port (%s) is ON THE AIR!.\n\n DO NOT LEAVE KEY UNATTENDED. Press ESC to exit.", config.User.TransmitterPort), false)
				statusLine.SetText(" [red]LIVE TRANSMIT MODE ACTIVE")

				launchOscillator(true)
			} else {
				pages.RemovePage("TxWarning")
			}
		}

		inputForm.AddButton("Save", onSave)

		inputForm.AddButton("Cancel", func() {
			pages.RemovePage("TxWarning")
			app.SetFocus(inputArea)
		})

		modal := tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
				AddItem(nil, 0, 1, false).
				AddItem(inputForm, 22, 1, true).
				AddItem(nil, 0, 1, false), 70, 1, true).
			AddItem(nil, 0, 1, false)

		modal.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyCtrlS {
				onSave()
				return nil
			}
			if event.Key() == tcell.KeyCtrlC {
				pages.RemovePage("TxWarning")
				app.SetFocus(inputArea)
				return nil
			}
			if event.Key() == tcell.KeyEscape {
				pages.RemovePage("TxWarning")
				app.SetFocus(inputArea)
				return nil
			}
			return event
		})

		pages.AddPage("TxWarning", modal, true, true)
		app.SetFocus(inputForm)
	}

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {

		// ----------------------------------------------------
		// 1. LIVE SPEED ADJUSTMENTS (TOP-LEVEL PRIORITY)
		// ----------------------------------------------------
		if currentState == StatePlaying {
			key := event.Key()
			handled := false
			speedChanged := false

			// PAGE UP: Character Speed UP
			if key == tcell.KeyPgUp {
				handled = true
				if config.User.CharacterSpeed >= float64(config.MaxCharSpeed) {
					statusLine.SetText(" [yellow]Max Character Speed Reached[-]")
				} else {
					config.User.CharacterSpeed += 1.0
					if config.User.CharacterSpeed > float64(config.MaxCharSpeed) {
						config.User.CharacterSpeed = float64(config.MaxCharSpeed)
					}
					// Snowplow IWR up to maintain separation
					if config.User.IWREnabled && config.User.IWRSpeed <= config.User.CharacterSpeed {
						config.User.IWRSpeed = config.User.CharacterSpeed + 1.0
						if config.User.IWRSpeed > float64(config.MaxIWRSpeed) {
							config.User.IWRSpeed = float64(config.MaxIWRSpeed)
							config.User.CharacterSpeed = config.User.IWRSpeed - 1.0
						}
					}
					speedChanged = true
				}
				// PAGE DOWN: Character Speed DOWN
			} else if key == tcell.KeyPgDn {
				handled = true
				floor := float64(config.MinCharSpeed)
				if !config.User.UseStandard && config.User.EffectiveSpeed > floor {
					floor = config.User.EffectiveSpeed
				}

				if config.User.CharacterSpeed <= floor {
					if !config.User.UseStandard && config.User.CharacterSpeed <= config.User.EffectiveSpeed {
						statusLine.SetText(" [yellow]Cannot drop Character Speed below Effective Speed[-]")
					} else {
						statusLine.SetText(" [yellow]Min Character Speed Reached[-]")
					}
				} else {
					oldSpeed := config.User.CharacterSpeed
					config.User.CharacterSpeed -= 1.0
					if config.User.CharacterSpeed < floor {
						config.User.CharacterSpeed = floor
					}
					if config.User.EndSpeed <= oldSpeed {
						config.User.EndSpeed = config.User.CharacterSpeed
					}
					speedChanged = true
				}

			// HOME: IWR Speed UP
			} else if key == tcell.KeyHome {
				handled = true
				if !morse.IsManualOscModeActive {
					// ... existing IWR Speed UP logic ...
				}
			} else if key == tcell.KeyEnd {
				handled = true
				if !morse.IsManualOscModeActive {
					// ... existing IWR Speed DOWN logic ...
				}
			// END: IWR Speed DOWN
			} else if key == tcell.KeyEnd && config.User.IWREnabled {
				handled = true
				floor := config.User.CharacterSpeed + 1.0
				if floor < float64(config.MinIWRSpeed) {
					floor = float64(config.MinIWRSpeed)
				}

				if config.User.IWRSpeed <= floor {
					statusLine.SetText(" [yellow]IWR Speed cannot be closer to Character Speed[-]")
				} else {
					config.User.IWRSpeed -= 1.0
					if config.User.IWRSpeed < floor {
						config.User.IWRSpeed = floor
					}
					speedChanged = true
				}
			}

			if speedChanged {
				statusLine.SetText("")
				updateBlueLine()
			}
			if handled {
				return nil
			}
		}

		if currentState == StatePromptingTx {
			char := event.Rune()
			key := event.Key()

			if char == 'y' || char == 'Y' {
				statusLine.SetText(" [red]Transmitter Keying ENABLED.")
				launchOscillator(true)
				return nil
			} else if char == 'n' || char == 'N' || key == tcell.KeyEnter {
				statusLine.SetText(" [green]Local Practice Only.")
				launchOscillator(false)
				return nil
			} else if key == tcell.KeyEscape {
				currentState = StateIdle
				inputArea.SetText("", false)
				statusLine.SetText(" [green]Ready.[-]")
				updateBlueLine()
				return nil
			}
			return nil
		}

		if event.Key() == tcell.KeyEsc {
			frontName, _ := pages.GetFrontPage()

			if frontName == "iwredit" {
				return event
			}

			if frontName == "echoStats" {
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

		if morse.IsManualOscModeActive {
			return nil
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

		if event.Key() == tcell.KeyCtrlC {
			return nil
		}

		// ----------------------------------------------------
		// THE SHIELD: Let modals handle their own keys!
		// ----------------------------------------------------
		frontName, _ := pages.GetFrontPage()
		if frontName != "main" && frontName != "" {
			return event
		}

		switch event.Key() {
		case tcell.KeyCtrlH, tcell.KeyBackspace, tcell.KeyBackspace2:
			if currentState == StatePlaying {
				if morse.IsWaitingForUserKey() {
					morse.SignalUserKey('B')
					eraseLastGroupFromScreen()
				}
				return nil
			}
			if currentState == StatePaused && app.GetFocus() == inputArea {
				return nil
			}
			return event
		case tcell.KeyCtrlA:
			text := inputArea.GetText()
			hasText := len(strings.Fields(text)) > 0
			if currentState == StateIdle || currentState == StateStopped || !hasText {
				showAbout()
				return nil
			}
			if config.User.Echo {
				config.User.EchoAutoRetry = !config.User.EchoAutoRetry
				updateBlueLine()
				return nil
			}
		case tcell.KeyCtrlN:
			if (currentState == StateIdle || currentState == StateStopped) && len(inputArea.GetText()) > 0 {
				showNumWordsModal()
			}
			return nil
		case tcell.KeyCtrlP:
			if currentState == StateIdle || currentState == StateStopped || currentState == StatePlaying || currentState == StatePaused {
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
			if currentState == StateIdle || currentState == StateStopped {
				showFile(app, pages, inputArea)
				clearStats()
			}
			return nil
		case tcell.KeyF1:
			if currentState == StateIdle || currentState == StateStopped {
				showHelp()
			}
			return nil
		case tcell.KeyF4:
			if currentState == StateIdle || currentState == StateStopped {
				header.SetText(fmt.Sprintf(" YAMA - Yet Another Morse-Code App v%s\n ", Ver))
				inputArea.SetText("\n\n *** PRACTICE OSCILLATOR ***\n\n Initializing local practice...", false)
				statusLine.SetText(" [yellow]Starting Practice Oscillator...")
				launchOscillator(false)
			}
			return nil
		case tcell.KeyF5:
			if config.User.TransmitterPort == "" || strings.EqualFold(config.User.TransmitterPort, "none") {
				statusLine.SetText(" [red]No Transmitter Port configured in KeyDevice, Ctrl-K.[-]")
			} else if currentState == StateIdle || currentState == StateStopped {
				showTransmitSafetyLock()
			}
			return nil
		case tcell.KeyCtrlT:
			if currentState == StateIdle || currentState == StateStopped || currentState == StatePlaying {
				showTiming()
			}
			return nil
		case tcell.KeyCtrlO:
			if currentState == StateIdle || currentState == StateStopped {
				showOptions()
			}
			return nil
		case tcell.KeyCtrlE:
			if currentState == StateIdle || currentState == StateStopped {
				showKeyEchoOptions()
			}
			return nil
		case tcell.KeyCtrlK:
			if currentState == StateIdle || currentState == StateStopped {
				showKeyDeviceOptions()
			}
			return nil
		case tcell.KeyCtrlD:
			if config.User.Echo {
				if currentEchoView != nil {
					mainFlex.RemoveItem(currentEchoView)
					currentEchoView = nil
					app.SetFocus(inputArea)
					return nil
				}
				statsView := showStatsEcho()
				currentEchoView = statsView
				mainFlex.AddItem(currentEchoView, 18, 1, true)
				app.SetFocus(statsView)
			} else {
				if config.StatsTotalWords > 0 {
					if currentState == StateIdle || currentState == StateStopped {
						if pages.HasPage("stats") {
							pages.RemovePage("stats")
							app.SetFocus(inputArea)
						} else {
							showStats()
						}
					}
				}
			}
			return nil
		case tcell.KeyCtrlI:
			showImpairments()
			return nil
		case tcell.KeyCtrlL:
			if currentState == StateIdle || currentState == StateStopped {
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
			if (currentState == StateIdle || currentState == StateStopped) && len(inputArea.GetText()) > 0 {
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
			// ----------------------------------------------------
			// STANDARD PLAYBACK CONTROLS
			// ----------------------------------------------------
			if event.Key() == tcell.KeyEnter {
				if morse.IsWaitingForUserKey() {
					morse.SignalUserKey('E')
					return nil
				}
				if config.User.Echo {
					if time.Since(lastSkipTime) > 500*time.Millisecond {
						lastSkipTime = time.Now()
						morse.ForceEchoFinish = true
						morse.AutoNextAction = 'E'
					}
					return nil
				}
			}

			if event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2 {
				if morse.IsWaitingForUserKey() {
					morse.SignalUserKey('B')
					return nil
				}
				if config.User.Echo {
					if time.Since(lastSkipTime) > 500*time.Millisecond {
						lastSkipTime = time.Now()
						morse.ForceEchoRetry = true
						morse.AutoNextAction = 'B'
					}
					return nil
				}
			}

			if event.Key() == tcell.KeyCtrlR {
				if config.User.Echo {
					if time.Since(lastSkipTime) > 500*time.Millisecond {
						lastSkipTime = time.Now()
						morse.ForceEchoRest = true
					}
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
}

func EmergencyQuit(errorMessage string) {
	if app != nil {
		app.Stop()
	}

	log.Fatalf("\n[YAMA WATCHDOG] FATAL ERROR: %s\n", errorMessage)
}

func getModifierWarning() string {
	if config.User.Echo {
		return ""
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

func eraseLastGroupFromScreen() {
	if len(lastFlashcardGroup) == 0 {
		return
	}

	words := strings.Fields(actualText)

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
