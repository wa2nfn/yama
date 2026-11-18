package main

import (
	"fmt"
	"math"
	"slices"
	"cmp"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"yama/config"
	"yama/morse"
	"yama/parser"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// MaxExportWords defines the hard limit for WAV exports to prevent massive file generation
const MaxExportWords = 5000

// --- Logic Helpers ---
var currentInputFile string
var currentFileDir string
var playPauseMu sync.Mutex
var isCountingDown bool
var currentGroupStats morse.EchoStats

// Memory for the Ctrl-N Text Resizer Revert feature
// var preResizeSnapshot string
var isResized bool

func handlePlayPause(iwrMan *morse.IWRManager) {
	playPauseMu.Lock()
	defer playPauseMu.Unlock()

	// THE SHEILD: block Pause during Echo playback
	if currentState == StatePlaying && config.User.Echo {
		statusLine.SetText(" [yellow]Pause disabled in Echo Mode. Use Mute (Ctrl-M) or Stop (Ctrl-S).[-]")
		return
	}
	// 0. THE SHIELD: Completely ignore Ctrl-P if the clock is ticking
	if isCountingDown {
		return
	}

	// 1. DEBOUNCE
	if time.Since(lastPlayPause) < playPauseDelay*time.Millisecond {
		return
	}
	lastPlayPause = time.Now()

	// 2. SAFETY LOCK
	if morse.AudioHardwareDead {
		statusLine.SetText(" [#FFFF55::b]FATAL: Audio hardware lost. Restart app.[::-]")
		app.Stop()
		log.Fatalf("FATAL: Audio hardware lost. Restart app.")
		return
	}

	if currentState == StateIdle || currentState == StateStopped {
		currentText := strings.TrimSpace(inputArea.GetText())

		if len(currentText) == 0 {
			statusLine.SetText(" [yellow]Nothing to play. Please enter some text...[-]")
			return
		}

		// Activate the shield if we have a delay.
		if config.User.StartDelay > 0 {
			isCountingDown = true
		} else {
			currentState = StatePlaying
		}

		startAudioSequence(iwrMan)
		clearStats()

	} else if currentState == StatePlaying {
		currentState = StatePaused
		morse.IsPaused = true
		morse.PauseStart = time.Now()
		refreshUI(currentState)

	} else if currentState == StatePaused {
		currentState = StatePlaying
		morse.IsPaused = false
		morse.TotalPaused += time.Since(morse.PauseStart)
		refreshUI(currentState)
	}
}

func clearStats() {
	config.StatsTotalWords = 0
	statsIWRWords = 0
	statsIWRMap = make(map[string]int)
	statsIWRList = []string{}
	morse.TotalPaused = 0
	finalPlayTime = 0

	morse.SessionStats = morse.EchoStats{}
	currentGroupStats = morse.EchoStats{}
}

func runEngine(parsedText string, iwrMan *morse.IWRManager) {
	defer func() {
		if r := recover(); r != nil {
			app.Stop()
			log.Fatalf("Panic Error: %v", r)
			app.QueueUpdateDraw(func() {
				isBlocked = false
				currentState = StateStopped
				statusLine.SetText(" [#FFFF55::b]Engine Crashed![::-]")
				refreshUI(StateStopped)
			})
		}
	}()

	morse.RunIWR(parsedText, iwrMan)

	app.QueueUpdateDraw(func() {
		isBlocked = false
		if !morse.IsStopping {
			lockPlayTime()

			currentState = StateIdle
			fullTextToPlay = ""
			refreshUI(StateIdle)
		} else {
			currentState = StateStopped
			refreshUI(StateStopped)
		}
	})
}

func stopAudio() {
	morse.IsStopping = true
	morse.IsPaused = false

	// THE USER HIT STOP: Lock the time immediately!
	lockPlayTime()

	isBlocked = false
	updateVisibility()

	currentState = StateStopped

	// Ensure a manual stop doesn't overwrite a hardware death warning
	if morse.AudioHardwareDead {
		statusLine.SetText(" [#FFFF55::b]ERROR: Audio Device Disconnected! Restart App.[::-]")
	} else {
		statusLine.SetText(" [#FFFF55]Stopped")
	}

	refreshUI(StateStopped)
}

// Safely converts text to asterisks but preserves all layout spacing
func maskText(text string) string {
	var sb strings.Builder
	sb.Grow(len(text))
	for _, ch := range text {
		// Keep structural characters intact
		if ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t' {
			sb.WriteRune(ch)
		} else {
			sb.WriteRune('*')
		}
	}
	return sb.String()
}

func updateVisibility() {
	if isBlocked {
		inputArea.SetText(maskText(actualText), false)
	} else {
		inputArea.SetText(actualText, false)
	}

	updateBlueLine()
}

func updateBlueLine() {
	if morse.IsManualOscModeActive {
		// Only show the Character Speed and nothing else
		speedOnlyText := fmt.Sprintf("[black] Speed: %g wpm ", config.User.CharacterSpeed)
		blueLine.SetText(speedOnlyText)
		return // Exit early!
	}

	iwrStatus := "N"
	if config.User.IWREnabled {
		iwrStatus = "Y"
	}
	retryStatus := "N"
	if config.User.EchoAutoRetry {
		retryStatus = "Y"
	}
	muteStatus := "N"
	if config.User.Mute {
		muteStatus = "Y"
	}

	var info string
	if config.User.UseStandard {
		info = fmt.Sprintf("[black] Speed: %g wpm ", config.User.CharacterSpeed)
	} else {
		mode := "Farnsworth"

		if config.User.UseWordsworth {
			mode = "Wordsworth"
		}

		info = fmt.Sprintf(" [black] %s Speed: %g/%g wpm ", mode, config.User.CharacterSpeed, config.User.EffectiveSpeed)
	}

	// adjust for YAMA voice
	info += fmt.Sprintf("| Mute: %s ", muteStatus)

	// adjust for IWR
	if config.User.IWREnabled && !config.User.Echo {
		info += fmt.Sprintf("| IWR: %s (%g wpm) ", iwrStatus, config.User.IWRSpeed)
	}

	// adjust for auto retry
	if config.User.Echo {
		paddingName := []string{"Strict", "Normal", "Relaxed", "Generous"}
		info += fmt.Sprintf("| Auto Retry: %s | Tolerance: %d%% | Key Padding: %s ", retryStatus, config.User.EchoTolerance, paddingName[config.User.KeyTimePadding])
	}

	wordCount := len(strings.Fields(actualText))
	info += fmt.Sprintf("| Word Cnt: %d ", wordCount)

	blueLine.SetText(info + getModifierWarning())
}

var hadText = false

func onInputAreaChanged() {
	// THE SHIELD: Prevent paused/playing states from dropping to Idle
	if currentState == StatePlaying || currentState == StatePaused {
		return
	}

	stateChanged := false
	if currentState == StateStopped {
		currentState = StateIdle
		stateChanged = true
	}

	// Menu visibility logic
	currentText := inputArea.GetText()
	hasText := len(strings.TrimSpace(currentText)) > 0

	// Only refresh if we transitioned from Stopped->Idle OR Empty->Full
	if hasText != hadText || stateChanged { // Use hadText here
		hadText = hasText                     // And update it here
		refreshUI(currentState)
	}
}

func refreshUI(state AppState) {
	var menu string
	hasText := len(inputArea.GetText()) > 0

	updateBlueLine()

	switch state {
	case StateIdle, StateStopped:
		status := "Ready"
		if state == StateStopped {
			status = "Stopped"
		}

		if morse.AudioHardwareDead {
			statusLine.SetText(" [#FFFF55::b]ERROR: Audio Device Disconnected! Restart App.[::-]")
		} else {
			statusLine.SetText(" [#FFFF55]" + status)
		}

		if config.User.Echo {
			menu = "[#FFFF55]F[white]ile  [#FFFF55]P[white]lay  [#FFFF55]T[white]iming  [#FFFF55]O[white]ption  [#FFFF55]E[white]cho  [#FFFF55]K[while]eyDevice  [#FFFF55]F1[white]help  [#FFFF55]A[white]bout  [#FFFF55]Q[white]uit "

		} else {
			menu = "[#FFFF55]F[white]ile  [#FFFF55]P[white]lay  [#FFFF55]T[white]iming  [#FFFF55]I[white]mpairments  [#FFFF55]O[white]ption  [#FFFF55]E[white]cho  [#FFFF55]K[white]eyDevice  [#FFFF55]F1[white]help  [#FFFF55]A[white]bout  [#FFFF55]Q[white]uit "
		}
		if hasText {
			menu = strings.Replace(menu, "[#FFFF55]A[white]bout  ", "", 1)
			menu = strings.Replace(menu, "[#FFFF55]F[white]ile", "[#FFFF55]F[white]ile  [#FFFF55]N[white]umWords", 1)
			menu = strings.Replace(menu, "[#FFFF55]P[white]lay", "[#FFFF55]P[white]lay  [#FFFF55]W[white]ave  c[#FFFF55]L[white]ear", 1)
		}

		if config.StatsTotalWords > 0 {
			menu = strings.Replace(menu, "[#FFFF55]O[white]ption", "[#FFFF55]O[white]ption  [#FFFF55]D[white]ataStats", 1)
		}

	case StatePlaying:
		statusLine.SetText(" [#FFFF55]Playing")

		if config.User.Echo {
			menu = "[#FFFF55]S[white]top  [#FFFF55]D[white]ataStats"
		} else {
			menu = "[#FFFF55]P[white]ause  [#FFFF55]S[white]top  [#FFFF55]I[white]mpairments [#FFFF55]T[white]iming"
		}
	case StatePaused:
		statusLine.SetText(" [#FFFF55]Paused")

		if config.User.Echo {
			menu = "[#FFFF55]S[white]top  [#FFFF55]D[white]ataStats  [#FFFF55]Q[white]uit "
		} else {
			menu = "[#FFFF55]P[white]ause/Resume  [#FFFF55]S[white]top  [#FFFF55]I[white]mpairments  [#FFFF55]Q[white]uit "
		}
	}
	header.SetText("[#55FFFF::b] YAMA: Yet Another Morse-code App\nv" + Ver + "[white::-]\n" + menu)
}

// --- UI Components & Modals ---

func applyFocusStyles(form *tview.Form) {
	for i := 0; i < form.GetFormItemCount(); i++ {
		item := form.GetFormItem(i)
		if input, ok := item.(*tview.InputField); ok {
			input.SetFocusFunc(func() { input.SetFieldBackgroundColor(tcell.ColorDarkGreen) })
			input.SetBlurFunc(func() { input.SetFieldBackgroundColor(tcell.ColorBlue) })
		}
	}
}

func createModal(p tview.Primitive, width, height int) tview.Primitive {
	app.EnableMouse(true)

	return tview.NewGrid().
		SetColumns(0, width, 0). // Left space (auto), Center (fixed width), Right space (auto)
		SetRows(0, height, 0).   // Top space (auto), Center (fixed height), Bottom space (auto)
		AddItem(p, 1, 1, 1, 1, 0, 0, true)
}

func showErrorModal(errors []string, onDismiss func()) {
	app.EnableMouse(true)
	msg := "[yellow::b]Configuration Adjustments Required:[::-]\n\n"
	for _, e := range errors {
		msg += "- " + e + "\n"
	}
	msg += "\n[white]Value(s) must be corrected to Save.[-]"

	modal := tview.NewModal().
		SetText(msg).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			pages.RemovePage("errorModal")
			if onDismiss != nil {
				onDismiss()
			}
		})
	modal.SetBackgroundColor(tcell.ColorDarkRed)
	pages.AddPage("errorModal", modal, true, true)
	app.SetFocus(modal)
}

func showAbout() {
	app.EnableMouse(true)
	aboutText := `About [#55FFFF]YAMA: Yet Another Morse-code App[-]
` + "Version " + Ver +
		`
Created by: Bill Lanahan, WA2NFN

Send feedback to wa2nfn@gmail.com.

License & Terms of Use
This software is shared with the community under the Creative Commons Attribution-NonCommercial 4.0 International (CC BY-NC 4.0) license.

You are free to: Share, copy, and modify this software.

Under the following terms: You must give appropriate credit to the original creator.

Non-Commercial: You may not use this material for commercial purposes. This means you cannot sell this app or its source code, nor include it in a paid bundle.

To view a copy of this license, visit: http://creativecommons.org/licenses/by-nc/4.0/

Disclaimer of Warranty
THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, AND NON-INFRINGEMENT.

IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES, OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT, OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE. USE AT YOUR OWN RISK.`

	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetText(aboutText).
		SetWrap(true).
		SetWordWrap(true)
	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	tv.SetBorder(false)

	form := tview.NewForm().
		AddButton("Close", func() {
			pages.RemovePage("about")
			app.SetFocus(inputArea)
		})
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	applyFocusStyles(form)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tv, 0, 1, true).
		AddItem(form, 3, 1, false)

	layout.SetBorder(true).SetTitle(" About ")
	layout.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("about")
			app.SetFocus(inputArea)
			return nil
		}
		if event.Key() == tcell.KeyTab {
			app.SetFocus(form)
			return nil
		}
		return event
	})

	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("about")
			app.SetFocus(inputArea)
			return nil
		}
		if event.Key() == tcell.KeyBacktab {
			app.SetFocus(tv)
			return nil
		}
		return event
	})

	pages.AddPage("about", createModal(layout, 110, 32), true, true)
	app.SetFocus(layout)
}

func showStats() {
	app.EnableMouse(true)
	statsIWRList = make([]string, 0, len(statsIWRMap))
	for k := range statsIWRMap {
		statsIWRList = append(statsIWRList, k)
	}
	sort.Strings(statsIWRList)

	var activePlayTime time.Duration
	if finalPlayTime > 0 {
		activePlayTime = finalPlayTime
	} else {
		totalElapsed := time.Since(morse.StartTime)
		currentTotalPaused := morse.TotalPaused
		if currentState == StatePaused {
			currentTotalPaused += time.Since(morse.PauseStart)
		}
		activePlayTime = totalElapsed - currentTotalPaused
	}

	totalSecs := int(activePlayTime.Seconds())
	m := totalSecs / 60
	s := totalSecs % 60
	timeStr := fmt.Sprintf("%dm %ds", m, s)

	lifeTotal := config.User.LifetimePlaySeconds
	lifeH := lifeTotal / 3600
	lifeM := (lifeTotal % 3600) / 60
	lifeS := lifeTotal % 60

	var lifeStr string
	if lifeH > 0 {
		lifeStr = fmt.Sprintf("%dh %dm %ds", lifeH, lifeM, lifeS)
	} else {
		lifeStr = fmt.Sprintf("%dm %ds", lifeM, lifeS)
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("[yellow::b] Lifetime Practice: %s[::-]\n", lifeStr))
	sb.WriteString(strings.Repeat("-", 36) + "\n\n")

	sb.WriteString("[white::b] Current Session[::-]\n")
	sb.WriteString(fmt.Sprintf(" Total Words Played: %d\n", config.StatsTotalWords))
	sb.WriteString(fmt.Sprintf(" Total IWR Matches: %d\n", statsIWRWords))
	sb.WriteString(fmt.Sprintf(" Active Play Time: %s\n\n", timeStr))

	sb.WriteString("[#00BFFF::b] IWR WORD    COUNT[::-]\n")
	sb.WriteString(strings.Repeat("-", 20) + "\n")

	for _, w := range statsIWRList {
		sb.WriteString(fmt.Sprintf(" %-11s  %d\n", w, statsIWRMap[w]))
	}

	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetText(sb.String())
	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	tv.SetBorder(false)

	form := tview.NewForm().
		AddButton("Close (ESC)", func() {
			pages.RemovePage("stats")
			app.SetFocus(inputArea)
		})
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	applyFocusStyles(form)

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tv, 0, 1, true).
		AddItem(form, 3, 1, false)

	layout.SetBorder(true).SetTitle(" DataStats ")
	layout.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyCtrlC {
			pages.RemovePage("stats")
			app.SetFocus(inputArea)
			return nil
		}
		if event.Key() == tcell.KeyTab {
			app.SetFocus(form)
			return nil
		}
		return event
	})

	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape { // <--- Removed Ctrl-C
			pages.RemovePage("stats")
			app.SetFocus(inputArea)
			return nil
		}
		if event.Key() == tcell.KeyBacktab {
			app.SetFocus(tv)
			return nil
		}
		return event
	})

	pages.AddPage("stats", createModal(layout, 40, 26), true, true)
	app.SetFocus(tv)
}

func showFile(app *tview.Application, pages *tview.Pages, inputArea *tview.TextArea) {
	inputArea.SetMouseCapture(func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
		return action, nil 
	})

	list := tview.NewList().ShowSecondaryText(false)
	list.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	list.SetBorder(false)

	currentDir, _ := os.Getwd()
	selectedFiles := make(map[string]bool) 

	populate := func(dir string) {
		currentIndex := list.GetCurrentItem()

		list.Clear()
		files, err := os.ReadDir(dir)
		if err != nil {
			return
		}

		list.AddItem("..", "", 0, nil)

		var dirs []os.DirEntry
		var txts []os.DirEntry

		for _, f := range files {
			if f.IsDir() {
				dirs = append(dirs, f)
			} else if strings.HasSuffix(f.Name(), ".txt") {
				txts = append(txts, f)
			}
		}

		for _, d := range dirs {
			list.AddItem("[#00BFFF]"+d.Name()+"/", "", 0, nil)
		}

		if len(dirs) == 0 && len(txts) == 0 {
			list.AddItem("[gray](Directory is empty)[-]", "", 0, nil)
		} else if len(dirs) == 0 {
			list.AddItem("[gray](No sub-directories found)[-]", "", 0, nil)
		}

		for _, t := range txts {
			fullPath := filepath.Join(dir, t.Name())
			prefix := "  "
			if selectedFiles[fullPath] {
				prefix = "[yellow]* [-]" // VISUAL TAG
			}
			list.AddItem(prefix+t.Name(), "", 0, nil)
		}

		if currentIndex < list.GetItemCount() {
			list.SetCurrentItem(currentIndex)
		}
	}

	populate(currentDir)

	// Helper to strip formatting tags to get the real filename
	getCleanName := func(main string) string {
		name := strings.TrimPrefix(main, "[yellow]* [-]")
		name = strings.TrimPrefix(name, "  ")
		name = strings.TrimSuffix(strings.TrimPrefix(name, "[#00BFFF]"), "/")
		return name
	}

	onSelect := func(main string) {
		name := getCleanName(main)

		if name == ".." {
			currentDir = filepath.Dir(currentDir)
			populate(currentDir)
			return
		}

		if name == "[gray](No sub-directories found)[-]" ||
			name == "[gray](Directory is empty)[-]" {
			return
		}

		full := filepath.Join(currentDir, name)

		info, err := os.Stat(full)
		if err != nil {
			inputArea.SetText("Error finding file: "+full+"\n"+err.Error(), true)
			pages.RemovePage("file")
			inputArea.SetMouseCapture(nil) // RESTORE MOUSE
			app.SetFocus(inputArea)
			return
		}

		if info.IsDir() {
			currentDir = full
			populate(currentDir)
			return
		}

		// GATHER FILES TO READ
		var filesToRead []string

		for path, isSelected := range selectedFiles {
			if isSelected {
				filesToRead = append(filesToRead, path)
			}
		}

		if len(filesToRead) == 0 {
			filesToRead = append(filesToRead, full)
		}

		currentInputFile = "Multiple Files"
		if len(filesToRead) == 1 {
			currentInputFile = filepath.Base(filesToRead[0])
		}
		currentFileDir = currentDir

		go func(targets []string) {
			stopAudio()
			time.Sleep(150 * time.Millisecond)

			var combinedData []byte
			for _, targetFile := range targets {
				data, err := os.ReadFile(targetFile)
				if err != nil {
					app.QueueUpdateDraw(func() {
						inputArea.SetText("Error reading file: "+err.Error(), true)
						pages.RemovePage("file")
						inputArea.SetMouseCapture(nil) // RESTORE MOUSE
						app.SetFocus(inputArea)
					})
					return
				}
				combinedData = append(combinedData, data...)
				combinedData = append(combinedData, []byte(" ")...) // Space between files
			}

			txt := parser.NormalizeText(string(combinedData))

			if config.User.UseSkip {
				parser.SetSkipList(config.User.SkipList, morse.ProSignTable)
				txt = parser.ApplySkip(txt)
			}

			txt = strings.ToUpper(txt)
			txt = parser.FilterValidMorse(txt, morse.MorseTable)
			txt = parser.CompressSpace(txt)

			app.QueueUpdateDraw(func() {
				inputArea.SetChangedFunc(nil) 
				inputArea.SetText(txt, false)
				inputArea.SetChangedFunc(onInputAreaChanged)

				pages.RemovePage("file")
				inputArea.SetMouseCapture(nil) // RESTORE MOUSE
				app.SetFocus(inputArea)
				refreshUI(currentState)
			})
		}(filesToRead)
	}

	list.SetSelectedFunc(func(i int, main string, sec string, r rune) {
		onSelect(main)
	})

	form := tview.NewForm().
		AddButton("Open Selected", func() {
			i := list.GetCurrentItem()
			main, _ := list.GetItemText(i)
			onSelect(main)
		}).
		AddButton("Cancel", func() {
			pages.RemovePage("file")
			inputArea.SetMouseCapture(nil) // RESTORE MOUSE
			app.SetFocus(inputArea)
		})
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	applyFocusStyles(form)

	footerView := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetDynamicColors(true)
	footerView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	footerView.SetText("[yellow]Arrows: Scroll  -  Space: Tag File\nEnter: Open / Change Directory[-]") // Added newline

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(form, 3, 1, false).
		AddItem(footerView, 2, 1, false) // Bumped height from 1 to 2

	layout.SetBorder(true).SetTitle(" Input Files ")
	layout.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == ' ' {
			i := list.GetCurrentItem()
			main, _ := list.GetItemText(i)
			name := getCleanName(main)

			full := filepath.Join(currentDir, name)
			info, err := os.Stat(full)

			if err == nil && !info.IsDir() {
				selectedFiles[full] = !selectedFiles[full]
				populate(currentDir)
				if i+1 < list.GetItemCount() {
					list.SetCurrentItem(i + 1)
				}
			}
			return nil
		}

		if event.Key() == tcell.KeyTab {
			app.SetFocus(form)
			return nil
		}
		return event
	})

	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyBacktab || event.Key() == tcell.KeyTab {
			app.SetFocus(list)
			return nil
		}
		return event
	})

	layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("file")
			inputArea.SetMouseCapture(nil) // RESTORE MOUSE
			app.SetFocus(inputArea)
			return nil
		}
		return event
	})

	modalGrid := tview.NewGrid().
		SetColumns(0, 55, 0).
		SetRows(0, 27, 0). // Bumped from 26 to 27
		AddItem(layout, 1, 1, 1, 1, 0, 0, true)

	pages.AddPage("file", modalGrid, true, true)
	app.SetFocus(list)
}

var preResizeSnapshot string
var echoStatsTextView *tview.TextView
var currentSessionStats morse.EchoStats

func startAudioSequence(iwrMan *morse.IWRManager) {
	if morse.AudioHardwareDead {
		statusLine.SetText(" [#FFFF55::b]FATAL: Audio hardware lost. Restart app.[::-]")
		app.Stop()
		log.Fatalf("FATAL: Audio hardware lost. Restart app.")
		return
	}

	time.Sleep(250 * time.Millisecond)

	morse.IsStopping = false
	morse.IsPaused = false
	isBlocked = false
	clearStats()

	rawInput := inputArea.GetText()
	rawInput = colorTagRegex.ReplaceAllString(rawInput, "")

	fullTextToPlay = parser.NormalizeText(rawInput)

	if config.User.UseSkip {
		parser.SetSkipList(config.User.SkipList, morse.ProSignTable)
		fullTextToPlay = parser.ApplySkip(fullTextToPlay)
	}

	actualText = ""
	inputArea.SetText("", false)

	parsedText := parser.FilterValidMorse(fullTextToPlay, morse.MorseTable)
	parsedText = parser.CompressSpace(parsedText)

	if (config.User.WordBuilder && config.User.WordBuilderSort) || (config.User.TextBuilder && config.User.TextBuilderSort) {
		parsedText = SortByWordLength(parsedText)
	}

	if config.User.StartMsg {
		txt := strings.TrimSpace(config.User.StartMsgText)
		if txt == "" {
			txt = "VVV <KA>"
		}
		config.User.StartMsgText = strings.ToUpper(txt)
	}

	if config.User.EndMsg {
		txt := strings.TrimSpace(config.User.EndMsgText)
		if txt == "" {
			txt = "<AR>"
		}
		config.User.EndMsgText = strings.ToUpper(txt)
	}

	parsedText = parser.CompressSpace(parsedText)

	if config.User.StartDelay > 0 {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					playPauseMu.Lock()
					isCountingDown = false
					playPauseMu.Unlock()

					app.Stop()
					log.Fatalf("\nFATAL YAMA Crashed in delayed engine start: %v\n", r)
				}
			}()

			for i := config.User.StartDelay; i > 0; i-- {
				if morse.IsStopping {
					playPauseMu.Lock()
					isCountingDown = false
					playPauseMu.Unlock()
					return
				}
				app.QueueUpdateDraw(func() {
					msg := fmt.Sprintf("\n\n\n     Starting in %d...", i)
					inputArea.SetText(msg, false)
					statusLine.SetText(" [#FFFF55]Preparing to Play...")
				})
				time.Sleep(1 * time.Second)
			}

			playPauseMu.Lock()

			if morse.IsStopping {
				isCountingDown = false
				playPauseMu.Unlock()
				return
			}

			isCountingDown = false
			currentState = StatePlaying

			app.QueueUpdateDraw(func() {
				inputArea.SetText("", false)
				refreshUI(StatePlaying)

				morse.StartTime = time.Now()
				go runEngine(parsedText, iwrMan)
			})

			playPauseMu.Unlock()
		}()
	} else {
		currentState = StatePlaying
		refreshUI(StatePlaying)

		morse.StartTime = time.Now()
		go runEngine(parsedText, iwrMan)
	}
}

func SortByWordLength(text string) string {
	words := strings.Fields(text)
	slices.SortFunc(words, func(a, b string) int {
		return cmp.Compare(len(a), len(b))
	})
	return strings.Join(words, " ")
}

func closeEchoStatsWindow() {
	if currentEchoView != nil {
		mainFlex.RemoveItem(currentEchoView)
		currentEchoView = nil
	}
}

func showStatsEcho() *tview.Flex {
	footer := tview.NewTextView().
		SetText(" [yellow]Ctrl-D to Toggle ").
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)

	container := tview.NewFlex().SetDirection(tview.FlexRow)

	var title = " DataStats "
	container.SetBorder(true).
		SetTitle(title).
		SetTitleColor(tcell.ColorYellow)

	echoStatsTextView = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)

	currentProfile := morse.GetTiming(false, config.User)

	if config.User.KeyDevice == "Paddle" || config.User.KeyDevice == "IambicA" || config.User.KeyDevice == "IambicB" || config.User.KeyDevice == "Ultimatic" {
		echoStatsTextView.SetText(buildEchoStatsPaddleText(currentGroupStats, currentSessionStats, currentProfile))
	} else {
		echoStatsTextView.SetText(buildEchoStatsText(currentGroupStats, currentSessionStats, currentProfile))
	}

	container.AddItem(echoStatsTextView, 0, 1, false)
	container.AddItem(footer, 1, 0, false)

	container.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			closeEchoStatsWindow()
			app.SetFocus(inputArea)
			return nil
		}
		return event
	})

	app.SetFocus(container)
	return container
}

func buildEchoStatsText(grp morse.EchoStats, ses morse.EchoStats, tp morse.TimingProfile) string {
	var sb strings.Builder

	calcAvg := func(sumMs float64, short, perfect, long int) int {
		totalCount := short + perfect + long
		if totalCount > 0 {
			return int(math.Round(sumMs / float64(totalCount)))
		}
		return 0
	}

	tgtDit := int(math.Round(tp.DitDuration * 1000))
	tgtDah := int(math.Round(tp.DahDuration * 1000))
	tgtEle := int(math.Round(tp.InterElement * 1000))
	tgtChr := int(math.Round(tp.CharSpace * 1000))
	tgtWrd := int(math.Round(tp.WordSpace * 1000))

	avgDit := calcAvg(ses.SumDitMs, ses.ShortDits, ses.PerfectDits, ses.LongDits)
	avgDah := calcAvg(ses.SumDahMs, ses.ShortDahs, ses.PerfectDahs, ses.LongDahs)
	avgEle := calcAvg(ses.SumElementGapsMs, ses.ShortElementGaps, ses.PerfectElementGaps, ses.LongElementGaps)
	avgChr := calcAvg(ses.SumCharGapsMs, ses.ShortCharGaps, ses.PerfectCharGaps, ses.LongCharGaps)
	avgWrd := calcAvg(ses.SumWordGapsMs, ses.ShortWordGaps, ses.PerfectWordGaps, ses.LongWordGaps)

	sb.WriteString("\n                     [yellow::b]CURRENT GROUP[-:-:-]                            [yellow::b]SESSION TOTALS[-:-:-]\n\n")

	sb.WriteString("  [cyan]Elements[-]                                                                              [cyan::b]Target(ms)    Avg(ms)[-:-:-]\n")
	sb.WriteString(fmt.Sprintf("    Dits:        [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long      [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long       [white]%4d[-]       [white]%4d[-]\n",
		grp.ShortDits, grp.PerfectDits, grp.LongDits, ses.ShortDits, ses.PerfectDits, ses.LongDits, tgtDit, avgDit))
	sb.WriteString(fmt.Sprintf("    Dahs:        [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long      [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long       [white]%4d[-]       [white]%4d[-]\n",
		grp.ShortDahs, grp.PerfectDahs, grp.LongDahs, ses.ShortDahs, ses.PerfectDahs, ses.LongDahs, tgtDah, avgDah))

	sb.WriteString("  [cyan]Spacing[-]\n")
	sb.WriteString(fmt.Sprintf("    Intra-Char:  [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long      [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long       [white]%4d[-]       [white]%4d[-]\n",
		grp.ShortElementGaps, grp.PerfectElementGaps, grp.LongElementGaps, ses.ShortElementGaps, ses.PerfectElementGaps, ses.LongElementGaps, tgtEle, avgEle))
	sb.WriteString(fmt.Sprintf("    Char Gap:    [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long      [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long       [white]%4d[-]       [white]%4d[-]\n",
		grp.ShortCharGaps, grp.PerfectCharGaps, grp.LongCharGaps, ses.ShortCharGaps, ses.PerfectCharGaps, ses.LongCharGaps, tgtChr, avgChr))
	sb.WriteString(fmt.Sprintf("    Word Gap:    [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long      [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long       [white]%4d[-]       [white]%4d[-]\n",
		grp.ShortWordGaps, grp.PerfectWordGaps, grp.LongWordGaps, ses.ShortWordGaps, ses.PerfectWordGaps, ses.LongWordGaps, tgtWrd, avgWrd))

	sb.WriteString("  [cyan]Accuracy[-]\n")
	sb.WriteString(fmt.Sprintf("  Invalid (*):   [red]%3d[-]                                  [red]%3d[-]\n", grp.InvalidSymbols, ses.InvalidSymbols))
	sb.WriteString(fmt.Sprintf("  Group Retries: [yellow]%3d[-]                                  [yellow]%3d[-]\n", grp.Retries, ses.Retries))
	sb.WriteString(fmt.Sprintf("  Chars:         [yellow]%3d[-]                                  [yellow]%3d[-]\n", grp.TotalChars, ses.TotalChars))
	sb.WriteString(fmt.Sprintf("  Words:         [yellow]%3d[-]                                  [yellow]%3d[-]\n", grp.TotalWords, ses.TotalWords))

	return sb.String()
}

func buildEchoStatsPaddleText(grp morse.EchoStats, ses morse.EchoStats, tp morse.TimingProfile) string {
	var sb strings.Builder

	calcAvg := func(sumMs float64, short, perfect, long int) int {
		totalCount := short + perfect + long
		if totalCount > 0 {
			return int(math.Round(sumMs / float64(totalCount)))
		}
		return 0
	}

	tgtChr := int(math.Round(tp.CharSpace * 1000))
	tgtWrd := int(math.Round(tp.WordSpace * 1000))

	avgChr := calcAvg(ses.SumCharGapsMs, ses.ShortCharGaps, ses.PerfectCharGaps, ses.LongCharGaps)
	avgWrd := calcAvg(ses.SumWordGapsMs, ses.ShortWordGaps, ses.PerfectWordGaps, ses.LongWordGaps)

	sb.WriteString("\n                     [yellow::b]CURRENT GROUP[-:-:-]                            [yellow::b]SESSION TOTALS[-:-:-]\n\n")

	sb.WriteString("  [cyan]Spacing[-]                                                                               [cyan::b]Target(ms)    Avg(ms)[-:-:-]\n")
	sb.WriteString(fmt.Sprintf("    Char Gap:    [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long      [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long       [white]%4d[-]       [white]%4d[-]\n",
		grp.ShortCharGaps, grp.PerfectCharGaps, grp.LongCharGaps, ses.ShortCharGaps, ses.PerfectCharGaps, ses.LongCharGaps, tgtChr, avgChr))
	sb.WriteString(fmt.Sprintf("    Word Gap:    [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long      [red]%3d[-] Short | [green]%3d[-] Good | [red]%3d[-] Long       [white]%4d[-]       [white]%4d[-]\n",
		grp.ShortWordGaps, grp.PerfectWordGaps, grp.LongWordGaps, ses.ShortWordGaps, ses.PerfectWordGaps, ses.LongWordGaps, tgtWrd, avgWrd))

	sb.WriteString("\n  [cyan]Accuracy[-]\n")
	sb.WriteString(fmt.Sprintf("  Invalid (*):   [red]%3d[-]                                  [red]%3d[-]\n", grp.InvalidSymbols, ses.InvalidSymbols))
	sb.WriteString(fmt.Sprintf("  Group Retries: [yellow]%3d[-]                                  [yellow]%3d[-]\n", grp.Retries, ses.Retries))
	sb.WriteString(fmt.Sprintf("  Chars:         [yellow]%3d[-]                                  [yellow]%3d[-]\n", grp.TotalChars, ses.TotalChars))
	sb.WriteString(fmt.Sprintf("  Words:         [yellow]%3d[-]                                  [yellow]%3d[-]\n", grp.TotalWords, ses.TotalWords))

	return sb.String()
}
