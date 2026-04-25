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
	isProgrammaticUpdate = true // Lock the listener

	if isBlocked {
		inputArea.SetText(strings.Repeat("*", len(actualText)), false)
	} else {
		inputArea.SetText(actualText, false)
	}

	isProgrammaticUpdate = false // Unlock it
}

func updateBlueLine() {

	iwrStatus := "OFF"
	if config.User.IWREnabled {
		iwrStatus = "ON"
	}

	var info string
	if config.User.CharacterSpeed <= config.User.EffectiveSpeed {
		info = fmt.Sprintf(" [black]Character Speed: %d wpm | IWR: %s (%d wpm) ", config.User.CharacterSpeed, iwrStatus, config.User.IWRSpeed)
	} else {
		mode := "Farnsworth"
		if config.User.UseWordsworth {
			mode = "Wordsworth"
		}
		info = fmt.Sprintf(" [black]Mode: %s | Character Speed: %d wpm | Effective Speed: %d wpm | IWR: %s (%d wpm) ", mode, config.User.CharacterSpeed, config.User.EffectiveSpeed, iwrStatus, config.User.IWRSpeed)
	}
	
	// Stick the debug numbers right at the front
	blueLine.SetText(info + getModifierWarning())
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

Whether you are looking for routine practice, some head copy, or want to test your copying limits against a simulated ionospheric storm, YAMA is built to help you.

[red]A note about access to Help. Using the function key F1 works for Windows or Linux; Ctrl-H will work if you have not typed text in the Text Input screen, if you have the OS interprets it as a Backspace (legacy Linux issue), the status line will give you a hint.[-]

[green::b]Getting Started: Entering Text[::-]
[white]Before YAMA can play anything, it needs some text! You have two easy ways to do this:
1. Type or Paste: Simply click into the main Text Input box and type or paste your practice text directly. Cursor keys, Backspace, Delete are supported for editing)
2. Load a File: Press [yellow]Ctrl-F[-] (note: Ctrl-F, for example means hold the Control Key and simultaneously the 'f' key) to open the File Selector and browse for any standard '.txt' file on your computer.

[green::b]Dynamic Menus & Navigation[::-]
[white]YAMA is operated entirely via keyboard shortcuts. Keep an eye on the top menu bar — it is dynamic. YAMA will only show you the shortcuts that make sense for what you are currently doing. For example, you cannot open the Options menu while audio is actively playing, therefore there will not be an Options label and [yellow]Ctrl-O[-] will be ignored, Wave export label will only appear when you actually have text loaded to export.

The method to enter or change an option on the Ctrl-O or Ctrl-T screen will depend on the type of option it is. First navigate to the option of interest using the TAB or BACKTAB, then if the option is a single-charater field like Use ... or Random Order simply hit Enter or space to toggle the option (then TAB forward); if the option shows a single digit or the name of a timing mode (i.e. Farnsworth) the choices are provided by a drop down, use the cursor and hit Enter; multi digit options like tones or speeds, use Backspace, type new value and hit Enter or TAB; input text boxes, use Backspace, enter a value and hit Enter or TAB. 

If you ever get stuck in a menu, just press [yellow]ESC[-] to safely close it without saving. [red](Note: insertion or removal of headphones can trigger a Windows hang of the Yama app requiring a restart.)[::-]

[white]Key           | Menu Name  | Purpose[-]
--------------|------------|--------------------------------------------------------
Ctrl-F, Fn f3 | File       | Open a .txt file for playback
Ctrl-P        | Play/Pause | Start or pause the current loaded input text
Ctrl-S        | Stop       | Halt playback immediately (cannot be resumed)
Ctrl-W        | Wave       | Export current text to .wav file(s)
Ctrl-E        | Erase      | Clear the current text input aka screen clear
Ctrl-T        | Timing     | Speed, Tone, and IWR settings
Ctrl-O        | Options    | Parser, messaging, and text processing options
Ctrl-A        | Audio      | Audio impacting impairements (QRN, QSB, Drift, etc.)
Ctrl-D        | Data-Stats | View statistics and IWR counts. Use when play stops,
                           | any new input clears the old data.
CtrlB         | aBout      | App info and License
Ctrl-H, Fn f1 | Help       | This screen text.
Ctrl-Q        | Quit       | Exit YAMA. 
ESC           | Close      | Cancel/Close menus without saving
Spacebar      | Hide/Unhide| Toggle text visibility during audio playback.

[green::b]Supported Characters & Punctuation[::-]
[white]YAMA naturally supports standard letters [yellow]A-Z[-] and numbers [yellow]0-9[-].

[white]Basic punctuation: [yellow]. , ? /[-]
[white]Extended punctuation (Enable in Options): [yellow]: ; " @ ' ( ) $ ! \ [-]

[green::b]ProSigns & Equivalents[::-]
[white]Supported ProSigns:[yellow] <AR> <AS> <BT> <KA> <SK> <VA> <VE> <SN> <BK> <HH> <DU> <SOS> <CH>[-].

If "Play ProSigns" is disabled in Options ([yellow]Ctrl-O[-]), bracketed ProSigns will be ignored (including their use in the Start/End Msg). However, the standard keyboard equivalents [yellow]+[-] (<AR>), [yellow]=[-] (<BT>), and [yellow]-[-] (<DU>) will still play, unless added to the Skip List in the Options screen. (Note: any other use of '<' or '>' is ignored.)

[green::b]Options Screen (Ctrl-O) - Settings[::-]
[white]Setting               | Description
----------------------|---------------------------------------------------------
Play ProSigns         | Toggles support for bracketed ProSigns (e.g., <AR>).
                      | Does NOT affect [yellow]-+=[-]. (Note: <BK> is sounded as  "B K").
Extended Punctuation  | Toggles support for extended punctuation marks.
Use Skip              | Enables the Skip List filtering during playback.
Skip List             | Define specific characters or ProSigns to silently ignore.
                      | Entered without any separators. e.g. XY7<BT>=
Start Delay           | Adds a countdown timer (in seconds) before playback begins.
Repeat Limit          | Caps consecutive repeating characters to prevent runaway sequences.
                      | sometimes used in books to underline titles. Default 3.
Random Order          | Shuffles the playback order of the entire document's words.
Random Words          | Scrambles the letters within individual words (e.g. a code group).
                      | Mutually exclusive with the IWR function.
Word Builder          | Plays words progressively (e.g., T, TH, THE) for comprehension.
                      | Mutually exclusive with IWR. IWR speed is used to sound the last word.
Use Start Msg         | Toggles injecting a custom message at the beginning of the text.
Start Msg Text        | The specific text to play at the start (e.g., VVV <KA>).
Use End Msg           | Toggles injecting a custom message at the end of the text.
End Msg Text          | The specific text to play at the end (e.g., <AR>).

[green::b]The Skip List & Contractions[::-]
[white]You can define specific characters or ProSigns to silently skip during playback (Options -> Skip List).
[yellow]Important Apostrophe Rule:[-] If you add the apostrophe [yellow](')[-] to your skip list, YAMA will automatically expand 17 common English contractions before removing the remaining apostrophes (e.g., "DON'T" safely becomes "DO NOT").

Note that the bottom of the screen has a [green]green[-] status line and below that a [blue]blue[-] reminder line about your current vales from the Timing screen. It may also the phrase [yellow]Input Modified[-], this indicates that a least one option on the Options screen will modify the input in the text screen, before it becomes audible CW, so you are not surprised when the firt string is sounded and it maybe very different that what was just displayed.

[green::b]Audio Screen (Ctrl-A) - Audio-impacting impairments[::-]
[white]Setting               | Description
----------------------|---------------------------------------------------------
Static (QRN)          | Injects constant background hiss and random lightning crashes.
Fading (QSB)          | Simulates a slow ionospheric roll, dipping and recovering volume.
Tone Drift            | Simulates an unstable oscillator, bending the pitch up and down.
Speed Drift           | Simulates a tired operator by slowly expanding/contracting the timing.
Key Clicks            | Injects a harsh electrical spark at the start and end of elements.

[yellow]Note that you can make changes to the currently playing audio with the Timing or Audio screens.[-]

[green::b]WAV File Export (Ctrl-W)[::-]
[white]YAMA exports 16-bit Mono audio. The wave screen lets you select a target directory for the created wave files, if the path does not exist, it will create it. The approximate play time for the chosen speed and the corresponding size is shown. You can also specify (actually limit) the number of files. If your input is a large novel you can certainly limit output to a handful of practice files. If you are emailing the completed files to yourself so that you can play them on cell phone, then capping the file size near 10Mb should be reasonable.

[green::b]IWR - Instant Word Recognition Feature[::-]
[white]This a a head-copy- related feature. I looks to match words (actually any space separated string of supported characters (e.g. the qsl 73 cul) in the input, and override the chosen timing mode (standard, Farnsworth, Wordsworth and the associated speed/tone) and play the matched word at a increased speed with standard timing. To do this you must create an [blue]yamaIWR.txt[-] file. A sample file has been created in the in your OS's standard configuration file directory ($HOME\AppData\Roaming\YAMA for Windows). That file will be editable from the Timing screen ([yellow]Ctrl-T[-], or you may create another one in the same directory that Yama is launched from, this one will take priority but you will have to edit it with notepad, vi, emacs or what ever your favorite text editor is (not a word processor, unless it has a save as txt option). The file should list one word per line (any case, any order); a [yellow]'#'[-] at the start of line tells YAMA to ignore that line. If you choose to also match the word if its immediately follow by [yellow], . ? : [-] as well as the bare word, this is indicated by a trailing asterisk (e.g. qsl* matches: qsl qsl? qsl. qsl: qsl, ). The IWR feature as described is ignored if you have choosen either WordBuilder or Random Word in the Options menu, since you would never get a match. A small purposeful interaction with IWR speed is as follows: if you chose Word Builder and have IWR enabled, then when word builder has completed constructing a word (as in: t te tes test) you will have one more sounding of the final word, but this time at IWR speed.

Experiment and I'm sure you will quickly understand the capabilities. Remeber, an ESC or two will always get you back to main Text Input screen.

[green::b]System Files (Misc)[::-]
• [blue]yama_config.json:[-] Automatically manages your saved settings. Please use the UI menus rather than hand-editing this file.
• [blue]yamaIWR.txt:[-] Your active dictionary for Instant Word Recognition. Edit this safely via the "Edit IWR" button in the Timing menu. Note: Edits via the app are rather simple, cursor keys, Insert/Delete, Backspace, TAB to access Save button.
• [blue]yama_help.html:[-] This information saved in html format for browser display or printing.
• [blue]YAMA directory:[-] On windows, $HOME/AppData/Roaming/YAMA, on Linux $HOME/.config/YAMA, will contain a file that holds your options and a stating IWR text file, neighter should be edited manually. If you ever discard the YAMA app, remoe this directory as well.

Note: Yama does not require or make any changes to non-YAMA values on your PC. All files related to YAMA have, yama in the name for you to remove as you see fit.

In the Wave creation screen, if you enter a non-existent directory, it will be created - this is recommended to make the eventual removal of WAV files simpler.

73 and best of luck on your CW journey.
WA2NFN
[::-].`

	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText).
		SetScrollable(true)

	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	// 1. BUILD THE FORM FIRST
	/*
		form := tview.NewForm().
			AddButton("Export to yama_help.txt", func() {
				cleanText := colorTagRegex.ReplaceAllString(helpText, "")

				// APPLY RESOLVER
				exportPath := morse.ResolvePath("yama_help.txt")

				err := os.WriteFile(exportPath, []byte(cleanText), 0644)
				if err == nil {
					statusLine.SetText(" [#00FF00]Help manual exported to " + exportPath + "![-]")
				} else {
					statusLine.SetText(" [red]Failed to export help file.[-]")
				}
				pages.RemovePage("help")
				app.SetFocus(inputArea)
			}).

	*/
	form := tview.NewForm().
		AddButton("Save to yama_help.html", func() {

			// 1. ESCAPE THE PROSIGNS! <AR> becomes &lt;AR&gt; so the browser doesn't hide it.
			htmlText := strings.ReplaceAll(helpText, "<", "&lt;")
			htmlText = strings.ReplaceAll(htmlText, ">", "&gt;")

			// 2. Translate tview color tags into HTML CSS spans
			htmlText = strings.ReplaceAll(htmlText, "[yellow::b]", "<span style='color: #FFD700; font-weight: bold;'>")
			htmlText = strings.ReplaceAll(htmlText, "[green::b]", "<span style='color: #00FF00; font-weight: bold;'>")
			htmlText = strings.ReplaceAll(htmlText, "[white]", "<span style='color: white;'>")
			htmlText = strings.ReplaceAll(htmlText, "[yellow]", "<span style='color: #FFD700;'>")
			htmlText = strings.ReplaceAll(htmlText, "[red]", "<span style='color: #FF6666;'>")
			htmlText = strings.ReplaceAll(htmlText, "[blue]", "<span style='color: #00BFFF;'>")
			htmlText = strings.ReplaceAll(htmlText, "[-]", "</span>")
			htmlText = strings.ReplaceAll(htmlText, "[::-]", "</span>")

			// 3. Wrap it in a beautiful, printable HTML document
			finalHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>YAMA Help Manual</title>
    <style>
        body {
            background-color: #1B2B44;
            color: white;
            font-family: 'Consolas', 'Courier New', monospace;
            padding: 40px;
            line-height: 1.5;
        }
        .container {
            max-width: 900px;
            margin: auto;
            background-color: #0A1423;
            padding: 30px;
            border-radius: 8px;
            border: 1px solid #334466;
            box-shadow: 0 4px 15px rgba(0,0,0,0.5);
        }
        pre { white-space: pre-wrap; font-family: inherit; margin: 0; }

        /* This makes sure it looks good if they actually print it to paper */
        @media print {
            body, .container { background-color: white; color: black; box-shadow: none; border: none; padding: 0; }
            span { font-weight: bold !important; }
            span[style*="color: #FFD700"] { color: #8B8B00 !important; } /* Darken yellow for white paper */
            span[style*="color: white"] { color: black !important; }
            span[style*="color: #00FF00"] { color: darkgreen !important; }
            span[style*="color: #00BFFF"] { color: blue !important; }
        }
    </style>
</head>
<body>
    <div class="container">
        <pre>%s</pre>
    </div>
</body>
</html>`, htmlText)

			exportPath := morse.ResolvePath("yama_help.html")

			err := os.WriteFile(exportPath, []byte(finalHTML), 0644)
			if err == nil {
				statusLine.SetText(" [#00FF00]Help manual exported to " + exportPath + "![-]")
			} else {
				statusLine.SetText(" [red]Failed to export HTML file.[-]")
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
		SetText(" (Cursor Up/Down as needed) ").
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

	pages.AddPage("help", createModal(layout, 90, 26), true, true)
	app.SetFocus(layout)
}

func showAbout() {
	aboutText := `About [yellow]YAMA - Yet Another Morse App[-]
` + Version +
		`
Created by: Bill Lanahan, WA2NFN

Positive feedback accepted at cw.or.bust@gmail.com.

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
	list.SetBackgroundColor(tcell.GetColor(AppBackgroundColor)).SetBorder(true).SetTitle(" Input Files (cursor & Enter) ")

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

					isProgrammaticUpdate = true // Lock it before injecting the file text!
					inputArea.SetText(actualText, false)
					isProgrammaticUpdate = false // Unlock it

					inputArea.SetChangedFunc(func() {
						// Ignore text changes made by the engine!
						if isProgrammaticUpdate {
							return
						}
						if currentState == StateStopped || currentState == StatePaused {
							currentState = StateIdle
						}
						refreshUI(currentState)
					})

					if currentState == StateStopped || currentState == StatePaused {
						currentState = StateIdle
					}
					refreshUI(currentState)
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

	useProsignsCb := tview.NewCheckbox().SetLabel("Play ProSigns")
	extendedPuncCb := tview.NewCheckbox().SetLabel("Extended Punctuation")
	useSkipCb := tview.NewCheckbox().SetLabel("Use Skip")

	skipListInput := tview.NewInputField().SetLabel("Skip List").SetFieldWidth(35)
	skipListInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)
	skipListInput.SetPlaceholder(" e.g. XYZ789<SK>").SetPlaceholderTextColor(tcell.ColorYellow)

	delayOptions := []string{"0", "1", "2", "3", "4", "5"}
	delayDropDown := tview.NewDropDown().SetLabel("Start Delay (sec)").SetOptions(delayOptions, nil)

	repeatOptions := []string{"2", "3", "4", "5", "6", "7", "8", "9"}
	repeatDropDown := tview.NewDropDown().SetLabel("Repeat Limit").SetOptions(repeatOptions, nil)

	randomOrderCb := tview.NewCheckbox().SetLabel("Random Order")
	randomWordsCb := tview.NewCheckbox().SetLabel("Random Words")
	wordBuilderCb := tview.NewCheckbox().SetLabel("Word Builder")

	startMsgCb := tview.NewCheckbox().SetLabel("Use Start Msg")
	startMsgInput := tview.NewInputField().SetLabel("Start Msg Text").SetFieldWidth(30)
	startMsgInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)
	startMsgInput.SetPlaceholder("VVV <KA>").SetPlaceholderTextColor(tcell.ColorYellow)

	endMsgCb := tview.NewCheckbox().SetLabel("Use End Msg")
	endMsgInput := tview.NewInputField().SetLabel("End Msg Text").SetFieldWidth(30)
	endMsgInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)
	endMsgInput.SetPlaceholder("<AR>").SetPlaceholderTextColor(tcell.ColorYellow)

	resetState := func() {
		startMsgCb.SetChecked(config.User.StartMsg)

		// Intercept an empty config and provide real, editable default text
		if config.User.StartMsgText == "" {
			startMsgInput.SetText("VVV <KA>")
		} else {
			startMsgInput.SetText(config.User.StartMsgText)
		}

		endMsgCb.SetChecked(config.User.EndMsg)

		if config.User.EndMsgText == "" {
			endMsgInput.SetText("<AR>")
		} else {
			endMsgInput.SetText(config.User.EndMsgText)
		}

		useProsignsCb.SetChecked(config.User.Playprosigns)
		extendedPuncCb.SetChecked(config.User.UseExtendedPunctuation)
		useSkipCb.SetChecked(config.User.UseSkip)
		skipListInput.SetText(config.User.SkipList)
		randomOrderCb.SetChecked(config.User.RandomOrder)
		randomWordsCb.SetChecked(config.User.RandomWords)
		wordBuilderCb.SetChecked(config.User.WordBuilder)

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
			config.User.Playprosigns = useProsignsCb.IsChecked()
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
	localPath := morse.ResolvePath("yamaIWR.txt")

	if _, err := os.Stat(localPath); err == nil {
		filePath = localPath
	} else {
		if docDir, errDir := os.UserConfigDir(); errDir == nil {
			// APPLY RESOLVER
			filePath = morse.ResolvePath(filepath.Join(docDir, "YAMA", "yamaIWR.txt"))
		} else {
			filePath = localPath // Fallback
		}
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		content = []byte("# Error loading IWR file or file not found.\n")
	}

	textArea := tview.NewTextArea()
	textArea.SetText(string(content), false)
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

	charInput := tview.NewInputField().SetLabel("Character Speed (wpm)").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
	charInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	effInput := tview.NewInputField().SetLabel("Effective Speed (wpm)").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
	effInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	toneInput := tview.NewInputField().SetLabel("Tone (Hz)").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
	toneInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	iwrCheckbox := tview.NewCheckbox().SetLabel("Use IWR")

	iwrSpdInput := tview.NewInputField().SetLabel("IWR Speed (wpm)").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
	iwrSpdInput.SetFieldBackgroundColor(tcell.ColorBlack).SetFieldTextColor(tcell.ColorWhite)

	iwrToneInput := tview.NewInputField().SetLabel("IWR Tone (Hz)").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
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

	timingContainer.SetBorder(true).SetTitle(" Timing ")
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

	if config.User.UseSkip {
		parser.SetSkipList(config.User.SkipList, morse.ProSignTable)
		rawInput = parser.ApplySkip(rawInput)
	}

	fullTextToPlay = colorTagRegex.ReplaceAllString(rawInput, "")

	actualText = ""
	inputArea.SetText("", false)

	parsedText := parser.CleanText(fullTextToPlay, morse.MorseTable)
	parsedText = parser.CompressSpace(parsedText)

	// --- SMART DEFAULTS ---
	if config.User.StartMsg && strings.TrimSpace(config.User.StartMsgText) == "" {
		config.User.StartMsgText = "VVV <KA>"
	}
	if config.User.EndMsg && strings.TrimSpace(config.User.EndMsgText) == "" {
		config.User.EndMsgText = "<AR>"
	}
	// ----------------------

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

		msg := fmt.Sprintf("\n [yellow]Generating up to %d files (Max %d mins each)\n"+
			"Maximum Output: ~%.1f MB | (~%.1f MB max per file)[-]\n"+
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
		exportDir := morse.ResolvePath(strings.TrimSpace(dirInput.GetText()))
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
