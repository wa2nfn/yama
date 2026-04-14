package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

func handlePlayPause(iwrMan *morse.IWRManager) {
	if currentState == StateIdle || currentState == StateStopped {
		startAudioSequence(iwrMan)
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
	statsTotalWords = 0
	statsIWRWords = 0
	statsIWRMap = make(map[string]int)
	statsIWRList = []string{}

	morse.TotalPaused = 0
	finalPlayTime = 0
}

func runEngine(parsedText string, iwrMan *morse.IWRManager) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic Error: %v", r)
			app.QueueUpdateDraw(func() {
				isBlocked = false
				currentState = StateStopped
				statusLine.SetText(" [#00FF00]Engine Crashed!")
				refreshUI(StateStopped)
			})
		}
	}()

	morse.RunIWR(parsedText, iwrMan)

	app.QueueUpdateDraw(func() {
		isBlocked = false
		if !morse.IsStopping {
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

	if finalPlayTime == 0 {
		finalPlayTime = time.Since(morse.StartTime) - morse.TotalPaused
	}

	isBlocked = false
	updateVisibility()

	currentState = StateStopped
	statusLine.SetText(" [#00FF00]Stopped")
	refreshUI(StateStopped)
}

func updateVisibility() {
	if isBlocked {
		inputArea.SetText(strings.Repeat("*", len(actualText)), false)
	} else {
		inputArea.SetText(actualText, false)
	}
}

func updateBlueLine() {
	iwrStatus := "OFF"
	if config.User.IWREnabled {
		iwrStatus = "ON"
	}

	var info string
	if config.User.UseStandard {
		info = fmt.Sprintf(" [black]Character Speed: %d wpm | IWR: %s (%d wpm) ", config.User.CharacterSpeed, iwrStatus, config.User.IWRSpeed)
	} else {
		mode := "Farnsworth"
		if config.User.UseWordsworth {
			mode = "Wordsworth"
		}
		info = fmt.Sprintf(" [black]Mode: %s | Character Speed: %d wpm | Effective Speed: %d wpm | IWR: %s (%d wpm) ", mode, config.User.CharacterSpeed, config.User.EffectiveSpeed, iwrStatus, config.User.IWRSpeed)
	}
	blueLine.SetText(info)
}

func refreshUI(state AppState) {
	var menu string
	hasText := len(inputArea.GetText()) > 0

	switch state {
	case StateIdle, StateStopped:
		status := "Ready"
		if state == StateStopped {
			status = "Stopped"
		}
		statusLine.SetText(" [#00FF00]" + status)
		menu = "[#00FF00]F[white]ile  [#00FF00]P[white]lay  [#00FF00]T[white]iming  [#00FF00]O[white]ption  [#00FF00]A[white]bout  [#00FF00]H[white]elp  [#00FF00]Q[white]uit"

		if hasText {
			menu = strings.Replace(menu, "[#00FF00]P[white]lay", "[#00FF00]P[white]lay  [#00FF00]W[white]ave  [#00FF00]E[white]rase", 1)
		}

		if statsTotalWords > 0 {
			menu = strings.Replace(menu, "[#00FF00]O[white]ption", "[#00FF00]O[white]ption  [#00FF00]D[white]ata-Stats", 1)
		}

	case StatePlaying:
		statusLine.SetText(" [#00FF00]Playing")
		menu = "[#00FF00]P[white]ause  [#00FF00]S[white]top"
	case StatePaused:
		statusLine.SetText(" [#00FF00]Paused")
		menu = "[#00FF00]R[white]esume  [#00FF00]S[white]top  [#00FF00]T[white]iming "
	}
	header.SetText("[#00FF00::b] YAMA - Yet Another Morse App [white::-]\n" + menu)
}

// --- UI Components & Modals ---

// applyFocusStyles forces InputFields to light up Green when focused
func applyFocusStyles(form *tview.Form) {
	for i := 0; i < form.GetFormItemCount(); i++ {
		item := form.GetFormItem(i)
		if input, ok := item.(*tview.InputField); ok {
			input.SetFocusFunc(func() { input.SetFieldBackgroundColor(tcell.ColorDarkGreen) })
			input.SetBlurFunc(func() { input.SetFieldBackgroundColor(tcell.ColorBlack) })
		}
	}
}

func createModal(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, true).
		AddItem(nil, 0, 1, false)
}

func showErrorModal(errors []string, onDismiss func()) {
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

func showStats() {
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

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Total Words Played: %d\n", statsTotalWords))
	sb.WriteString(fmt.Sprintf("Total IWR Matches: %d\n", statsIWRWords))
	sb.WriteString(fmt.Sprintf("Active Play Time: %s\n\n", timeStr))

	sb.WriteString("[::b]IWR WORD    COUNT[::-]\n")
	sb.WriteString(strings.Repeat("-", 20) + "\n")

	for _, w := range statsIWRList {
		sb.WriteString(fmt.Sprintf("%-11s %d\n", w, statsIWRMap[w]))
	}

	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetScrollable(true).
		SetText(sb.String())

	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor)).
		SetBorder(true).
		SetTitle(" Data Stats ")

	pages.AddPage("stats", createModal(tv, 30, 20), true, true)
	app.SetFocus(tv)
}

func showHelp() {
	helpText := `
 [yellow]Keyboard Shortcuts:[-]
 [green]Ctrl-P:[-] Play / Pause / Resume
 [green]Ctrl-S:[-] Stop Audio
 [green]Ctrl-F:[-] File Explorer
 [green]Ctrl-T:[-] Tone & Speed Settings
 [green]Ctrl-O:[-] General Options
 [green]Ctrl-W:[-] Generate WAV Files
 [green]Ctrl-E:[-] Erase Input
 [green]Ctrl-D:[-] View Session Stats
 [green]Space:[-] Play / Pause
 [green]Ctrl-Q:[-] Quit App
 [green]ESC:[-]    Close Modals / Stop Audio
	`
	tv := tview.NewTextView().SetDynamicColors(true).SetText(helpText)
	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor)).SetBorder(true).SetTitle(" Help ")
	tv.SetScrollable(true)

	pages.AddPage("help", createModal(tv, 40, 17), true, true)
	app.SetFocus(tv)
}

func showAbout() {
	aboutText := `About YAMA - Yet Another Morse App
 ` + Version +
		`
Created by: Bill Lanahan WA2NFN

Positive feedback accepted at cw.or.bust@gmail.com

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

	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor)).
		SetBorder(true).
		SetTitle(" About ")

	pages.AddPage("about", createModal(tv, 70, 26), true, true)
	app.SetFocus(tv)
}

func showFile() {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBackgroundColor(tcell.GetColor(AppBackgroundColor)).SetBorder(true).SetTitle(" Input Files (cursor & enter) ")

	currentDir, _ := os.Getwd()
	populate := func(dir string) {
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

		for _, f := range dirs {
			list.AddItem("[#00BFFF]"+f.Name()+"/", "", 0, nil)
		}

		// The Empty Directory Indicator
		if len(dirs) == 0 && len(txts) == 0 {
			list.AddItem("[gray](Directory is empty)[-]", "", 0, nil)
		} else if len(dirs) == 0 {
			list.AddItem("[gray](No sub-directories found)[-]", "", 0, nil)
		}

		for _, f := range txts {
			list.AddItem(f.Name(), "", 0, nil)
		}
	}
	populate(currentDir)

	list.SetSelectedFunc(func(i int, main string, sec string, r rune) {
		name := strings.TrimSuffix(strings.TrimPrefix(main, "[#00BFFF]"), "/")

		if name == ".." {
			currentDir = filepath.Dir(currentDir)
			populate(currentDir)
			return
		}
		if name == "[gray](No sub-directories found)[-]" || name == "[gray](Directory is empty)[-]" {
			return // Unclickable
		}

		full := filepath.Join(currentDir, name)

		info, err := os.Stat(full)
		if err != nil {
			inputArea.SetText("Error finding file: "+full+"\n"+err.Error(), false)
			pages.RemovePage("file")
			app.SetFocus(inputArea)
			app.Draw()
			return
		}

		if info.IsDir() {
			currentDir = full
			populate(currentDir)

		} else {
			currentInputFile = name
			currentFileDir = currentDir // Capture for Wave Exporter

			go func(targetFile string) {
				stopAudio()

				data, err := os.ReadFile(targetFile)
				if err != nil {
					app.QueueUpdateDraw(func() {
						inputArea.SetText("Error reading file: "+err.Error(), false)
						app.SetFocus(inputArea)
						pages.RemovePage("file")
					})
					return
				}

				txt := string(data)
				txt = strings.ReplaceAll(txt, "\r", "")
				txt = strings.ReplaceAll(txt, "\n", " ")
				txt = strings.ToUpper(txt)

				if config.User.UseSkip {
					parser.SetSkipList(config.User.SkipList, morse.ProSignTable)
					txt = parser.ApplySkip(txt)
				}

				actualText := parser.CleanText(txt, morse.MorseTable)

				app.QueueUpdateDraw(func() {
					inputArea.SetChangedFunc(nil)
					pages.RemovePage("file")
					app.SetFocus(inputArea)
					inputArea.SetText(actualText, false)

					inputArea.SetChangedFunc(func() {
						if currentState == StateStopped {
							currentState = StateIdle
							refreshUI(currentState)
						}
					})

					if currentState == StateStopped {
						currentState = StateIdle
						refreshUI(currentState)
					}
				})

			}(full)
		}

	})
	pages.AddPage("file", createModal(list, 60, 20), true, true)
	app.SetFocus(list)
}

func showOptions() {
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)
	form.SetItemPadding(0)

	useProsignsCb := tview.NewCheckbox().SetLabel("Use Prosigns")
	extendedPuncCb := tview.NewCheckbox().SetLabel("All Punctuation")
	useSkipCb := tview.NewCheckbox().SetLabel("Use Skip")

	skipListInput := tview.NewInputField().SetLabel("Skip List").SetFieldWidth(35)
	skipListInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)
	skipListInput.SetPlaceholder(" e.g. <AR> <SK>").SetPlaceholderTextColor(tcell.ColorYellow)

	delayOptions := []string{"0", "1", "2", "3", "4", "5"}
	delayDropDown := tview.NewDropDown().SetLabel("Start Delay (sec)").SetOptions(delayOptions, nil)

	repeatOptions := []string{"2", "3", "4", "5", "6", "7", "8", "9"}
	repeatDropDown := tview.NewDropDown().SetLabel("Repeat Limit").SetOptions(repeatOptions, nil)

	randomOrderCb := tview.NewCheckbox().SetLabel("Random Order")
	randomWordsCb := tview.NewCheckbox().SetLabel("Random Words")
	wordBuilderCb := tview.NewCheckbox().SetLabel("Word Builder")

	startMsgCb := tview.NewCheckbox().SetLabel("Start Msg")
	startMsgInput := tview.NewInputField().SetLabel("Start Msg Text").SetFieldWidth(20)
	startMsgInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)
	startMsgInput.SetPlaceholder(" e.g. VVV <KA>").SetPlaceholderTextColor(tcell.ColorYellow)

	endMsgCb := tview.NewCheckbox().SetLabel("End Msg")
	endMsgInput := tview.NewInputField().SetLabel("End Msg Text").SetFieldWidth(20)
	endMsgInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)
	endMsgInput.SetPlaceholder(" e.g. <AR>").SetPlaceholderTextColor(tcell.ColorYellow)

	resetState := func() {
		useProsignsCb.SetChecked(config.User.UseProsigns)
		extendedPuncCb.SetChecked(config.User.UseExtendedPunctuation)
		useSkipCb.SetChecked(config.User.UseSkip)
		skipListInput.SetText(config.User.SkipList)
		randomOrderCb.SetChecked(config.User.RandomOrder)
		randomWordsCb.SetChecked(config.User.RandomWords)
		wordBuilderCb.SetChecked(config.User.WordBuilder)

		startMsgCb.SetChecked(config.User.StartMsg)
		startMsgInput.SetText(config.User.StartMsgText) // Applies empty string if no config, revealing placeholder

		endMsgCb.SetChecked(config.User.EndMsg)
		endMsgInput.SetText(config.User.EndMsgText) // Applies empty string if no config, revealing placeholder

		dIdx := 0
		for i, opt := range delayOptions {
			if opt == fmt.Sprintf("%d", config.User.StartDelay) {
				dIdx = i
				break
			}
		}
		delayDropDown.SetCurrentOption(dIdx)

		rIdx := 0
		for i, opt := range repeatOptions {
			if opt == fmt.Sprintf("%d", config.User.RepeatLimit) {
				rIdx = i
				break
			}
		}
		repeatDropDown.SetCurrentOption(rIdx)
	}

	resetState()

	form.AddFormItem(useProsignsCb)
	form.AddFormItem(extendedPuncCb)
	form.AddFormItem(useSkipCb)
	form.AddFormItem(skipListInput)
	form.AddFormItem(delayDropDown)
	form.AddFormItem(repeatDropDown)
	form.AddFormItem(randomOrderCb)
	form.AddFormItem(randomWordsCb)
	form.AddFormItem(wordBuilderCb)
	form.AddFormItem(startMsgCb)
	form.AddFormItem(startMsgInput)
	form.AddFormItem(endMsgCb)
	form.AddFormItem(endMsgInput)

	onSave := func() {
		isRandWords := randomWordsCb.IsChecked()
		isWB := wordBuilderCb.IsChecked()
		isSkip := useSkipCb.IsChecked()

		var errors []string

		if isWB && isRandWords {
			errors = append(errors, "Word Builder and Random Words cannot both be enabled. Random Words disabled.")
			isRandWords = false
		}

		rawSkipFields := strings.Fields(skipListInput.GetText())
		skipMap := make(map[string]bool)
		var cleanSkips []string
		for _, w := range rawSkipFields {
			lw := strings.ToLower(w)
			if !skipMap[lw] {
				skipMap[lw] = true
				cleanSkips = append(cleanSkips, lw)
			}
		}
		formattedSkipList := strings.Join(cleanSkips, " ")

		apply := func() {
			config.User.UseProsigns = useProsignsCb.IsChecked()
			config.User.UseExtendedPunctuation = extendedPuncCb.IsChecked()
			config.User.UseSkip = isSkip
			config.User.SkipList = formattedSkipList
			config.User.RandomOrder = randomOrderCb.IsChecked()
			config.User.RandomWords = isRandWords
			config.User.WordBuilder = isWB
			config.User.StartMsg = startMsgCb.IsChecked()
			config.User.StartMsgText = startMsgInput.GetText()
			config.User.EndMsg = endMsgCb.IsChecked()
			config.User.EndMsgText = endMsgInput.GetText()

			dIdx, _ := delayDropDown.GetCurrentOption()
			config.User.StartDelay, _ = strconv.Atoi(delayOptions[dIdx])

			rIdx, _ := repeatDropDown.GetCurrentOption()
			config.User.RepeatLimit, _ = strconv.Atoi(repeatOptions[rIdx])

			config.SaveConfig()
			morse.RebuildMorseTable(config.User.UseExtendedPunctuation, config.User.UseSkip, config.User.SkipList)

			updateBlueLine()
			pages.RemovePage("options")
			app.SetFocus(inputArea)
		}

		if len(errors) > 0 {
			showErrorModal(errors, func() { app.SetFocus(form) })
		} else {
			apply()
		}
	}

	onReset := func() {
		resetState()
	}

	form.AddButton("Save", onSave)
	form.AddButton("Reset", onReset)

	applyFocusStyles(form)
	form.SetBorder(false)

	footerView := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetDynamicColors(true)
	footerView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	footerView.SetText("\n[yellow]ESC to Close[-]\n")

	optionsContainer := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true).
		AddItem(footerView, 2, 1, false)

	optionsContainer.SetBorder(true).SetTitle(" Options ")
	optionsContainer.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	pages.AddPage("options", createModal(optionsContainer, 65, 23), true, true)
	app.SetFocus(optionsContainer)
}

func showIWREditModal(parentContainer tview.Primitive) {
	var filePath string
	localPath := "yamaIWR.txt"
	if _, err := os.Stat(localPath); err == nil {
		filePath = localPath
	} else {
		if docDir, errDir := os.UserConfigDir(); errDir == nil {
			filePath = filepath.Join(docDir, "YAMA", "yamaIWR.txt")
		}
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		content = []byte("# Error loading IWR file or file not found.\n")
	}

	textArea := tview.NewTextArea()
	textArea.SetText(string(content), true)
	textArea.SetBorder(true).SetTitle(fmt.Sprintf(" Editing: %s ", filePath))

	// FIX 1: Set to Black so the border is clean and the cursor pops
	textArea.SetBackgroundColor(tcell.ColorBlack)

	form := tview.NewForm()
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	onSave := func() {
		rawText := textArea.GetText()
		var scrubbedLines []string

		for _, line := range strings.Split(rawText, "\n") {
			lineText := strings.TrimSpace(line)
			if lineText == "" || strings.HasPrefix(lineText, "#") {
				scrubbedLines = append(scrubbedLines, line)
				continue
			}

			var cleanWords []string
			for _, word := range strings.Fields(lineText) {
				matchAny := strings.HasSuffix(word, "*")
				cleanWord := strings.TrimSuffix(word, "*")
				cleanWord = morse.ProcessMorseString(cleanWord)
				cleanWord = strings.TrimSpace(cleanWord)

				if len(cleanWord) > 0 {
					if matchAny {
						cleanWord += "*"
					}
					cleanWords = append(cleanWords, cleanWord)
				}
			}
			if len(cleanWords) > 0 {
				scrubbedLines = append(scrubbedLines, strings.Join(cleanWords, " "))
			}
		}

		finalText := strings.Join(scrubbedLines, "\n")
		os.WriteFile(filePath, []byte(finalText), 0644)

		if mgr := morse.GetManager(); mgr != nil {
			mgr.LoadIWRFile()
		}

		pages.RemovePage("iwredit")
		app.SetFocus(parentContainer)
	}

	form.AddButton("Save", onSave)
	form.AddButton("Cancel", func() {
		pages.RemovePage("iwredit")
		app.SetFocus(parentContainer)
	})

	textArea.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// FIX 2: Intercept Tab to jump to the buttons instead of typing a tab space!
		if event.Key() == tcell.KeyTab {
			app.SetFocus(form)
			return nil
		}
		if event.Key() == tcell.KeyCtrlW {
			onSave()
			return nil
		}
		if event.Key() == tcell.KeyEsc {
			pages.RemovePage("iwredit")
			app.SetFocus(parentContainer)
			return nil
		}
		return event
	})

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(textArea, 0, 1, true).
		AddItem(form, 3, 1, false)

	pages.AddPage("iwredit", createModal(layout, 65, 24), true, true)
	app.SetFocus(textArea)
}

func showToneSpeed() {
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	acceptDigits := func(textToCheck string, lastChar rune) bool {
		if textToCheck == "" {
			return true
		}
		_, err := strconv.Atoi(textToCheck)
		return err == nil
	}

	modeDropDown := tview.NewDropDown().SetLabel("Mode").SetOptions([]string{"Standard", "Farnsworth", "Wordsworth"}, nil)

	charInput := tview.NewInputField().SetLabel("Char Speed (wpm)").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
	charInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	effInput := tview.NewInputField().SetLabel("Eff. Speed (wpm)").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
	effInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	toneInput := tview.NewInputField().SetLabel("Tone (Hz)").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
	toneInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	iwrCheckbox := tview.NewCheckbox().SetLabel("Use IWR")

	iwrSpdInput := tview.NewInputField().SetLabel("IWR Speed").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
	iwrSpdInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	iwrToneInput := tview.NewInputField().SetLabel("IWR Tone").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
	iwrToneInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	resetState := func() {
		modeIdx := 0
		if config.User.UseFarnsworth {
			modeIdx = 1
		}
		if config.User.UseWordsworth {
			modeIdx = 2
		}
		modeDropDown.SetCurrentOption(modeIdx)

		charInput.SetText(fmt.Sprintf("%d", config.User.CharacterSpeed))
		effInput.SetText(fmt.Sprintf("%d", config.User.EffectiveSpeed))
		toneInput.SetText(fmt.Sprintf("%d", config.User.Tone))
		iwrCheckbox.SetChecked(config.User.IWREnabled)
		iwrSpdInput.SetText(fmt.Sprintf("%d", config.User.IWRSpeed))
		iwrToneInput.SetText(fmt.Sprintf("%d", config.User.IWRTone))
	}

	resetState()

	form.AddFormItem(modeDropDown)
	form.AddFormItem(charInput)
	form.AddFormItem(effInput)
	form.AddFormItem(toneInput)
	form.AddFormItem(iwrCheckbox)
	form.AddFormItem(iwrSpdInput)
	form.AddFormItem(iwrToneInput)

	var timingContainer *tview.Flex

	onSave := func() {
		optIdx, _ := modeDropDown.GetCurrentOption()
		effSpd, _ := strconv.Atoi(effInput.GetText())
		charSpd, _ := strconv.Atoi(charInput.GetText())
		tone, _ := strconv.Atoi(toneInput.GetText())
		iwrSpd, _ := strconv.Atoi(iwrSpdInput.GetText())
		iwrTone, _ := strconv.Atoi(iwrToneInput.GetText())

		var errors []string

		if effSpd < config.MinEffSpeed || effSpd > config.MaxEffSpeed {
			errors = append(errors, fmt.Sprintf("Effective Speed clamped to %d-%d", config.MinEffSpeed, config.MaxEffSpeed))
			if effSpd < config.MinEffSpeed {
				effSpd = config.MinEffSpeed
			} else {
				effSpd = config.MaxEffSpeed
			}
		}

		if charSpd < config.MinCharSpeed || charSpd > config.MaxCharSpeed {
			errors = append(errors, fmt.Sprintf("Character Speed clamped to %d-%d", config.MinCharSpeed, config.MaxCharSpeed))
			if charSpd < config.MinCharSpeed {
				charSpd = config.MinCharSpeed
			} else {
				charSpd = config.MaxCharSpeed
			}
		}

		if charSpd <= effSpd && optIdx != 0 {
			errors = append(errors, "Character Speed must be > Effective Speed")
			charSpd = effSpd + 1
		}

		if tone < config.MinTone || tone > config.MaxTone {
			errors = append(errors, fmt.Sprintf("Tone clamped to %d-%d Hz", config.MinTone, config.MaxTone))
			if tone < config.MinTone {
				tone = config.MinTone
			} else {
				tone = config.MaxTone
			}
		}

		if iwrTone < config.MinIWRTone || iwrTone > config.MaxIWRTone {
			errors = append(errors, fmt.Sprintf("IWR Tone clamped to %d-%d Hz", config.MinIWRTone, config.MaxIWRTone))
			if iwrTone < config.MinIWRTone {
				iwrTone = config.MinIWRTone
			} else {
				iwrTone = config.MaxIWRTone
			}
		}

		if iwrCheckbox.IsChecked(){
			if iwrSpd < config.MinIWRSpeed || iwrSpd > config.MaxIWRSpeed {
				errors = append(errors, fmt.Sprintf("IWR Speed clamped to %d-%d", config.MinIWRSpeed, config.MaxIWRSpeed))
				if iwrSpd < config.MinIWRSpeed {
					iwrSpd = config.MinIWRSpeed
				} else {
					iwrSpd = config.MaxIWRSpeed
				}
			}

			if iwrSpd <= charSpd {
				errors = append(errors, "IWR Speed must be > Character Speed")
				iwrSpd = charSpd + 1
			}
		}

		apply := func() {
			config.User.UseStandard = (optIdx == 0)
			config.User.UseFarnsworth = (optIdx == 1)
			config.User.UseWordsworth = (optIdx == 2)
			config.User.EffectiveSpeed = effSpd
			config.User.CharacterSpeed = charSpd
			config.User.Tone = tone
			config.User.IWREnabled = iwrCheckbox.IsChecked()
			config.User.IWRSpeed = iwrSpd
			config.User.IWRTone = iwrTone
			config.SaveConfig()

			updateBlueLine()
			pages.RemovePage("tonespeed")
			app.SetFocus(inputArea)
		}

		if len(errors) > 0 {
			showErrorModal(errors, func() { app.SetFocus(form) })
		} else {
			apply()
		}
	}

	onReset := func() {
		resetState()
	}

	onEditIWR := func() {
		showIWREditModal(timingContainer)
	}

	form.AddButton("Save", onSave)
	form.AddButton("Reset", onReset)
	form.AddButton("Edit IWR", onEditIWR)

	applyFocusStyles(form)
	form.SetBorder(false)

	footerView := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetDynamicColors(true)
	footerView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	footerView.SetText("\n[yellow]ESC to Close[-]\n")

	timingContainer = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true).
		AddItem(footerView, 2, 1, false)

	timingContainer.SetBorder(true).SetTitle(" Timing: Speed & Tone ")
	timingContainer.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	pages.AddPage("tonespeed", createModal(timingContainer, 55, 22), true, true)
	app.SetFocus(timingContainer)
}

func startAudioSequence(iwrMan *morse.IWRManager) {

	morse.IsStopping = false
	morse.IsPaused = false
	isBlocked = false
	clearStats()

	rawInput := inputArea.GetText()
	rawInput = strings.ToUpper(rawInput)

	// before any lookups are done
	if config.User.UseSkip {
		parser.SetSkipList(config.User.SkipList, morse.ProSignTable)
		rawInput = parser.ApplySkip(rawInput)
	}

	fullTextToPlay = colorTagRegex.ReplaceAllString(rawInput, "")

	actualText = ""
	inputArea.SetText("", false)

	parsedText := parser.CleanText(fullTextToPlay, morse.MorseTable)

	parsedText = parser.CompressSpace(parsedText)

	var finalBuilder strings.Builder

	if config.User.StartMsg && config.User.StartMsgText != "" {
		cleanStart := strings.TrimSpace(parser.CleanText(config.User.StartMsgText, morse.MorseTable))
		if cleanStart != "" && !strings.HasPrefix(parsedText, cleanStart) {
			finalBuilder.WriteString(cleanStart)
			finalBuilder.WriteString(" ")
		}
	}

	finalBuilder.WriteString(parsedText)

	if config.User.EndMsg && config.User.EndMsgText != "" {
		cleanEnd := strings.TrimSpace(parser.CleanText(config.User.EndMsgText, morse.MorseTable))
		if cleanEnd != "" && !strings.HasSuffix(parsedText, cleanEnd) {
			finalBuilder.WriteString(" ")
			finalBuilder.WriteString(cleanEnd)
		}
	}

	parsedText = parser.CompressSpace(finalBuilder.String())

	if config.User.StartDelay > 0 {
		go func() {
			for i := config.User.StartDelay; i > 0; i-- {
				if morse.IsStopping {
					return
				}
				app.QueueUpdateDraw(func() {
					msg := fmt.Sprintf("\n\n\n     Starting in %d...", i)
					inputArea.SetText(msg, false)
					statusLine.SetText(" [#00FF00]Preparing to Play...")
				})
				time.Sleep(1 * time.Second)
			}
			app.QueueUpdateDraw(func() {
				inputArea.SetText("", false)
				currentState = StatePlaying
				refreshUI(StatePlaying)
			})

			morse.StartTime = time.Now()
			runEngine(parsedText, iwrMan)
		}()
	} else {
		currentState = StatePlaying
		refreshUI(StatePlaying)

		morse.StartTime = time.Now()
		go runEngine(parsedText, iwrMan)
	}
}

func showIWRWelcomeModal() {
	var globalPath string
	if docDir, err := os.UserConfigDir(); err == nil {
		globalPath = filepath.Join(docDir, "YAMA", "yamaIWR.txt")
	} else {
		globalPath = fmt.Sprintf("%s/yamaIWR.txt", os.UserConfigDir)
	}

	welcomeText := fmt.Sprintf(`[yellow]Welcome to YAMA[-]

IWR - Instant Word Recognition, is a key feature. Options to use it are on the Timing screen (see Help). We will setup an initial file to match IWR words. You can edit as you like (See Timing).

[green]FILE PRIORITY & LOCATIONS:[-]
[white]1. Local (Highest Priority):[-] Yama checks the folder it was launched from for a [blue]yamaIWR.txt[-]. This allows a local working, for easy access.
[white]2. Global Default:[-] If no local file is found, YAMA uses the default located at:
   [blue]%s[-]

[green]FORMATTING TIPS:[-]
• Add words or ProSigns (like <BT>) one per line.
• An asterisk (*) at the end of a word is for wildcard matching. Matching the exact word, or the word followed by: ",.?:".
• Lines starting with '#' are ignored.

You can edit the active file by clicking [yellow]"Edit IWR"[-] in the Timing menu. The editor is simple: cursor, backspace, delete, enter; TAB to access buttons.`, globalPath)

	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetWordWrap(true).
		SetText(welcomeText)
	textView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	form := tview.NewForm().
		AddButton("Close (ESC)", func() {
			pages.RemovePage("iwrwelcome")
			app.SetFocus(inputArea)
		})
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(textView, 0, 1, false).
		AddItem(form, 3, 1, true)

	layout.SetBorder(true).SetTitle(" IWR Initialization ")
	layout.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	pages.AddPage("iwrwelcome", createModal(layout, 85, 26), true, true)
	app.SetFocus(layout)
}

func showWaveModal(targetDir string) {
	rawText := inputArea.GetText()
	words := strings.Fields(rawText)
	wordCount := len(words)

	if wordCount == 0 {
		return
	}

	if wordCount > MaxExportWords {
		errorMsg := fmt.Sprintf("Export Limit Reached!\n\nYour text is %d words.\nTo prevent disk space exhaustion, the WAV exporter is limited to %d words at a time.\n\nPlease chunk your text into smaller files.", wordCount, MaxExportWords)

		errorModal := tview.NewModal().
			SetText(errorMsg).
			AddButtons([]string{"OK"}).
			SetDoneFunc(func(buttonIndex int, buttonLabel string) {
				pages.RemovePage("wavError")
				app.SetFocus(inputArea)
			})

		pages.AddPage("wavError", errorModal, true, true)
		app.SetFocus(errorModal)
		return
	}

	baseInputFile := filepath.Base(currentInputFile)
	if baseInputFile == "." || baseInputFile == "" {
		baseInputFile = "export"
	}
	cleanName := strings.TrimSuffix(baseInputFile, filepath.Ext(baseInputFile))
	filePrefix := "yama_" + cleanName

	estimatedTotalSeconds := len(rawText) / 2
	var wordsPer10Mins int
	if estimatedTotalSeconds > 0 {
		wordsPer10Mins = int((float64(wordCount) / float64(estimatedTotalSeconds)) * 600.0)
	} else {
		wordsPer10Mins = wordCount
	}
	if wordsPer10Mins < 1 {
		wordsPer10Mins = 1
	}

	maxPossibleFiles := (wordCount / wordsPer10Mins) + 1

	mbPerFile := float64(600*11025) / (1024.0 * 1024.0)

	infoTextView := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	infoTextView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	updateSizeLabel := func(numFilesStr string) {
		numFiles, err := strconv.Atoi(numFilesStr)
		if err != nil || numFiles < 1 {
			numFiles = 1
		}

		totalMB := mbPerFile * float64(numFiles)
		msg := fmt.Sprintf("\n [yellow]Generating up to %d files (Max 10 mins each)\nMaximum Output: ~%.1f MB | (~%.1f MB max per file)[-]\n[gray]*Note: Actual size will be smaller if your text doesn't fill the block.[-]", numFiles, totalMB, mbPerFile)
		infoTextView.SetText(msg)
	}

	updateSizeLabel(fmt.Sprintf("%d", maxPossibleFiles))

	form := tview.NewForm()

	prefixInput := tview.NewInputField().SetLabel("File Prefix:").SetText(filePrefix).SetFieldWidth(40)
	prefixInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	numFilesInput := tview.NewInputField().
		SetLabel("Num Files:").
		SetText(fmt.Sprintf("%d", maxPossibleFiles)).
		SetFieldWidth(10).
		SetAcceptanceFunc(tview.InputFieldInteger).
		SetChangedFunc(updateSizeLabel)
	numFilesInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	form.AddFormItem(prefixInput)
	form.AddFormItem(numFilesInput)

	saveFunc := func() {
		prefix := prefixInput.GetText()
		numFilesStr := numFilesInput.GetText()
		numFiles, _ := strconv.Atoi(numFilesStr)
		if numFiles < 1 {
			numFiles = 1
		}

		pages.RemovePage("waveConfig")
		statusLine.SetText(" [yellow]Generating WAV files, please wait...")

		go func() {
			generatedNames, err := morse.ExportWAVBatch(rawText, targetDir, prefix, wordsPer10Mins, numFiles)

			app.QueueUpdateDraw(func() {
				if err != nil {
					statusLine.SetText(" [red]Export failed: " + err.Error())
					app.SetFocus(inputArea)
					return
				}
				statusLine.SetText(" [#00FF00]WAV files generated successfully!")
				showSuccessModal(targetDir, generatedNames)
			})
		}()
	}

	cancelFunc := func() {
		pages.RemovePage("waveConfig")
		app.SetFocus(inputArea)
	}

	form.AddButton("Save", saveFunc).AddButton("Cancel", cancelFunc)

	applyFocusStyles(form)
	form.SetBorder(false)
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	container := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(infoTextView, 5, 1, false).
		AddItem(form, 0, 1, true)

	container.SetBorder(true).SetTitle(" Generate Wave Files (ESC: Cancel) ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	layout := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(container, 14, 1, true).
			AddItem(nil, 0, 4, false),
			65, 1, true).
		AddItem(nil, 0, 1, false)

	pages.AddPage("waveConfig", layout, true, true)
	app.SetFocus(container)
}

func showSuccessModal(dir string, files []string) {
	// Using strings.Builder for clean formatting
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[white]Saved to:\n[#00BFFF]%s[-]\n", dir))
	sb.WriteString(strings.Repeat("-", 46) + "\n")

	for _, f := range files {
		sb.WriteString(fmt.Sprintf("[white]- %s\n", f))
	}
	sb.WriteString("\n[yellow](Press ESC or Enter to close)[-]")

	// Use a TextView instead of a List so long directory paths wrap!
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetWrap(true).
		SetWordWrap(true).
		SetText(sb.String())

	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor)).
		SetBorder(true).
		SetTitle(" Files Generated Successfully ")

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape || event.Key() == tcell.KeyEnter {
			pages.RemovePage("success")
			app.SetFocus(inputArea)
			return nil
		}
		return event
	})

	// Dynamically size the box based on how many files were created
	boxHeight := len(files) + 10
	if boxHeight > 22 {
		boxHeight = 22 // Cap the height so it doesn't blow past the screen
	}

	pages.AddPage("success", createModal(tv, 55, boxHeight), true, true)
	app.SetFocus(tv)
}

func createDefaultIWRFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("Fatal error creating IWR directory: %v", err)
		return err
	}

	defaultContent := []byte("<BT>\n<AR>\n<SK>\n")
	return os.WriteFile(path, defaultContent, 0644)
}

