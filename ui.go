package main

import (
	"fmt"
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
var preResizeSnapshot string
var isResized bool

func handlePlayPause(iwrMan *morse.IWRManager) {
	playPauseMu.Lock()
	defer playPauseMu.Unlock()

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

	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor)).
		SetBorder(true).
		SetTitle(" DataStats ")

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("stats")
			app.SetFocus(inputArea)
			return nil
		}
		return event
	})

	pages.AddPage("stats", createModal(tv, 38, 22), true, true)
	app.SetFocus(tv)
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
	isProgrammaticUpdate = true

	if isBlocked {
		inputArea.SetText(maskText(actualText), false)
	} else {
		inputArea.SetText(actualText, false)
	}

	isProgrammaticUpdate = false
	updateBlueLine()
}

func updateBlueLine() {
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
		//info = fmt.Sprintf(" [black] %s  Speed: %g wpm | Eff. Speed: %g wpm ", mode, config.User.CharacterSpeed, config.User.EffectiveSpeed)
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
			menu = "[#FFFF55]F[white]ile  [#FFFF55]P[white]lay  [#FFFF55]T[white]iming  [#FFFF55]O[white]ption  [#FFFF55]K[white]eyEcho  [#FFFF55]F1[white]help  a[#FFFF55]B[white]out  [#FFFF55]Q[white]uit "

		} else {
			menu = "[#FFFF55]F[white]ile  [#FFFF55]P[white]lay  [#FFFF55]T[white]iming  [#FFFF55]A[white]udio  [#FFFF55]O[white]ption  [#FFFF55]K[white]eyEcho  [#FFFF55]F1[white]help  a[#FFFF55]B[white]out  [#FFFF55]Q[white]uit "
		}
		if hasText {
			menu = strings.Replace(menu, "a[#FFFF55]B[white]out  ", "", 1)
			menu = strings.Replace(menu, "[#FFFF55]F[white]ile", "[#FFFF55]F[white]ile  [#FFFF55]N[white]umWords", 1)
			menu = strings.Replace(menu, "[#FFFF55]P[white]lay", "[#FFFF55]P[white]lay  [#FFFF55]W[white]ave  [#FFFF55]E[white]rase", 1)
		}

		if config.StatsTotalWords > 0 {
			menu = strings.Replace(menu, "[#FFFF55]O[white]ption", "[#FFFF55]O[white]ption  [#FFFF55]D[white]ataStats", 1)
		}

	case StatePlaying:
		statusLine.SetText(" [#FFFF55]Playing")

		if config.User.Echo {
			menu = "[#FFFF55]P[white]ause  [#FFFF55]S[white]top  [#FFFF55]D[white]ataStats"
		} else {
			menu = "[#FFFF55]P[white]ause  [#FFFF55]S[white]top  [#FFFF55]A[white]udio"
		}
	case StatePaused:
		statusLine.SetText(" [#FFFF55]Paused")

		if config.User.Echo {
			menu = "[#FFFF55]R[white]esume  [#FFFF55]S[white]top  [#FFFF55]T[white]iming  [#FFFF55]D[white]ataStats  [#FFFF55]Q[white]uit "
		} else {
			menu = "[#FFFF55]R[white]esume  [#FFFF55]S[white]top  [#FFFF55]T[white]iming  [#FFFF55]A[white]udio  [#FFFF55]Q[white]uit "
		}
	}
	header.SetText("[#55FFFF::b] YAMA - Yet Another Morse-code App[white::-]\n\n" + menu)
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
	modal := tview.NewFlex().
		SetDirection(tview.FlexRow)

	modal.AddItem(nil, 0, 1, false) // top spacer
	modal.AddItem(
		tview.NewFlex().
			AddItem(nil, 0, 1, false).
			AddItem(p, width, 1, true). // <-- THIS MUST BE focusable
			AddItem(nil, 0, 1, false),
		height,
		1,
		true,
	)
	modal.AddItem(nil, 0, 1, false) // bottom spacer

	return modal
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

func showHelp() {
	helpText := ` [white::-]
Whether you are looking for routine practice, some head-copy tools, want to test your copying limits against simulated audio impairments, or working on your CW sending skills, YAMA is built to help you.

YAMA has some standard input processing, for example: discarding non-morseable characters, space compression, input case agnostic, as well as some non-traditional ones: selectable ProSign support, selected character filtering, expansion of contractions (e.g. won't to will not), European & Esperanto support, graduating speed, dynamic wave shaping for QRQ, and more. Changes to speed/tone and audio impairments can be made during play. Almost all features are compatible with the key/echo sending feature as well.

YAMA uses a Terminal User Interface (TUI), navigation and selections will be by key combinations, mostly the Control Key and one letter, a few Function keys are supported as alternatives. Help is available with the standard F1 function key. Note: In the menu screens: Timing, Options, and Audio; the back-tab is often quicker to navigate to a field, than several forward tabs.


[green::b]Getting Started: Entering Text[::-]
[white]Before YAMA can play anything, it needs some text. You have three easy ways to do this:
1. Type or Paste: Simply click into the main Text Input box and type or paste your practice text directly. Cursor keys, Backspace, Delete/Insert, Page Up/Down, Home/End, are supported for editing.
2. File Load: Press [yellow]Ctrl-F[-] (note: Ctrl-F, means hold the Control Key and simultaneously the 'f' key) to open the File Selector and browse for any standard '.txt' file on your computer.

With text on the Text Input screen, you can increase or decrease the amount of text with the NumWords option [yellow]Ctrl-N[-].

[green::b]Dynamic Menus & Navigation[::-]
[white]YAMA is operated entirely via keyboard shortcuts (no mouse). Keep an eye on the top menu bar, it is dynamic. YAMA will only show you the shortcuts that make sense for the current context. For example, you cannot open the Options menu while audio is actively playing, therefore there will not be an Options label and [yellow]Ctrl-O[-] will be ignored, the Wave export label will only appear when you actually have text loaded to export.

The method to enter or change an option on the Ctrl-O or Ctrl-T screen will depend on the option type. First navigate to the option of interest using the TAB or BACKTAB, then if the option is a single-character field, like Use ... or Random Order, simply hit Enter or space to toggle the option (then TAB forward); if the option shows a single digit or the name of a timing methods (i.e. Farnsworth) the choices are provided by a drop down, use the cursor and hit Enter; multi-digit options like tones or speeds, use Backspace, type new value and hit Enter or TAB; input text boxes, such as start/end msg or skip characters, use Backspace, enter a value and hit Enter or TAB. 
You can always exit a menu, without a SAVE, by pressing [yellow]ESC[-] to safely close and return to the prior screen. And [yellow]Ctrl-S[-], to SAVE without navigating to the Save button.

[#FFFF55](Note: insertion or removal of headphones can trigger a Windows hang of the YAMA app requiring an app restart. This is a common Windows issue, not YAMA's.)[-]

The following keys display sub menus or perform significant actions.

[white]Key            | Menu Name  | Purpose[-]
---------------|------------|--------------------------------------------------------
Ctrl-F, F3     | File       | Open a .txt file for playback.
Ctrl-N         | NumWords   | Iteratively copies or truncates the current text.
Ctrl-M         |(No Menu)   | This is a Hot-key, use anytime to Mute input sent by YAMA.
               |            | For copy practice features, YAMA continues to consume input|            | but silently. In KeyEcho, the input you are to echo back   |            | is written to the screen instantly and silently. It is an  |            | On/Off toggle, its state is on the Blue Line.
Ctrl-P         | Play/Pause | Start or pause the current loaded input text.
Ctrl-S         | Stop       | Halt playback immediately (cannot be resumed).
Ctrl-W         | Wave       | Export current text to .wav file(s).
Ctrl-E, Ctrl-L | Erase      | Clear the current text input aka screen clear.
Ctrl-T         | Timing     | Speed, Tone, and IWR settings.
Ctrl-O         | Options    | Parser, messaging, and text processing options.
Ctrl-K         | Key/Echo   | Options for sending practice. (see Key/Echo table)
Ctrl-A         | Audio      | Audio impacting impairements (QRN, QSB, Drift, etc.)
Ctrl-D         | DataStats  | View statistics and IWR counts. New input clears old data.
Ctrl-B         | aBout      | App info and License.
Ctrl-Q         | Quit       | Exit YAMA. (Or close the parent window.)
F1             | Help       | This screen text.
ESC            | Close      | Cancel/Close menus without saving.
Spacebar       | Hide/Unhide| Toggle text visibility during audio playback.

[green::b]Supported Characters & Punctuation[::-]
[white]YAMA naturally supports standard letters [yellow]A-Z[-] and numbers [yellow]0-9[-].

[white]Basic punctuation: [yellow]. , ? /[-]
[white]Extended punctuation (Enable in Options): [yellow]: ; " @ ' ( ) $ ! \ [-]

[green::b]ProSigns & Equivalents[::-]
[white]Supported ProSigns:[yellow] <AR> <AS> <BT> <KA> <SK> <VA> <VE> <SN> <BK> <HH> <DU> <SOS> <CH>[-].

If "ProSign Support" is disabled in Options ([yellow]Ctrl-O[-]), bracketed ProSigns will be ignored (including their use in the Start/End Message). However, the standard keyboard equivalents [yellow]+[-] (<AR>), [yellow]=[-] (<BT>), and [yellow]-[-] (<DU>) will still play, unless the shortcuts are added to the Skip List in the Options screen. (Note: any other use of the '<' or '>' character within YAMA is ignored.)

[green::b]Options Screen (Ctrl-O) - Settings[::-]
[white]Setting               | Description
----------------------|--------------------------------------------------------------------------
Word Builder Mode     | Plays words progressively (e.g., T, TH, THE) for building head
                      | buffer comprehension. Mutually exclusive with IWR. IWR speed is 
                      | used to sound the last word.
Word Separator        | If it exists, one character or ProSign in this option separates output.
                      | e.g. A AM AM I IT IT could play as: A AM AM <BT> I IT IT ?. 
                      | If <BT> and ? were in the Word Separator field. If IWR is enabled,
                      | one more full word is played at IWR speed.
Sort                  | With Text or Word Builder, sorts input words in short to long order.
Text Builder          | Plays input words progressively (e.g., 11 22 33 44 plays as:  
                      | 11 11 22 11 22 33 11 22 33 44).
                      | Mutually exclusive with Word Builder and Randomize Words.
Word Count            | Limits the count of words used by Text Builder (2-25).
Text Separator        | If its exists, one character or ProSign in this option 
                      | separates output iterations. e.g. aa bb cc play as: 
                      | aa aa bb aa bb cc <BT>, if <BT> was in the Text Separator field.
Flashcard             | For live play except Word Builder, play a word(s) at current speed and 
                      | waits for the user to recognize and hit Enter to get the next word.
                      | Backspace will replay the current word(s).
WordCount             | Number of words per flash (1-20, default 1)
Random Value          | Words per flash, range from 1 to WordCount value.
KeyEcho Mode          | Enables echo back sending feature. Uses external key device. Text is     
                      | played simlarly to Flashcard Mode, then the user keys in back (see below)
Sylablize Words       | Play multi-sylable words by sylable instead of by letter. A head buffer
                      | feature midway between normal play and Word Builder. Approx. 1000 
                      | common words will be syalbalized if not attached to punctuation.
		      | E.g. "COMMON BUT DIFFICULT" plays as "COM MON BUT DIF FI CULT".
Random Word Order     | Shuffles the playback order of the entire document's words.
Randomize Word Chars  | Mutually exclusive with the IWR Mode, scrambles char order in a word.
ProSign Support       | Toggles support for bracketed ProSigns (e.g., <AR>). Does NOT affect [yellow]-+=[-].
Extended Punctuation  | Toggles support for extended punctuation marks.ZZZZ
European Characters   | Toggles support for European & Esperanto Morse characters ([yellow]ä, û, etc.[-])
Use Skip Char List    | Enables the Skip List filtering during playback.
Skip List             | Define specific characters or ProSigns to silently ignore.
Char Repeat Limit     | Caps consecutive repeating characters to prevent 
                      | runaway use of a character. Default 3.
StartUp Delay         | Adds a countdown timer (in seconds) before playback begins.
Use Start Message     | Toggles injecting a custom message at the beginning of the text.
Message Text          | Specific text to play at the start (e.g., VVV <KA>).
Use End Message       | Toggles injecting a custom message at the end of text.
Message Text          | The specific text to play at the end (e.g., <AR>).

[green::b]The Skip List & Contractions[::-]
[white]You can define specific characters or ProSigns to silently skip during playback
(Options -> Skip List).
[yellow]Important Apostrophe Rule:[-] If you add the apostrophe [yellow](')[-] to your skip list, YAMA will automatically expand 17 common English contractions before removing the remaining apostrophes (e.g., "DON'T" safely becomes "DO NOT").

Note that the bottom of the screen has a [yellow]yellow[-] status line and below that a [blue]blue[-] reminder line about your current values from the Timing screen. It may also the phrase [yellow]Input Modified[-], this indicates that a least one option on the Options screen will modify the input in the text screen before it becomes audible CW, so you are not surprised when the first string is sounded that it maybe different than what was just displayed.

Below is an overview of KeyEcho options, a narrative will follow to pull the ideas into a cohesive description.

[green::b]KeyEcho Screen (Ctrl-K) - Settings[::-]
[white]Setting                 | Description
------------------------|-----------------------------------------------------------------------
Tolerance (%)           | % a symbol element (dit, dah, space, etc.) can vary from expected value.
Echo Word Count (1-20)  | Number of words played (default 1), and to key/echo back in each group.
Random Word Count       | Allows count from 1 to Echo Word Count.
Key Now Alert Tone      | Plays a short audible prompt (~.5 dit) for you to begin keying.
                        | There is also a Status Line prompt "Key now..." as well as a brief cursor
			| below the words from YAMA. Do NOT key before the prompt - the decoder will
			| not be active, nor the sidetone.
Alert Tone Frequency    | Allows the alert tone to differ from audible code.
Error Tone              | Play Alert Tone Freq for about 0.5 Dah on keying error.
Key Padding             | Once you start to key, you have the same amount of time that YAMA took
                        | to send you the word(s), plus 4 levels of padding given as descriptive names.
SideTone                | Use the PC sidetone for keying using the same tone as set in 
                        | Timing screen.
Visual Feedback         | If set, your keyed symbols line up below what YAMA sent.
                        | Two identical lines.
                        | is perfect match; * is an invalid morse (i.e. 7 dits) (more on this later)
KeyEcho Port            | A COM port to connect your device (straight key, keyer, bug).
Key Line Interface      | Which leads in the COM port are being used.
Key Line Polarity       | Whether key up is silent (standard) or plays tone, down is inverted.
Parasitic Power         | If needed by the comport cable. (If cable uses opto-isolators, as an example).

[green::b]Timing Screen (Ctrl-T) - Timing[::-]
[white]Setting               | Description
----------------------|-------------------------------------------------------------------------
Timing Method         | Standard, Farnsworth or Wordsworth. Choice controlsif/when thw next few
                      | options apply.
Character Speed       | Applies to all of the above methods.      
End Ramp Speed        | If set, must be greater than the above. Will have an audio session       
                      | increase in speed linearly during the session.
		      | speed will not change with a word.
Effective Speed       | A speed lower than Char Speed, used for Farnsworth
or Wordsworth spacing.|
Tone                  | For YAMA played audio, also fo optional sidetone
in the KeyEcho Mode.
Use IWR Mode          | On on/off feature toggle, placed hear since related to Char speed if the
                      | played audio will sound your chossed IWR words at a fast rate than other
		      | words. 
Speed                 | The character speed for IWR matched words.
Tone                  | Likely best set the same as previous tone.




[green::b]Audio Screen (Ctrl-A) - Audio-impacting impairments[::-]
[white]Setting               | Description
----------------------|-------------------------------------------------------------------------
Static (QRN)          | Injects constant background hiss and random lightning crashes.
Fading (QSB)          | Simulates a slow ionospheric roll, dipping and recovering volume.
Tone Drift            | Simulates an unstable oscillator, bending the pitch up and down.
Speed Drift           | Simulates a tired operator by slowly expanding/contracting the timing.
Key Clicks            | Injects a harsh electrical spark at the start and end of elements.
Brown Noise           | Not an impairment. Can increase mental focus by masking other noise.
Pink Noise            | Not an impairment. Can increase learning (some clinical evidence).

[yellow]Note that you can make changes to the currently playing audio with the Timing or Audio screens.[-]
[yellow]Note the two noise tones are hypothetical, not specific to morse code.[-]

[green::b]WAV File Export (Ctrl-W)[::-]
[white]YAMA exports 16-bit Mono audio. The wave screen lets you select a target directory for the created wave files, if the path does not exist, it will create it. The approximate play time for the chosen speed and the corresponding size is shown. You can also specify (actually limit) the number of files. If your input is a large novel you can certainly limit output to a handful of practice files. If you are emailing the completed files to yourself so that you can play them on cell phone, then capping the file size near 10Mb should be reasonable.

[green::b]IWR - Instant Word Recognition Feature[::-]
[white]This a a head-copy related feature. I looks to match words (actually any space separated string of supported characters (e.g. the qsl 73 cul) in the input, and override the chosen timing mode (standard, Farnsworth, Wordsworth and the associated speed/tone) and play the matched word at a increased speed with standard timing. To do this you must create an [blue]yamaIWR.txt[-] file. A sample file has been created in the in your OS's standard configuration file directory ($HOME\AppData\Roaming\YAMA for Windows). The file will be editable (cursor keys, home/end, pg up/down, backspace, delete/insert) from the Timing screen ([yellow]Ctrl-T[-], or you may create a local one in the same directory that YAMA is launched from, this one will take priority but you will have to edit it with notepad, vi, emacs or your favorite text editor is (not a word processor, unless it has a save as txt option). The file should list one word per line (any case, any order); a [yellow]'#'[-] at the start of line tells YAMA to ignore that line. If you choose to also match the word if its immediately follow by [yellow], . ? : [-] as well as the bare word, this is indicated by a trailing asterisk (e.g. qsl* matches: qsl qsl? qsl. qsl: qsl, Note this is the only supported use of asterisk in the app). The IWR feature as described is ignored if you have choosen either Word Builder or Randomize Word in the Options menu since you would never get a match. A small purposeful interaction with IWR speed is as follows: if you chose Word Builder and have IWR enabled, when Word Builder has completed constructing the word (as in: t te tes test) you will have one more sounding of the final word, but now at IWR speed.

[#FFFF55]Note: Using the high end of the 2K Tone limit may impact the audio profile for QRQ speeds, let your ears guide your choice, rather than the app limit you.[-]

Experiment and I'm sure you will quickly understand the capabilities. Remember, an ESC will always get you back to the previous screen,(without SAVING) whether there is a Cancel button or not.

[green::b]KeyEcho (Ctrl-K) YAMA's Only Sending Feature[::-]
[white]The previous KeyEcho table fully documents the KeyEcho ([yellow]Ctrl-K[-]) options. 

[green::b]Purpose & Interactive Experience[::-]
[white]KeyEcho is designed to actively build and improve your sending skills by having you echo back perfectly timed Morse code generated by YAMA. To align with head word buffering and Instant Word Recognition (IWR) training, the interaction is based on [yellow]groups of one or more words[-], not individual characters (e.g., <word1><wordspace><word2>). 

The length of these groups is unrestricted and depends entirely on your chosen input stream; it could be "E", "RST", "555-1212", or "MISSISSIPPI". Note that selections in the Options screen ([yellow]Ctrl-O[-]), such as Use Prosigns or Extended Punctuation, act as filters to automatically allow or delete specific characters from your input stream.

To begin, enable KeyEcho in Options ([yellow]Ctrl-O[-]) and start the session with Play ([yellow]Ctrl-P[-]). The timing mode will always be standard (IWR feature timing is ignored during KeyEcho).
- Listen & Echo: YAMA plays the audio (with an optional sidetone available in the [yellow]Ctrl-T[-] Timing menu), and you echo it back.
- Success: If your input matches exactly, YAMA automatically advances to the next group.
- Mismatch: YAMA pauses. Press Backspace to retry the group, or Enter to skip and advance.
- Live Controls: Press Spacebar at any time to hide or unhide the main YAMA screen, and [yellow]Ctrl-D[-] to toggle the DataStats overlay.

[green::b]Timing & The Keying Window[::-]
[white]Understanding the timeline of a KeyEcho group is critical for a smooth UX. The sequence flows linearly:
1. YAMA Plays: The application sends the group at your configured speed.
2. Word Space & Alert: YAMA waits one standard word space, then plays a short alert tone (if configured) to signal your turn.
3. Hesitation Window: You have a generous 2-second window to mentally process the word and strike the paddle for your first element. 
4. Keying Window: The moment you close the paddle, the hesitation timer stops, and your performance clock begins.
5. Element Accuracy: Individual elements you key (dits, dahs, and internal spaces) must meet a settable accuracy tolerance to be decoded correctly. 
6. Padding & Evaluation: You must complete the entire group within the exact time YAMA took to send it, plus your chosen Key Padding (Strict, Normal, Relaxed, or Forgiving). When YAMA detects silence equal to 3 standard word spaces, it closes the window and grades your input.

Tip for progression: As your keying practice improves, you can force yourself to match YAMA more strictly by reducing the element Tolerance and lowering the Key Padding both on the [green::b]KeyEcho[-] screen.

[green::b]Hardware Requirements & Setup[::-]
[white]KeyEcho requires an RS-232 COM port adapter. The physical wiring is radically simple: it requires exactly two wires. No jumpers, no common grounds, and no resistors are needed. You simply wire your CW key to bridge one output pin to one input pin, making a series loop from the DB9 source pin through your key device and back to the monitor pin.

[green::b]DB9 Solder-Side Pins:[::-]
[white]- Outputs (Power Sources): Pin 4 (DTR) or Pin 7 (RTS)
- Inputs (Listeners): Pin 8 (CTS), Pin 6 (DSR), Pin 1 (CD), or Pin 9 (RI)
(Pin numbers are listed in the KetEcho options page. Viewd from the solder side of a DB9, with the wide edge on top, the top row
from LEFT to RIGHT is 1 2 3 4 5, the lower row is 6 7 8 9.)

In YAMA's Key Interface options, select the pair you soldered (e.g., CTS-DTR). If your hardware requires an inverted open/closed state (rare), check the Inverted Polarity box. If your adapter requires both outputs to be asserted to supply enough voltage (rare: a harware specific comport cable), check Parasitic Power.

[green::b]KeyEcho Quick Test[::-]
[white]1. Ctrl-T Timing screen: set a Character Speed, and Tone. Then SAVE
2. Ctrl-K KeyEcho screen: set sidetone, set alert, set KeyLineMode for the wiring/pins you soldered on your
DB9. Set Polarity to standard, uncheck Parasitic Power. SAVE
3. Ctrl-O Options: Check KeyEcho. SAVE
4. On Text Input (main screen), type a few words. 
5. Of course insert your comport, with the key device connected.
6. Ctrl-P to start Play. YAMA should sound and display an input word(s); when it stops the lower left side of
the screen should have a green "key now" and you should have heard a brief alert tone. Attempt to key back to YAMA.

If you heard some CW before you got a TIMED OUT message, you have verified minimum setup.
If you heard a long DAH, not in sync with you sending, then the Polarity option on the KeyEcho
screen needs to be changed. If the status line says NO INPUT, you didn't key anything or connectivity issue.
Default COM PORT is 3, your PC may have other see the drop down choices.

You can see the app is not for complete beginners, nor does it attempt to complete with many excellent training apps such as: Precision CW Tutor & Fistcheck, 
G4FON, LCWO.net, LICW.org, etc., but rather to bundle a number of practice features in a single app.

You can email me at wa2nfn@gmail.com if you find something that needs clarification, a bug, typo, or if a numerical limit causes you an issue. The sound library will not work on MAC, so thats not a consideration; a mouse will never be supported by the UI libarary, so again that is not a consideration either.

73 and best of luck on your CW journey.
WA2NFN
[::-]`

	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText).
		SetScrollable(true)

	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	form := tview.NewForm().
		AddButton("Save to yama_help.html", func() {

			htmlText := strings.ReplaceAll(helpText, "<", "&lt;")
			htmlText = strings.ReplaceAll(htmlText, ">", "&gt;")

			htmlText = strings.ReplaceAll(htmlText, "[yellow::b]", "<span style='color: #FFD700; font-weight: bold;'>")
			htmlText = strings.ReplaceAll(htmlText, "[green::b]", "<span style='color: #FFFF55; font-weight: bold;'>")
			htmlText = strings.ReplaceAll(htmlText, "[white]", "<span style='color: white;'>")
			htmlText = strings.ReplaceAll(htmlText, "[yellow]", "<span style='color: #FFD700;'>")
			htmlText = strings.ReplaceAll(htmlText, "[#FFFF55]", "<span style='color: #FF6666;'>")
			htmlText = strings.ReplaceAll(htmlText, "[blue]", "<span style='color: #00BFFF;'>")
			htmlText = strings.ReplaceAll(htmlText, "[-]", "</span>")
			htmlText = strings.ReplaceAll(htmlText, "[::-]", "</span>")

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

        @media print {
            body, .container { background-color: white; color: black; box-shadow: none; border: none; padding: 0; }
            span { font-weight: bold !important; }
            span[style*="color: #FFD700"] { color: #8B8B00 !important; }
            span[style*="color: white"] { color: black !important; }
            span[style*="color: #FFFF55"] { color: darkgreen !important; }
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
				statusLine.SetText(" [#FFFF55]Help manual exported to " + exportPath + "![-]")
			} else {
				statusLine.SetText(" [#FFFF55]Failed to export HTML file.[-]")
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

	hint := tview.NewTextView().
		SetText(" (Cursor Up/Down as needed) ").
		SetTextColor(tcell.ColorYellow).
		SetTextAlign(tview.AlignCenter)
	hint.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	tv.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyTab {
			app.SetFocus(form)
			return nil
		}
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("help")
			app.SetFocus(inputArea)
			return nil
		}
		return event
	})

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(tv, 0, 1, true).
		AddItem(form, 3, 1, false).
		AddItem(hint, 1, 1, false)

	layout.SetBorder(true).SetTitle(" Help Information ")
	layout.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	pages.AddPage("help", createModal(layout, 100, 26), true, true)
	app.SetFocus(layout)
}

func showAbout() {
	aboutText := `About [#55FFFF]YAMA - Yet Another Morse-code App[-]
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

	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor)).
		SetBorder(true).
		SetTitle(" About ")

	pages.AddPage("about", createModal(tv, 110, 28), true, true)
	app.SetFocus(tv)
}

func showFile(app *tview.Application, pages *tview.Pages, inputArea *tview.TextArea) {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBackgroundColor(tcell.GetColor(AppBackgroundColor)).
		SetBorder(true).SetTitle(" Input Files (cursor & Enter) ")

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

		for _, d := range dirs {
			list.AddItem("[#00BFFF]"+d.Name()+"/", "", 0, nil)
		}

		if len(dirs) == 0 && len(txts) == 0 {
			list.AddItem("[gray](Directory is empty)[-]", "", 0, nil)
		} else if len(dirs) == 0 {
			list.AddItem("[gray](No sub-directories found)[-]", "", 0, nil)
		}

		for _, t := range txts {
			list.AddItem(t.Name(), "", 0, nil)
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

		if name == "[gray](No sub-directories found)[-]" ||
			name == "[gray](Directory is empty)[-]" {
			return
		}

		full := filepath.Join(currentDir, name)

		info, err := os.Stat(full)
		if err != nil {
			inputArea.SetText("Error finding file: "+full+"\n"+err.Error(), true)
			pages.RemovePage("file")
			app.SetFocus(inputArea)
			return
		}

		if info.IsDir() {
			currentDir = full
			populate(currentDir)
			return
		}

		// FILE SELECTED
		currentInputFile = name
		currentFileDir = currentDir

		go func(targetFile string) {
			stopAudio()
			time.Sleep(150 * time.Millisecond)

			data, err := os.ReadFile(targetFile)
			if err != nil {
				app.QueueUpdateDraw(func() {
					inputArea.SetText("Error reading file: "+err.Error(), true)
					pages.RemovePage("file")
					app.SetFocus(inputArea)
				})
				return
			}

			// Your parser pipeline
			txt := parser.NormalizeText(string(data))

			if config.User.UseSkip {
				parser.SetSkipList(config.User.SkipList, morse.ProSignTable)
				txt = parser.ApplySkip(txt)
			}

			actualText := parser.FilterValidMorse(txt, morse.MorseTable)

			// Normalize file content (UC + space compression)
			normalized := strings.Join(strings.Fields(actualText), " ")
			normalized = strings.ToUpper(normalized)
			inputArea.SetText(normalized, false)

			app.QueueUpdateDraw(func() {
				// Disable ChangedFunc temporarily
				inputArea.SetChangedFunc(nil)

				isProgrammaticUpdate = true
				isResized = false
				preResizeSnapshot = ""
				inputArea.SetText(normalized, false) // final load
				isProgrammaticUpdate = false

				// Restore your REAL ChangedFunc
				inputArea.SetChangedFunc(func() {
					if isProgrammaticUpdate {
						return
					}

					txt := inputArea.GetText()
					upper := strings.ToUpper(txt)

					isProgrammaticUpdate = true
					inputArea.SetText(upper, false)
					isProgrammaticUpdate = false

					if currentState == StateStopped || currentState == StatePaused {
						currentState = StateIdle
					}

					isResized = false
					preResizeSnapshot = ""
					refreshUI(currentState)
				})

				pages.RemovePage("file")
				app.SetFocus(inputArea)
				refreshUI(currentState)
			})
		}(full)
	})

	pages.AddPage("file", createModal(list, 40, 20), true, true)
	app.SetFocus(list)
}
