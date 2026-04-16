package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"yama/config"
	"yama/morse"

	"golang.org/x/term"
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

func checkIWRFiles() (targetPath string, isFirstRun bool) {
	localPath := "./yamaIWR.txt"

	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	fallbackPath := filepath.Join(configDir, "YAMA", "yamaIWR.txt")

	// 1. Check if the local file exists
	if _, err := os.Stat(localPath); err == nil {
		return localPath, false
	}

	// 2. Check if the fallback/AppData file exists
	if _, err := os.Stat(fallbackPath); err == nil {
		return fallbackPath, false
	}

	// 3. If neither exists, generate the default using your function!
	if createErr := createDefaultIWRFile(fallbackPath); createErr == nil {
		return fallbackPath, true // true = First Run
	}

	// Fallback return if everything fails
	return fallbackPath, true
}

func main() {
	ensureTerminal()

	config.LoadConfig() // config.go now handles all defaults natively!

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
	targetPath, firstRunCheck := checkIWRFiles()
	isFirstRun = firstRunCheck // Safely update the GLOBAL variable, no ':=' shadowing

	if isFirstRun {
		// FIX: Create the directory and the file immediately so it's not a "first run" next time!
		err := os.MkdirAll(filepath.Dir(targetPath), 0755)
		if err == nil {
			// Write a default empty file (or add some starter instructions/words)
			os.WriteFile(targetPath, []byte("# Add your PureCW IWR words here, one per line.\n"), 0644)
		} else {
			log.Printf("Failed to create IWR directory: %v", err)
		}
	} else {
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
		case tcell.KeyCtrlB:
			// used B so A can be for Audio
			showAbout()
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
			// Lock out the Options menu unless fully stopped/idle
			if currentState != StatePlaying && currentState != StatePaused {
				showOptions()
			}
			return nil
		case tcell.KeyCtrlA: 
			// A stolen for Audio 
			showImpairments()
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

func ensureTerminal() {
	// 1. Check if standard input is attached to a terminal
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return
	}

	// 2. Get the absolute path to the current executable
	exe, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}

	// 3. Handle non-terminal execution based on the OS
	switch runtime.GOOS {
	case "windows":
		// Pop a native Windows dialog: Yes = PowerShell, No = CMD, Cancel = Exit
		script := fmt.Sprintf(`
			Add-Type -AssemblyName PresentationFramework
			$res = [System.Windows.MessageBox]::Show("App requires a terminal. Restart in PowerShell (Yes), Command Prompt (No), or Exit (Cancel)?", "PureCW IWR", 'YesNoCancel', 'Question')
			if ($res -eq 'Yes') { Start-Process powershell.exe -ArgumentList "-NoExit -Command & '%s'" }
			if ($res -eq 'No') { Start-Process cmd.exe -ArgumentList '/k "%s"' }
		`, exe, exe)
		exec.Command("powershell", "-NoProfile", "-Command", script).Run()

	case "darwin":
		// Mac: Automatically pop open Terminal.app and run the executable
		script := fmt.Sprintf(`tell application "Terminal" to do script "%s"`, exe)
		exec.Command("osascript", "-e", script).Run()

	case "linux":
		fallthrough
	default:
		// Linux: Too many terminal choices. Try displaying a GUI warning.
		msg := "Please start this application manually from a terminal window."

		// Try zenity first (common on GNOME)
		if err := exec.Command("zenity", "--warning", "--text="+msg).Run(); err != nil {
			// Fallback to kdialog (common on KDE)
			exec.Command("kdialog", "--msgbox", msg).Run()
		}
	}

	// Exit the silent/GUI instance so the user doesn't have hanging background processes
	os.Exit(0)
}
