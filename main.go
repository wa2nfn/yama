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

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/term"
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
	inputArea  *tview.TextArea
	blueLine   *tview.TextView
	mainFlex   *tview.Flex

	currentState   AppState = StateIdle
	actualText     string
	fullTextToPlay string

	statsTotalWords int
	statsIWRWords   int
	statsIWRList    []string
	statsIWRMap     = make(map[string]int)
	isBlocked       bool
	finalPlayTime   time.Duration
	isFirstRun      = true

	colorTagRegex = regexp.MustCompile(`\[.*?\]`)

	// Flag to prevent the text box from resetting the menu when the engine updates it
	isProgrammaticUpdate bool
)

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

func checkIWRFiles() (targetPath string, isFirstRun bool) {
	localPath := ResolvePath("./yamaIWR.txt")

	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	fallbackPath := ResolvePath(filepath.Join(configDir, "YAMA", "yamaIWR.txt"))

	if _, err := os.Stat(localPath); err == nil {
		return localPath, false
	}

	if _, err := os.Stat(fallbackPath); err == nil {
		return fallbackPath, false
	}

	if createErr := createDefaultIWRFile(fallbackPath); createErr == nil {
		return fallbackPath, true
	}

	return fallbackPath, true
}

func main() {
	ensureTerminal()

	config.LoadConfig()

	logPath := ResolvePath("yama.log")
	logFile, _ := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	log.SetOutput(logFile)

	if err := morse.InitAudio(); err != nil {
		log.Fatal(err)
	}

	morse.RebuildMorseTable(config.User.UseExtendedPunctuation, config.User.UseSkip, config.User.SkipList)

	app = tview.NewApplication()

	iwrMan := &morse.IWRManager{
		App:        app,
		IWREnabled: true,
	}
	morse.SetManager(iwrMan)

	targetPath, firstRunCheck := checkIWRFiles()
	isFirstRun = firstRunCheck

	if isFirstRun {
		err := os.MkdirAll(filepath.Dir(targetPath), 0755)
		if err == nil {
			os.WriteFile(targetPath, []byte("# Add your PureCW IWR words here, one per line.\n"), 0644)
		} else {
			log.Printf("Failed to create IWR directory: %v", err)
		}
	} else {
		_, err := iwrMan.LoadIWRFile()
		if err != nil {
			log.Printf("IWR Load Error: %v", err)
		}
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

	// --- THIS INITIALIZATION WAS MISSING ---
	inputArea = tview.NewTextArea()
	inputArea.SetBackgroundColor(bgColor)
	inputArea.SetBorder(true).SetTitle(" [white]Text Input ")
	inputArea.SetPlaceholder("Enter text, Ctrl-P to Play or Ctrl-H for Help, ESC closes screens without SAVE.")

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
		case tcell.KeyCtrlB:
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
			// Unlocked during Paused state!
			if currentState != StatePlaying {
				showImpairments()
			}
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
		return event
	})

	updateBlueLine()
	app.SetRoot(pages, true)

	if isFirstRun {
		go func() {
			time.Sleep(100 * time.Millisecond)
			app.QueueUpdateDraw(func() {
				showIWRWelcomeModal()
			})
		}()
	} else {
		app.SetFocus(inputArea)
	}

	if err := app.Run(); err != nil {
		log.Printf("Fatal UI Error: %v", err)
	}

	logFile.Close()
	if info, err := os.Stat(logPath); err == nil && info.Size() == 0 {
		os.Remove(logPath)
	}
}

func ensureTerminal() {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	switch runtime.GOOS {
	case "windows":
		script := fmt.Sprintf(`
			Add-Type -AssemblyName PresentationFramework
			$res = [System.Windows.MessageBox]::Show("App requires a terminal. Restart in PowerShell (Yes), Command Prompt (No), or Exit (Cancel)?", "PureCW IWR", 'YesNoCancel', 'Question')
			if ($res -eq 'Yes') { Start-Process powershell.exe -ArgumentList "-NoExit -Command & '%s'" }
			if ($res -eq 'No') { Start-Process cmd.exe -ArgumentList '/k "%s"' }
		`, exe, exe)
		exec.Command("powershell", "-NoProfile", "-Command", script).Run()
	case "darwin":
		script := fmt.Sprintf(`tell application "Terminal" to do script "%s"`, exe)
		exec.Command("osascript", "-e", script).Run()
	case "linux":
		fallthrough
	default:
		msg := "Please start this application manually from a terminal window."
		if err := exec.Command("zenity", "--warning", "--text="+msg).Run(); err != nil {
			exec.Command("kdialog", "--msgbox", msg).Run()
		}
	}
	os.Exit(0)
}


