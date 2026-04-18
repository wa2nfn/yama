


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
	// 1. SAFETY LOCK: Prevent playback/interaction if the audio engine is dead
	if morse.AudioHardwareDead {
		statusLine.SetText(" [red::b]FATAL: Audio hardware lost. Restart app.[::-]")
		return
	}

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
				statusLine.SetText(" [red::b]Engine Crashed![::-]")
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

	// Ensure a manual stop doesn't overwrite a hardware death warning
	if morse.AudioHardwareDead {
		statusLine.SetText(" [red::b]ERROR: Audio Device Disconnected! Restart App.[::-]")
	} else {
		statusLine.SetText(" [#00FF00]Stopped")
	}

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

		// INTERCEPT STATUS: If the watchdog aborted the run, warn the user!
		if morse.AudioHardwareDead {
			statusLine.SetText(" [red::b]ERROR: Audio Device Disconnected! Restart App.[::-]")
		} else {
			statusLine.SetText(" [#00FF00]" + status)
		}

		menu = "[#00FF00]F[white]ile  [#00FF00]P[white]lay  [#00FF00]T[white]iming  [#00FF00]A[white]udio  [#00FF00]O[white]ption  [#00FF00]H[white]elp  [#00FF00]Q[white]uit a[#00FF00]B[white]out"

		if hasText {
			menu = strings.Replace(menu, "[#00FF00]P[white]lay", "[#00FF00]P[white]lay  [#00FF00]W[white]ave  [#00FF00]E[white]rase", 1)
		}

		if statsTotalWords > 0 {
			menu = strings.Replace(menu, "[#00FF00]O[white]ption", "[#00FF00]O[white]ption  [#00FF00]D[white]ata-Stats", 1)
		}

	case StatePlaying:
		statusLine.SetText(" [#00FF00]Playing")
		menu = "[#00FF00]P[white]ause  [#00FF00]S[white]top [#00FF00]A[white]udio"
	case StatePaused:
		statusLine.SetText(" [#00FF00]Paused")
		menu = "[#00FF00]R[white]esume  [#00FF00]S[white]top  [#00FF00]T[white]iming  [#00FF00]A[white]udio"
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
	msg := "[yellow::b]Configuration Adjustments ReqGuired:[::-]\n\n"
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
                         [yellow::b]Welcome to YAMA - Yet Another Morse App[::-]

Whether you are looking for routine practice, some head copy or want to test your copying limits against a simulated ionospheric storm, YAMA is built to help you.

[green::b]Getting Started: Entering Text[::-]
[white]Before YAMA can play anything, it needs some text! You have two easy ways to do this:
1. Type or Paste: Simply click into the main Text Input box and type or paste your practice text directly. (cursor keys, backspace, delete are supported for editing)
2. Load a File: Press [yellow]Ctrl-F[-] to open the File Selector and browse for any standard '.txt' file on your computer.

[green::b]Dynamic Menus & Navigation[::-]
[white]YAMA is operated entirely via keyboard shortcuts. Keep an eye on the top menu bar — it is dynamic. YAMA will only show you the shortcuts that make sense for what you are currently doing. For example, you cannot open the Options menu while audio is actively playing, and therefore there will not be an Open label and [yellow]Ctrl-O[-] will be ignored, Wave export shortcut will only appear when you actually have text loaded to export.

If you ever get stuck in a menu, just press [yellow]ESC[-] to safely close it without saving. [red](Note: insert or removal of headphones can trigger a Windows hang of the app requiring an app restart.)[::-]

[white]Ctrl Key | Menu Name  | Purpose[-]
---------|------------|--------------------------------------------------------
Ctrl-F   | File       | Open a .txt file for playback
Ctrl-P   | Play/Pause | Start or pause the current loaded input text
Ctrl-S   | Stop       | Halt playback immediately (cannot be resumed)
Ctrl-W   | Wave       | Export current text to .wav file(s)
Ctrl-E   | Erase      | Clear the current text input aka screen clear
Ctrl-T   | Timing     | Speed, Tone, and IWR settings
Ctrl-O   | Option     | Parser, messaging, and text processing options
Ctrl-A   | Audio      | Audio impacting impairements (QRN, QSB, Drift, etc.)
Ctrl-D   | Data-Stats | View session statistics and IWR counts
Ctrl-B   | aBout      | App info and License
Ctrl-H   | Help       | This screen
Ctrl-Q   | Quit       | Exit YAMA
ESC      | Close      | Cancel/Close menus without saving
Spacebar | Hide/Unhide| Toggle text visibility during playback

[green::b]Supported Characters & Punctuation[::-]
[white]YAMA naturally supports standard letters [yellow]A-Z[-] and numbers [yellow]0-9[-].

[white]Basic punctuation: [yellow]. , ? /[-]
[white]Full punctuation (Enable in Options): [yellow]: ; " @ '[-]

[green::b]ProSigns & Equivalents[::-]
[white]Supported ProSigns: <AR> <AS> <BT> <KA> <SK> <VA> <VE> <SN> <BK> <HH> <DU> <SOS> <CH>.
If "Use Prosigns" is disabled in Options, bracketed ProSigns will be ignored. However, the standard keyboard equivalents [yellow]+[-] (<AR>), [yellow]=[-] (<BT>), and [yellow]-[-] (<DU>) will still play, unless added to the Skip List in the Options screen. (Note, any other use of '<' or '>' is ignored.)

[green::b]Option Screen (Ctrl-O) - Settings[::-]
[white]Setting               | Description
----------------------|---------------------------------------------------------
Use Prosigns          | Toggles support for bracketed ProSigns (e.g., <AR>).
                      | Does NOT effect [yellow]-+=[-]. (Note: <BK> is sounded as  "B K").
All Punctuation       | Toggles support for extended punctuation marks.
Use Skip              | Enables the Skip List filtering during playback.
Skip List             | Define specific characters or ProSigns to silently ignore.
                      | Entered without any separators. e.g. XY7<BT>=
Start Delay           | Adds a countdown timer (in seconds) before playback begins.
Repeat Limit          | Caps consecutive repeating characters to prevent runaway sequences .
                      | sometimes used in books under titles. Default 3 or many words effected.
Random Order          | Shuffles the playback order of the entire document's words.
Random Words          | Scrambles the letters within individual words. e.g. a code group
                      | Mutually exclusive with the IWR function.
Word Builder          | Plays words progressively (e.g., T, TH, THE) for comprehension.
                      | Mutually exclusive with IWR. IWR speed is used to sound the last word.
Start Msg             | Toggles injecting a custom message at the beginning of the text.
Start Msg Text        | The specific text to play at the start (e.g., VVV <KA>).
End Msg               | Toggles injecting a custom message at the end of the text.
End Msg Text          | The specific text to play at the end (e.g., <AR>).

[green::b]The Skip List & Contractions[::-]
[white]You can define specific characters or ProSigns to silently skip during playback (Options -> Skip List).
[yellow]Important Apostrophe Rule:[-] If you add the apostrophe [yellow](')[-] to your skip list, YAMA will automatically expand 17 common English contractions before removing the remaining apostrophes (e.g., "DON'T" safely becomes "DO NOT").

[green::b]Audio Screen (Ctrl-A) - Audio Impairements[::-]
[white]Setting               | Description
----------------------|---------------------------------------------------------
Static (QRN)          | Injects constant background hiss and random lightning crashes.
Fading (QSB)          | Simulates a slow ionospheric roll, dipping and recovering volume.
Tone Drift            | Simulates an unstable oscillator, bending the pitch up and down.
Speed Drift           | Simulates a tired operator by slowly expanding/contracting timing.
Key Clicks            | Injects a harsh electrical spark at the start and end of elements.

[green::b]WAV File Export (Ctrl-W)[::-]
[white]YAMA exports 8-bit Mono audio. To prevent disk exhaustion, exports are capped at 5,000 words and automatically chunked into sequential files of roughly 10 minutes each.

[green::b]IWR Feature [::-]
[white]This a a head copy related feature. I looks to match words (actually any space separated string of suppored characters e.g. the qsl 73 cul) for the input, and override the chosen timing mode (standard, Farnsworth, Wordsworth and the associated speed/tone) and play the matched word at a increased speed with standard timing. To do this you must create an [blue]yamaIWR.txt[-] file in the current directory (or by default in your OS's standard configuration directory (for windows it will be $HOME\AppData\Roaming\YAMA). The file lists one word per line (any case, any order); a [yellow]'#'[-] at the start of line tells YAMA to ignore that line. If you choose to also match the word if its immediately follow by [yellow], . ? : [-] as well as the bare word, this is indicated by a trailing asterisk (e.g. qsl* matches: qsl qsl? qsl. qsl: qsl, ). The IWR feature as described is ignored if you have choosen either WordBuilder or RandomWord in the Options menu.
You can create or edit this file with any text editor of your choice, or the simple edit functions from the Timing [yellow](Ctrl-T)[-] screen with the other IWR options.

[green::b]System Files (Misc)[::-]
• [blue]yama_config.json:[-] Automatically manages your saved settings. Please use the UI menus rather than hand-editing this file.
• [blue]yamaIWR.txt:[-] Your active dictionary for Instant Word Recognition. Edit this safely via the "Edit IWR" button in the Timing menu. Note: Edit via the app are rather simple, cursor, delete, backspace, TAB to access Save button.
• [blue]yamaHELP.txt:[-] This file if Export To File is used.
[::-].`
	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText).
		SetScrollable(true)

	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	// 1. BUILD THE FORM FIRST
	form := tview.NewForm().
		AddButton("Export to yama_help.txt", func() {
			cleanText := colorTagRegex.ReplaceAllString(helpText, "")

			// APPLY RESOLVER
			exportPath := ResolvePath("yama_help.txt")

			err := os.WriteFile(exportPath, []byte(cleanText), 0644)
			if err == nil {
				statusLine.SetText(" [#00FF00]Help manual exported to " + exportPath + "![-]")
			} else {
				statusLine.SetText(" [red]Failed to export help file.[-]")
			}
			pages.RemovePage("help")
			app.SetFocus(inputArea)
		}).
		AddButton("ESC to Close", func() {
			pages.RemovePage("help")
			app.SetFocus(inputArea)
		})

	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	applyFocusStyles(form)

	// The new static hint text
	hint := tview.NewTextView().
		SetText(" (Cursor Up/Dn as needed) ").
		SetTextColor(tcell.ColorYellow).
		SetTextAlign(tview.AlignCenter)

	hint.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	// 2. NOW WE CAN INTERCEPT TAB (Because 'form' actually exists!)
	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			app.SetFocus(form) // Jump down to the buttons!
			return nil
		}
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("help")
			app.SetFocus(inputArea)
			return nil
		}
		return event
	})

	// 3. BUILD THE LAYOUT
	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tv, 0, 1, true).
		AddItem(form, 3, 1, false).
		AddItem(hint, 1, 1, false) // <-- Inserted the hint here! (1 row tall)

	layout.SetBorder(true).SetTitle(" Help Information ")
	layout.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	// 3. BUILD THE LAYOUT

	pages.AddPage("help", createModal(layout, 85, 26), true, true)
	app.SetFocus(layout)
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

	pages.AddPage("about", createModal(tv, 90, 26), true, true)
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

					// 1. The new typing listener (Fixed to match the flawless one in main.go)
					inputArea.SetChangedFunc(func() {
						if currentState == StateStopped || currentState == StatePaused {
							currentState = StateIdle
						}
						refreshUI(currentState) // <-- OUTSIDE the if block!
					})

					// 2. The immediate update for loading the file
					if currentState == StateStopped || currentState == StatePaused {
						currentState = StateIdle
					}
					refreshUI(currentState) // <-- OUTSIDE the if block!
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
	startMsgInput := tview.NewInputField().SetLabel("Start Msg Text").SetFieldWidth(30)
	startMsgInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)
	startMsgInput.SetPlaceholder(" e.g. VVV <KA>").SetPlaceholderTextColor(tcell.ColorYellow)

	endMsgCb := tview.NewCheckbox().SetLabel("End Msg")
	endMsgInput := tview.NewInputField().SetLabel("End Msg Text").SetFieldWidth(30)
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

	// APPLY RESOLVER
	localPath := ResolvePath("yamaIWR.txt")

	if _, err := os.Stat(localPath); err == nil {
		filePath = localPath
	} else {
		if docDir, errDir := os.UserConfigDir(); errDir == nil {
			// APPLY RESOLVER
			filePath = ResolvePath(filepath.Join(docDir, "YAMA", "yamaIWR.txt"))
		} else {
			filePath = localPath // Fallback
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

		if iwrCheckbox.IsChecked() {
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

	// 1. SAFETY LOCK: Prevent playback if the audio engine is dead
	if morse.AudioHardwareDead {
		statusLine.SetText(" [red::b]FATAL: Audio hardware lost. Restart app.[::-]")
		return
	}

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

	// APPLY RESOLVER
	if docDir, err := os.UserConfigDir(); err == nil {
		globalPath = ResolvePath(filepath.Join(docDir, "YAMA", "yamaIWR.txt"))
	} else {
		globalPath = ResolvePath("./yamaIWR.txt")
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

You can edit the active file by clicking [yellow]"Edit IWR"[-] in the Timing menu. The editor is simple: cursor, backspace, delete, enter; TAB to access buttons.

Note: This text will NOT be shown again.`, globalPath)

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

func showSuccessModal(dir string, files []string) {
	// Using strings.Builder for clean formatting
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[white]Saved to:\n[#00BFFF]%s[-]\n", dir))
	sb.WriteString(strings.Repeat("-", 46) + "\n")

	for _, f := range files {
		sb.WriteString(fmt.Sprintf("[white]- %s\n", filepath.Base(f)))
	}
	sb.WriteString("\n[yellow]ESC to close[-]")

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

// by Ctrl-A for audio
func showImpairments() {
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)
	form.SetItemPadding(0)

	// The 4 levels for our dropdowns
	levels := []string{"Off", "Light", "Heavy", "Severe"}

	// Create the DropDowns for the level-based impairments
	staticDropDown := tview.NewDropDown().SetLabel("Static (QRN)").SetOptions(levels, nil)
	fadingDropDown := tview.NewDropDown().SetLabel("Fading (QSB)").SetOptions(levels, nil)
	toneDriftDropDown := tview.NewDropDown().SetLabel("Tone Drift").SetOptions(levels, nil)
	speedDriftDropDown := tview.NewDropDown().SetLabel("Speed Drift").SetOptions(levels, nil)

	// Key Clicks remains a standard boolean checkbox
	keyClickCb := tview.NewCheckbox().SetLabel("Key Clicks")

	// Set their initial states based on your Config
	resetState := func() {
		staticDropDown.SetCurrentOption(config.User.NoiseStaticLevel)
		fadingDropDown.SetCurrentOption(config.User.NoiseFadingLevel)
		toneDriftDropDown.SetCurrentOption(config.User.NoiseToneDriftLevel)
		speedDriftDropDown.SetCurrentOption(config.User.NoiseSpeedDriftLevel)
		keyClickCb.SetChecked(config.User.NoiseKeyClick)
	}

	resetState()

	// Add them to the form
	form.AddFormItem(staticDropDown)
	form.AddFormItem(fadingDropDown)
	form.AddFormItem(toneDriftDropDown)
	form.AddFormItem(speedDriftDropDown)
	form.AddFormItem(keyClickCb)

	onSave := func() {
		// Save the dropdown levels to the global config
		config.User.NoiseStaticLevel, _ = staticDropDown.GetCurrentOption()
		config.User.NoiseFadingLevel, _ = fadingDropDown.GetCurrentOption()
		config.User.NoiseToneDriftLevel, _ = toneDriftDropDown.GetCurrentOption()
		config.User.NoiseSpeedDriftLevel, _ = speedDriftDropDown.GetCurrentOption()

		config.User.NoiseKeyClick = keyClickCb.IsChecked()

		config.SaveConfig()

		pages.RemovePage("impairments")
		app.SetFocus(inputArea)
	}

	onReset := func() {
		resetState()
	}

	// It forces all dropdowns back to Option 0 ("Off") and unchecks the box
	onClearAll := func() {
		// 1. Reset the UI elements so the user sees the change
		staticDropDown.SetCurrentOption(0)
		fadingDropDown.SetCurrentOption(0)
		toneDriftDropDown.SetCurrentOption(0)
		speedDriftDropDown.SetCurrentOption(0)
		keyClickCb.SetChecked(false)

		// 2. Immediately update the global config memory
		config.User.NoiseStaticLevel = 0
		config.User.NoiseFadingLevel = 0
		config.User.NoiseToneDriftLevel = 0
		config.User.NoiseSpeedDriftLevel = 0
		config.User.NoiseKeyClick = false

		// 3. Commit it to disk instantly!
		config.SaveConfig()
	}

	// 2. ADD THE BUTTON TO THE FORM
	form.AddButton("Save", onSave)
	form.AddButton("Reset", onReset)
	form.AddButton("Clear All", onClearAll) // <-- Your new panic button!

	applyFocusStyles(form)
	form.SetBorder(false)

	footerView := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetDynamicColors(true)
	footerView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	footerView.SetText("\n[yellow]ESC to Close[-]\n")

	container := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true).
		AddItem(footerView, 2, 1, false)

	container.SetBorder(true).SetTitle(" Audio Impairments ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	pages.AddPage("impairments", createModal(container, 45, 17), true, true)
	app.SetFocus(container)
}

func showWaveModal(targetDir string) {
	rawText := inputArea.GetText()

	rawText = strings.ToUpper(rawText)

	if config.User.UseSkip {
		parser.SetSkipList(config.User.SkipList, morse.ProSignTable)
		rawText = parser.ApplySkip(rawText)
	}

	rawText = parser.CleanText(rawText, morse.MorseTable)

	words := strings.Fields(rawText)
	wordCount := len(words)

	if wordCount == 0 {
		return
	}

	baseInputFile := filepath.Base(currentInputFile)
	if baseInputFile == "." || baseInputFile == "" {
		baseInputFile = "export"
	}
	cleanName := strings.TrimSuffix(baseInputFile, filepath.Ext(baseInputFile))
	filePrefix := "yama_" + cleanName

	infoTextView := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter)
	infoTextView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	form := tview.NewForm()

	dirInput := tview.NewInputField().
		SetLabel("Save Directory:").
		SetText(targetDir).
		SetFieldWidth(40)
	dirInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	prefixInput := tview.NewInputField().SetLabel("File Prefix:").SetText(filePrefix).SetFieldWidth(40)
	prefixInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	// --- THIS IS THE NEW FIELD THAT WAS MISSING! ---
	minsInput := tview.NewInputField().
		SetLabel("Minutes/File:").
		SetText("10").
		SetFieldWidth(10).
		SetAcceptanceFunc(func(textToCheck string, lastChar rune) bool {
			if textToCheck == "" {
				return true
			}
			val, err := strconv.Atoi(textToCheck)
			return err == nil && val >= 1 && val <= 60
		})
	minsInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	numFilesInput := tview.NewInputField().
		SetLabel("Number Of Files:").
		SetFieldWidth(10).
		SetAcceptanceFunc(tview.InputFieldInteger)
	numFilesInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	updateCalculations := func() {
		minsStr := minsInput.GetText()
		mins, err := strconv.Atoi(minsStr)
		if err != nil || mins < 1 {
			mins = 10
		}

		estimatedTotalSeconds := len(rawText) / 2
		targetSeconds := mins * 60

		var wordsPerBlock int
		if estimatedTotalSeconds > 0 {
			wordsPerBlock = int((float64(wordCount) / float64(estimatedTotalSeconds)) * float64(targetSeconds))
		} else {
			wordsPerBlock = wordCount
		}
		if wordsPerBlock < 1 {
			wordsPerBlock = 1
		}

		maxPossibleFiles := (wordCount / wordsPerBlock) + 1

		numFilesStr := numFilesInput.GetText()
		numFiles, err := strconv.Atoi(numFilesStr)
		if err != nil || numFiles < 1 {
			numFiles = maxPossibleFiles
		}

		mbPerFile := float64(targetSeconds*11025*2) / (1024.0 * 1024.0)
		totalMB := mbPerFile * float64(numFiles)

		msg := fmt.Sprintf("\n [yellow]Generating up to %d files (Max %d mins each)\n" +
			"Maximum Output: ~%.1f MB | (~%.1f MB max per file)[-]\n" +
			"[gray]*Note: Actual size will be smaller if your text doesn't fill the block.[-]",
			numFiles, mins, totalMB, mbPerFile)

		infoTextView.SetText(msg)
	}

	minsInput.SetChangedFunc(func(text string) {
		mins, err := strconv.Atoi(text)
		if err == nil && mins >= 1 {
			estimatedTotalSeconds := len(rawText) / 2
			targetSeconds := mins * 60
			var wordsPerBlock int
			if estimatedTotalSeconds > 0 {
				wordsPerBlock = int((float64(wordCount) / float64(estimatedTotalSeconds)) * float64(targetSeconds))
			} else {
				wordsPerBlock = wordCount
			}
			if wordsPerBlock < 1 {
				wordsPerBlock = 1
			}

			maxPossibleFiles := (wordCount / wordsPerBlock) + 1
			numFilesInput.SetText(fmt.Sprintf("%d", maxPossibleFiles))
		}
		updateCalculations()
	})

	numFilesInput.SetChangedFunc(func(text string) {
		updateCalculations()
	})

	form.AddFormItem(dirInput)
	form.AddFormItem(prefixInput)
	form.AddFormItem(minsInput)
	form.AddFormItem(numFilesInput)

	updateCalculations()
	saveFunc := func() {
		exportDir := ResolvePath(strings.TrimSpace(dirInput.GetText()))
		prefix := strings.TrimSpace(prefixInput.GetText())

		numFilesStr := numFilesInput.GetText()
		numFiles, _ := strconv.Atoi(numFilesStr)
		if numFiles < 1 {
			numFiles = 1
		}

		minsStr := minsInput.GetText()
		mins, err := strconv.Atoi(minsStr)
		if err != nil || mins < 1 {
			mins = 10
		}

		estimatedTotalSeconds := len(rawText) / 2
		targetSeconds := mins * 60
		var finalWordsPerBlock int
		if estimatedTotalSeconds > 0 {
			finalWordsPerBlock = int((float64(wordCount) / float64(estimatedTotalSeconds)) * float64(targetSeconds))
		} else {
			finalWordsPerBlock = wordCount
		}
		if finalWordsPerBlock < 1 {
			finalWordsPerBlock = 1
		}

		if err := os.MkdirAll(exportDir, 0755); err != nil {
			statusLine.SetText(" [red]Error: Cannot create save directory![-]")
			return
		}

		pages.RemovePage("waveConfig")
		statusLine.SetText(" [yellow]Generating WAV files, please wait...")

		go func() {
			generatedNames, err := morse.ExportWAVBatch(rawText, exportDir, prefix, finalWordsPerBlock, numFiles)

			app.QueueUpdateDraw(func() {
				if err != nil {
					statusLine.SetText(" [red]Export failed: " + err.Error())
					app.SetFocus(inputArea)
					return
				}
				statusLine.SetText(" [#00FF00]WAV files generated successfully!")
				showSuccessModal(exportDir, generatedNames)
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

	container.SetBorder(true).SetTitle(" Generate Wave Files (ESC To Cancel) ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	layout := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(container, 22, 1, true).
			AddItem(nil, 0, 4, false),
			65, 1, true).
		AddItem(nil, 0, 1, false)

	pages.AddPage("waveConfig", layout, true, true)
	app.SetFocus(container)
}
