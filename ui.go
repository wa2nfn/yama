package main

import (
	"cmp"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
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
		return
	}

	if currentState == StateIdle || currentState == StateStopped {
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
			log.Printf("Panic Error: %v", r)
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
	iwrStatus := "OFF"
	if config.User.IWREnabled {
		iwrStatus = "ON"
	}

	var info string
	if config.User.CharacterSpeed <= config.User.EffectiveSpeed {
		info = fmt.Sprintf(" [black]Char Speed: %g wpm | IWR: %s (%g wpm) ", config.User.CharacterSpeed, iwrStatus, config.User.IWRSpeed)
	} else {
		mode := "Farnsworth"
		if config.User.UseWordsworth {
			mode = "Wordsworth"
		}
		info = fmt.Sprintf(" [black]Mode: %s | Char Speed: %g wpm | Effective Speed: %g wpm | IWR: %s (%g wpm) ", mode, config.User.CharacterSpeed, config.User.EffectiveSpeed, iwrStatus, config.User.IWRSpeed)
	}

	wordCount := len(strings.Fields(actualText))
	info += fmt.Sprintf("| Word Cnt: %d ", wordCount)

	if echoActive {
		tol := fmt.Sprintf(" Tolerance %d %% ", config.User.EchoTolerance)
		info = info + tol
	}

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

		menu = "[#FFFF55]F[white]ile  [#FFFF55]P[white]lay  [#FFFF55]T[white]iming  [#FFFF55]A[white]udio  [#FFFF55]O[white]ption  [#FFFF55]K[white]eyEcho  [#FFFF55]F1[white]help  a[#FFFF55]B[white]out  [#FFFF55]Q[white]uit "

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
			menu = "[#FFFF55]P[white]ause  [#FFFF55]S[white]top [#FFFF55]A[white]udio  [#FFFF55]D[white]ataStats"
		} else {
			menu = "[#FFFF55]P[white]ause  [#FFFF55]S[white]top [#FFFF55]A[white]udio"
		}
	case StatePaused:
		statusLine.SetText(" [#FFFF55]Paused")

		if config.User.Echo {
			menu = "[#FFFF55]R[white]esume  [#FFFF55]S[white]top  [#FFFF55]T[white]iming  [#FFFF55]A[white]udio  [#FFFF55]D[white]ataStats  [#FFFF55]Q[white]uit "
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
Whether you are looking for routine practice, some headcopy tools, want to test your copying limits against simulated audio impairments, or working on your CW sending skills, YAMA is built to help you.

YAMA has some standard input processing, for example: discarding non-morseable characters, space compression, input case agnostic, as well as some non-traditional ones: selectable ProSign support, selected character filtering, expansion of contractions (e.g. won't to will not), European & Esparanto support, graduating speed, dynamic wave shaping for QRQ, and more. Changes to speed/tone and audio impairments can be made during play. Almost all features are compatible with the key/echo sendingfeature as well.

YAMA uses a Terminal User Interface (TUI), navigation and selections will be by key combinations, mostly the Control Key and one letter, a few Function keys are supported as alternatives. Help is available with the standard F1 function key. Note: In the menu screens: Timing, Options, and Audio; the back-tab is often quicker to navigate to a field, than several forward tabs.

[green::b]Getting Started: Entering Text[::-]
[white]Before YAMA can play anything, it needs some text. You have three easy ways to do this:
1. Type or Paste: Simply click into the main Text Input box and type or paste your practice text directly. Cursor keys, Backspace, Delete/Insert, Page Up/Down, Home/End, are supported for editing.
2. File Load: Press [yellow]Ctrl-F[-] (note: Ctrl-F, means hold the Control Key and simultaneously the 'f' key) to open the File Selector and browse for any standard '.txt' file on your computer.

With text on the Text Input screen, you can increase or decrease the amount of text with the NumWords option [yellow]Ctrl-N[-].

[green::b]Dynamic Menus & Navigation[::-]
[white]YAMA is operated entirely via keyboard shortcuts (no mouse). Keep an eye on the top menu bar, it is dynamic. YAMA will only show you the shortcuts that make sense for the current context. For example, you cannot open the Options menu while audio is actively playing, therefore there will not be an Options label and [yellow]Ctrl-O[-] will be ignored, the Wave export label will only appear when you actually have text loaded to export.

The method to enter or change an option on the Ctrl-O or Ctrl-T screen will depend on the option type. First navigate to the option of interest using the TAB or BACKTAB, then if the option is a single-character field, like Use ... or Random Order, simply hit Enter or space to toggle the option (then TAB forward); if the option shows a single digit or the name of a timing mode (i.e. Farnsworth) the choices are provided by a drop down, use the cursor and hit Enter; multi-digit options like tones or speeds, use Backspace, type new value and hit Enter or TAB; input text boxes, such as start/end msg or skip characters, use Backspace, enter a value and hit Enter or TAB. 

You can always exit a menu, without a SAVE, by pressing [yellow]ESC[-] to safely close and return to the prior screen.

[#FFFF55](Note: insertion or removal of headphones can trigger a Windows hang of the Yama app requiring an app restart. This is a common Windows issue, not YAMA's.)[-]

The following keys display sub menus or perform significant actions.

[white]Key            | Menu Name  | Purpose[-]
---------------|------------|--------------------------------------------------------
Ctrl-F, F3     | File       | Open a .txt file for playback.
Ctrl-N         | NumWords   | Iteratively copies or truncates the current text.
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

If "Play ProSigns" is disabled in Options ([yellow]Ctrl-O[-]), bracketed ProSigns will be ignored (including their use in the Start/End Msg). However, the standard keyboard equivalents [yellow]+[-] (<AR>), [yellow]=[-] (<BT>), and [yellow]-[-] (<DU>) will still play, unless the shortcuts are added to the Skip List in the Options screen. (Note: any other use of the '<' or '>' character within YAMA is ignored.)

[green::b]Options Screen (Ctrl-O) - Settings[::-]
[white]Setting               | Description
----------------------|--------------------------------------------------------------------------
Play ProSigns         | Toggles support for bracketed ProSigns (e.g., <AR>). Does NOT affect [yellow]-+=[-].
                      | (Note: <BK> is sounded as  "B K"). 
Extended Punctuation  | Toggles support for extended punctuation marks.
European Characters   | Toggles support for European & Esparanto Morse characters ([yellow]ä, û, etc.[-])
Use Skip              | Enables the Skip List filtering during playback.
Skip List             | Define specific characters or ProSigns to silently ignore.
                      | Entered without any separators (e.g. XY7<BT>=).
Start Delay           | Adds a countdown timer (in seconds) before playback begins.
Repeat Limit          | Caps consecutive repeating characters to prevent 
                      | runaway use of a character (e.g. underline titles). Default 3.
Random Order          | Shuffles the playback order of the entire document's words.
Randomize Words       | Scrambles the letters within individual words (e.g. code group).
                      | Mutually exclusive with the IWR function.
Word Builder          | Plays words progressively (e.g., T, TH, THE) for building head
                      | buffer comprehension. Mutually exclusive with IWR. IWR speed is 
                      | used to sound the last word.
Word Separator        | If it exists, one character or ProSign in this option separates output.
                      | e.g. A AM AM I IT IT could play as: A AM AM <BT> I IT IT ?. 
                      | If <BT> and ? were in the Word Separator field. If IWR is enabled,
                      | one more full word is played at IWR speed.
Sort                  | With Text Builder or Word Builder, soerts input words in shprt to long order,
Sylablize Words       | Play multi-sylable words by sylable instead of by letter. A head buffer
                      | feature midway between normal play and Word Builder. Approx. 1000 
                      | common words will ne syalbalized if not attached to punctuation.
                      | E.g. "THIS TEXT IS COMPLICATED" plays as "
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
Random                | Words per flash, range from 1 to WordCount value.
Use Start Msg         | Toggles injecting a custom message at the beginning of the text.
Start Msg Text        | Specific text to play at the start (e.g., VVV <KA>).
Use End Msg           | Toggles injecting a custom message at the end of text.
End Msg Text          | The specific text to play at the end (e.g., <AR>).

[green::b]The Skip List & Contractions[::-]
[white]You can define specific characters or ProSigns to silently skip during playback
(Options -> Skip List).
[yellow]Important Apostrophe Rule:[-] If you add the apostrophe [yellow](')[-] to your skip list, YAMA will automatically expand 17 common English contractions before removing the remaining apostrophes (e.g., "DON'T" safely becomes "DO NOT").

Note that the bottom of the screen has a [yellow]yellow[-] status line and below that a [blue]blue[-] reminder line about your current vales from the Timing screen. It may also the phrase [yellow]Input Modified[-], this indicates that a least one option on the Options screen will modify the input in the text screen before it becomes audible CW, so you are not surprised when the first string is sounded that it maybe different than what was just displayed.

Below is an overview of KeyEcho options, a narrative will follow to pull the ideas into a cohesive description.

[green::b]KeyEcho Screen (Ctrl-K) - Settings[::-]
[white]Setting                 | Description
------------------------|-----------------------------------------------------------------------
Tolerance (%)           | % a symbol element (dit, dah, space, etc.) can vary from expected value.
Echo Word Count (1-20)  | Number of words played (default 1), and to key/echo back in each group.
Random Word Count       | Allows count from 1 to Echo Word Count.
Key Now Alert Tone      | Plays a short audible prompt (~.5 dit) for you to begin keying.
Alert Tone Frequency    | Allows the alert tone to differ from audible code.
Last Word Space Dit Cnt | All words are followed by a space. This allows the LAST word in a group
                        | to have 4-7 dit length wordspace for more rythmic keying. (default 7)
Key Time Padding %      | Once you start to key, you have the same amount of time that YAMA took
                        | to send you the word(s), plus 4 levels of padding given as 
                        | descriptive names.
SideTone                | Use the PC sidetone for keying using the same tone as set in 
                        | Timing screen.
Visual Feedback         | If set, your keyed symbols line up below what YAMA sent.
                        | Two identical lines.
                        | is perfect match; * is an invalid morse (i.e. 7 dits) (more on this later)
KeyEcho Port            | A COM port to connect your device (straight key, keyer, bug).
Key Line Interface      | Which leads in the COM port are being used.
Key Line Polarity       | Whether key up is silent (standard) or plays tone, down is inverted.
Parasitic Power         | If needed by the comport cable. (If cable uses opto-isolators, as an example).

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
[white]This a a head-copy related feature. I looks to match words (actually any space separated string of supported characters (e.g. the qsl 73 cul) in the input, and override the chosen timing mode (standard, Farnsworth, Wordsworth and the associated speed/tone) and play the matched word at a increased speed with standard timing. To do this you must create an [blue]yamaIWR.txt[-] file. A sample file has been created in the in your OS's standard configuration file directory ($HOME\AppData\Roaming\YAMA for Windows). The file will be editable (cursor keys, home/end, pg up/down, backspace, delete/insert) from the Timing screen ([yellow]Ctrl-T[-], or you may create a local one in the same directory that Yama is launched from, this one will take priority but you will have to edit it with notepad, vi, emacs or your favorite text editor is (not a word processor, unless it has a save as txt option). The file should list one word per line (any case, any order); a [yellow]'#'[-] at the start of line tells YAMA to ignore that line. If you choose to also match the word if its immediately follow by [yellow], . ? : [-] as well as the bare word, this is indicated by a trailing asterisk (e.g. qsl* matches: qsl qsl? qsl. qsl: qsl, Note this is the only supported use of asterisk in the app). The IWR feature as described is ignored if you have choosen either Word Builder or Randomize Word in the Options menu since you would never get a match. A small purposeful interaction with IWR speed is as follows: if you chose Word Builder and have IWR enabled, when Word Builder has completed constructing the word (as in: t te tes test) you will have one more sounding of the final word, but now at IWR speed.

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
6. Padding & Evaluation: You must complete the entire group within the exact time YAMA took to send it, plus your chosen Key Time Padding (Strict, Normal, Relaxed, or Forgiving). When YAMA detects silence equal to 3 standard word spaces, it closes the window and grades your input.

Tip for progression: As your keying practice improves, you can force yourself to match YAMA more strictly by reducing the element Tolerance and lowering the Key Time Padding both on the [green::b]KeyEcho[-] screen.

[green::b]Hardware Requirements & Setup[::-]
[white]KeyEcho requires an RS-232 COM port adapter. The physical wiring is radically simple: it requires exactly two wires. No jumpers, no common grounds, and no resistors are needed. You simply wire your CW key to bridge one output pin to one input pin, making a series loop from the DB9 source pin through your key device and back to the monitor pin.

DB9 Solder-Side Pins:
- Outputs (Power Sources): Pin 4 (DTR) or Pin 7 (RTS)
- Inputs (Listeners): Pin 8 (CTS), Pin 6 (DSR), Pin 1 (CD), or Pin 9 (RI)
(Pin numbers are listed in the KetEcho options page. Viewd from the solder side of a DB9, with the wide edge on top, the top row
from LEFT to RIGHT is 1 2 3 4 5, the lower row is 6 7 8 9.)

In YAMA's Key Interface options, select the pair you soldered (e.g., CTS-DTR). If your hardware requires an inverted open/closed state (rare), check the Inverted Polarity box. If your adapter requires both outputs to be asserted to supply enough voltage (rare: a harware specific comport cable), check Parasitic Power.

[green::b]KeyEcho Quick Test[::-]
1. Ctrl-T Timing screen: set a Character Speed, and Tone. Then SAVE
2. Ctrl-K KeyEcho screen: set sidetone, set alert, set KeyLineMode for the wiring/pins you soldered on your
DB9. Set Polarity to standard, uncheck Parasitic Power. SAVE
3. Ctrl-O Options: Check KeyEcho. SAVE
4. On Text Input (main screen), type a few words. 
5. Of course insert your comport, with the key device connected.
6. Ctrl-P to start Play. YAMA should sound and display an input word(s); when it stops the lower left side of
the screen should have a green "key now" and you should have heard a brief alert tone. Attempt to key back to YAMA.

If you heard some CW before you got a TIMED OUT message, you have verified minium setup.
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

func showEuropeanCharSelector(parentContainer tview.Primitive, activeEuroSkip *string, parentSaveFunc func()) {
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	form.SetItemPadding(0)
	type ExtChar struct {
		Rune rune
		Name string
	}

	extChars := []ExtChar{
		{'Ä', "Ä (Euro: A-umlaut)"},
		{'Ö', "Ö (Euro: O-umlaut)"},
		{'Ü', "Ü (Euro: U-umlaut)"},
		{'É', "É (Euro: E-acute)"},
		{'Á', "Á (Euro: A-acute)"},
		{'Å', "Å (Euro: A-ring)"},
		{'Ç', "Ç (Euro: C-cedilla)"},
		{'Ñ', "Ñ (Euro: N-tilde)"},
		{'À', "À (Euro: A-grave)"},
		{'È', "È (Euro: E-grave)"},
		{'Ĉ', "Ĉ (Esp: C-circumflex)"},
		{'Ĝ', "Ĝ (Esp: G-circumflex)"},
		{'Ĥ', "Ĥ (Esp: H-circumflex)"},
		{'Ĵ', "Ĵ (Esp: J-circumflex)"},
		{'Ŝ', "Ŝ (Esp: S-circumflex)"},
		{'Ŭ', "Ŭ (Esp: U-breve)"},
	}

	var checkboxes []*tview.Checkbox

	for _, ec := range extChars {
		cb := tview.NewCheckbox().SetLabel(ec.Name)
		checkboxes = append(checkboxes, cb)
		form.AddFormItem(cb)
	}

	resetState := func() {
		for i, ec := range extChars {
			isChecked := strings.ContainsRune(*activeEuroSkip, ec.Rune)
			checkboxes[i].SetChecked(isChecked)
		}
	}

	resetState()

	onSave := func() {
		var newList string
		for i, cb := range checkboxes {
			if cb.IsChecked() {
				newList += string(extChars[i].Rune)
			}
		}

		*activeEuroSkip = newList

		pages.RemovePage("extCharSelector")
		parentSaveFunc()
	}

	form.AddButton("Save", onSave)
	form.AddButton("Reset", resetState)
	form.AddButton("Cancel", func() {
		pages.RemovePage("extCharSelector")
		app.SetFocus(parentContainer)
	})

	applyFocusStyles(form)
	form.SetBorder(false)

	footerView := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetDynamicColors(true)
	footerView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	footerView.SetText("\n[yellow]ESC to Cancel[-]\n")

	container := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true).
		AddItem(footerView, 2, 1, false)

	container.SetBorder(true).SetTitle(" European & Esperanto Skips ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	pages.AddPage("extCharSelector", createModal(container, 45, 24), true, true)
	app.SetFocus(container)
}

func forceUppercaseInputCapture() func(event *tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune {
			r := event.Rune()
			upper := unicode.ToUpper(r)
			if upper != r {
				return tcell.NewEventKey(tcell.KeyRune, upper, event.Modifiers())
			}
		}
		return event
	}
}

func showIWREditModal(parentContainer tview.Primitive) {
	var filePath string

	localPath := morse.ResolvePath("yamaIWR.txt")

	if _, err := os.Stat(localPath); err == nil {
		filePath = localPath
	} else {
		if docDir, errDir := os.UserConfigDir(); errDir == nil {
			filePath = morse.ResolvePath(filepath.Join(docDir, "YAMA", "yamaIWR.txt"))
		} else {
			filePath = localPath
		}
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		content = []byte("# Error loading IWR file or file not found.\n")
	}

	textArea := tview.NewTextArea()
	textArea.SetText(string(content), false)
	textArea.SetBorder(true).SetTitle(fmt.Sprintf(" Editing: %s ", filePath))

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
				cleanWord = strings.ToUpper(cleanWord)
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
		app.SetFocus(parentContainer) // HERE
	}

	form.AddButton("Save", onSave)
	form.AddButton("Cancel", func() {
		pages.RemovePage("iwredit")
		app.SetFocus(parentContainer) // HERE
	})

	textArea.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// 1. Navigation / control keys
		switch event.Key() {
		case tcell.KeyTab:
			app.SetFocus(form)
			return nil
		case tcell.KeyCtrlW:
			onSave()
			return nil
		case tcell.KeyEsc:
			pages.RemovePage("iwredit")
			app.SetFocus(parentContainer)
			return nil
		}

		// 2. Force uppercase for typed runes
		if event.Key() == tcell.KeyRune {
			r := event.Rune()
			upper := unicode.ToUpper(r)
			if upper != r {
				return tcell.NewEventKey(tcell.KeyRune, upper, event.Modifiers())
			}
		}

		return event
	})

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(textArea, 0, 1, true).
		AddItem(form, 3, 1, false)

	pages.AddPage("iwredit", createModal(layout, 75, 24), true, true)
	app.SetFocus(textArea)
}

func showToneSpeed() {
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	acceptDigits := func(textToCheck string, lastChar rune) bool {
		if textToCheck == "" {
			return true
		}
		_, err := strconv.Atoi(textToCheck)
		return err == nil
	}

	acceptFloat := func(textToCheck string, lastChar rune) bool {
		if textToCheck == "" || textToCheck == "." {
			return true
		}
		_, err := strconv.ParseFloat(textToCheck, 64)
		return err == nil
	}

	modeDropDown := tview.NewDropDown().SetLabel("Mode").SetOptions([]string{"Standard", "Farnsworth", "Wordsworth"}, nil)

	charInput := tview.NewInputField().SetLabel("Character Speed (wpm)").SetFieldWidth(6).SetAcceptanceFunc(acceptFloat)
	charInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	endSpeedInput := tview.NewInputField().SetLabel("      End Speed (wpm)").SetFieldWidth(6).SetAcceptanceFunc(acceptFloat)
	endSpeedInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	effInput := tview.NewInputField().SetLabel("Effective Speed (wpm)").SetFieldWidth(6).SetAcceptanceFunc(acceptFloat)
	effInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	toneInput := tview.NewInputField().SetLabel("Tone (Hz)").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
	toneInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	iwrCheckbox := tview.NewCheckbox().SetLabel("Use IWR")

	iwrSpdInput := tview.NewInputField().SetLabel("IWR Speed (wpm)").SetFieldWidth(6).SetAcceptanceFunc(acceptFloat)
	iwrSpdInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	iwrToneInput := tview.NewInputField().SetLabel("IWR Tone (Hz)").SetFieldWidth(5).SetAcceptanceFunc(acceptDigits)
	iwrToneInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	formatFloatOnExit := func(input *tview.InputField) {
		input.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			k := event.Key()
			if k == tcell.KeyTab || k == tcell.KeyEnter || k == tcell.KeyBacktab || k == tcell.KeyDown || k == tcell.KeyUp {
				valStr := input.GetText()
				if valStr != "" && valStr != "." {
					if val, err := strconv.ParseFloat(valStr, 64); err == nil {
						rounded := math.Round(val*10) / 10
						input.SetText(fmt.Sprintf("%g", rounded))
					}
				}
			}
			return event
		})
	}

	formatFloatOnExit(charInput)
	formatFloatOnExit(endSpeedInput)
	formatFloatOnExit(effInput)
	formatFloatOnExit(iwrSpdInput)

	resetState := func() {
		modeIdx := 0
		if config.User.UseFarnsworth {
			modeIdx = 1
		}
		if config.User.UseWordsworth {
			modeIdx = 2
		}
		modeDropDown.SetCurrentOption(modeIdx)

		charInput.SetText(fmt.Sprintf("%g", config.User.CharacterSpeed))
		effInput.SetText(fmt.Sprintf("%g", config.User.EffectiveSpeed))
		toneInput.SetText(fmt.Sprintf("%d", config.User.Tone))
		iwrCheckbox.SetChecked(config.User.IWREnabled)
		iwrSpdInput.SetText(fmt.Sprintf("%g", config.User.IWRSpeed))
		iwrToneInput.SetText(fmt.Sprintf("%d", config.User.IWRTone))
	}

	resetState()

	form.AddFormItem(modeDropDown)
	form.AddFormItem(charInput)
	form.AddFormItem(endSpeedInput)
	form.AddFormItem(effInput)
	form.AddFormItem(toneInput)
	form.AddFormItem(iwrCheckbox)
	form.AddFormItem(iwrSpdInput)
	form.AddFormItem(iwrToneInput)

	var timingContainer *tview.Flex

	onSave := func() {
		optIdx, _ := modeDropDown.GetCurrentOption()
		endSpd, _ := strconv.ParseFloat(endSpeedInput.GetText(), 64)
		effSpd, _ := strconv.ParseFloat(effInput.GetText(), 64)
		charSpd, _ := strconv.ParseFloat(charInput.GetText(), 64)
		tone, _ := strconv.Atoi(toneInput.GetText())
		iwrSpd, _ := strconv.ParseFloat(iwrSpdInput.GetText(), 64)
		iwrTone, _ := strconv.Atoi(iwrToneInput.GetText())

		effSpd = math.Round(effSpd*10) / 10
		endSpd = math.Round(endSpd*10) / 10
		charSpd = math.Round(charSpd*10) / 10
		iwrSpd = math.Round(iwrSpd*10) / 10

		var errors []string

		if effSpd < float64(config.MinEffSpeed) || effSpd > float64(config.MaxEffSpeed) {
			errors = append(errors, fmt.Sprintf("Effective Speed clamped to %d-%d", config.MinEffSpeed, config.MaxEffSpeed))
			if effSpd < float64(config.MinEffSpeed) {
				effSpd = float64(config.MinEffSpeed)
			} else {
				effSpd = float64(config.MaxEffSpeed)
			}
		}

		if charSpd < float64(config.MinCharSpeed) || charSpd > float64(config.MaxCharSpeed) {
			errors = append(errors, fmt.Sprintf("Character Speed clamped to %d-%d", config.MinCharSpeed, config.MaxCharSpeed))
			if charSpd < float64(config.MinCharSpeed) {
				charSpd = float64(config.MinCharSpeed)
			} else {
				charSpd = float64(config.MaxCharSpeed)
			}
		}

		if charSpd <= effSpd && optIdx != 0 {
			errors = append(errors, "Character Speed must be > Effective Speed")
			charSpd = effSpd + 0.1
		}

		if iwrCheckbox.IsChecked() {
			if iwrSpd < float64(config.MinIWRSpeed) || iwrSpd > float64(config.MaxIWRSpeed) {
				errors = append(errors, fmt.Sprintf("IWR Speed clamped to %d-%d", config.MinIWRSpeed, config.MaxIWRSpeed))
				if iwrSpd < float64(config.MinIWRSpeed) {
					iwrSpd = float64(config.MinIWRSpeed)
				} else {
					iwrSpd = float64(config.MaxIWRSpeed)
				}
			}

			if iwrSpd <= charSpd {
				errors = append(errors, "IWR Speed must be > Character Speed")
				iwrSpd = charSpd + 0.1
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

		if endSpd < charSpd {
			endSpd = charSpd
		}
		if endSpd > float64(config.MaxCharSpeed) {
			errors = append(errors, fmt.Sprintf("End Speed clamped to Max %d", config.MaxCharSpeed))
			endSpd = float64(config.MaxCharSpeed)
		}

		apply := func() {
			config.User.UseStandard = (optIdx == 0)
			config.User.UseFarnsworth = (optIdx == 1)
			config.User.UseWordsworth = (optIdx == 2)
			config.User.EffectiveSpeed = effSpd
			config.User.CharacterSpeed = charSpd
			config.User.EndSpeed = endSpd
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
		showIWREditModal(form.GetButton(0))
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

	pages.AddPage("tonespeed", createModal(timingContainer, 38, 23), true, true)
	app.SetFocus(timingContainer)
}

func showSuccessModal(dir string, files []string) {
	var sb strings.Builder

	cleanDir := morse.ResolvePath(dir)
	if absDir, err := filepath.Abs(cleanDir); err == nil {
		cleanDir = absDir
	}

	sb.WriteString(fmt.Sprintf("[white]Saved to:\n[#00BFFF]%s[-]\n", cleanDir))
	sb.WriteString(strings.Repeat("-", 46) + "\n")

	for _, f := range files {
		sb.WriteString(fmt.Sprintf("[white]%s\n", filepath.Base(morse.ResolvePath(f))))
	}
	sb.WriteString("\n[yellow]ESC to close[-]")

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

	boxHeight := len(files) + 10
	if boxHeight > 22 {
		boxHeight = 22
	}

	pages.AddPage("success", createModal(tv, 55, boxHeight), true, true)
	app.SetFocus(tv)
}

// IMPAIRMENTS MENU
func showImpairments() {
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	form.SetItemPadding(0)
	var container *tview.Flex

	levels := []string{"Off", "Light", "Heavy", "Severe"}

	staticDropDown := tview.NewDropDown().SetLabel("Static (QRN)").SetOptions(levels, nil)
	fadingDropDown := tview.NewDropDown().SetLabel("Fading (QSB)").SetOptions(levels, nil)
	toneDriftDropDown := tview.NewDropDown().SetLabel("Tone Drift").SetOptions(levels, nil)
	speedDriftDropDown := tview.NewDropDown().SetLabel("Speed Drift").SetOptions(levels, nil)

	keyClickCb := tview.NewCheckbox().SetLabel("Key Clicks")

	levelsB_PN := []string{"Off", "Minimal", "Low", "Medium", "High"}
	brownNoiseDropDown := tview.NewDropDown().SetLabel("Brown Noise").SetOptions(levelsB_PN, nil)
	pinkNoiseDropDown := tview.NewDropDown().SetLabel("Pink Noise").SetOptions(levelsB_PN, nil)

	resetState := func() {
		staticDropDown.SetCurrentOption(config.User.NoiseStaticLevel)
		fadingDropDown.SetCurrentOption(config.User.NoiseFadingLevel)
		toneDriftDropDown.SetCurrentOption(config.User.NoiseToneDriftLevel)
		speedDriftDropDown.SetCurrentOption(config.User.NoiseSpeedDriftLevel)
		keyClickCb.SetChecked(config.User.NoiseKeyClick)
		brownNoiseDropDown.SetCurrentOption(config.User.BrownNoiseLevel)
		pinkNoiseDropDown.SetCurrentOption(config.User.PinkNoiseLevel)
	}

	resetState()

	form.AddFormItem(staticDropDown)
	form.AddFormItem(fadingDropDown)
	form.AddFormItem(toneDriftDropDown)
	form.AddFormItem(speedDriftDropDown)
	form.AddFormItem(keyClickCb)
	form.AddFormItem(brownNoiseDropDown)
	form.AddFormItem(pinkNoiseDropDown)

	onSave := func() {
		// 1. Grab the current UI selections for the noise filters
		brownLvl, _ := brownNoiseDropDown.GetCurrentOption()
		pinkLvl, _ := pinkNoiseDropDown.GetCurrentOption()

		// 2. The Mutually Exclusive Check
		// Index 0 is "Off". If both are > 0, the user selected both.
		if brownLvl > 0 && pinkLvl > 0 {

			// Create a warning modal
			errorModal := tview.NewModal().
				SetText("Brown Noise and Pink Noise cannot be active at the same time.\n\nPlease set one of them to 'Off'.").
				AddButtons([]string{"OK"}).
				SetDoneFunc(func(buttonIndex int, buttonLabel string) {
					// Close the error modal and give focus back to the impairments form
					pages.RemovePage("noise_error")
					app.SetFocus(container)
				})

			// Add it to the page stack so it pops up immediately
			pages.AddPage("noise_error", errorModal, true, true)

			// Exit early! Do not save the config or close the main form
			return
		}

		// 3. If validation passes, save everything as normal
		config.User.NoiseStaticLevel, _ = staticDropDown.GetCurrentOption()
		config.User.NoiseFadingLevel, _ = fadingDropDown.GetCurrentOption()
		config.User.NoiseToneDriftLevel, _ = toneDriftDropDown.GetCurrentOption()
		config.User.NoiseSpeedDriftLevel, _ = speedDriftDropDown.GetCurrentOption()
		config.User.BrownNoiseLevel = brownLvl
		config.User.PinkNoiseLevel = pinkLvl

		config.User.NoiseKeyClick = keyClickCb.IsChecked()
		config.SaveConfig()

		pages.RemovePage("impairments")
		app.SetFocus(inputArea)
	}

	onReset := func() {
		resetState()
	}

	onClearAll := func() {
		staticDropDown.SetCurrentOption(0)
		fadingDropDown.SetCurrentOption(0)
		toneDriftDropDown.SetCurrentOption(0)
		speedDriftDropDown.SetCurrentOption(0)
		keyClickCb.SetChecked(false)

		config.User.NoiseStaticLevel = 0
		config.User.NoiseFadingLevel = 0
		config.User.BrownNoiseLevel = 0
		config.User.PinkNoiseLevel = 0
		config.User.NoiseToneDriftLevel = 0
		config.User.NoiseSpeedDriftLevel = 0
		config.User.NoiseKeyClick = false

		config.SaveConfig()
	}

	form.AddTextView(" ", "", 0, 1, false, false)
	form.AddButton("Save", onSave)
	form.AddButton("Reset", onReset)
	form.AddButton("Clear All", onClearAll)

	applyFocusStyles(form)
	form.SetBorder(false)

	footerView := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetDynamicColors(true)
	footerView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	footerView.SetText("\n[yellow]ESC to Close[-]\n")

	container = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true).
		AddItem(footerView, 2, 1, false)

	container.SetBorder(true).SetTitle(" Audio Impairments ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	// Catch the ESC key specifically for this modal to close it without saving
	container.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("impairments")
			app.SetFocus(inputArea)
			return nil // Swallow the key
		}
		return event // Pass all other keys (like Tab/Enter) down to the form
	})

	// Your existing code:
	pages.AddPage("impairments", createModal(container, 35, 16), true, true)
	app.SetFocus(container)
}

// NUMWORDS_MENU
func showNumWordsModal() {
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	rawText := inputArea.GetText()
	words := strings.Fields(rawText)
	currentCount := len(words)

	input := tview.NewInputField().
		SetLabel("Target Word Count (1-99999): ").
		SetText(fmt.Sprintf("%d", currentCount)).
		SetFieldWidth(10).
		SetAcceptanceFunc(tview.InputFieldInteger)

	form.AddFormItem(input)

	form.AddButton("Save", func() {
		val, err := strconv.Atoi(input.GetText())

		if err != nil || val < 1 {
			val = 1
		} else if val > 99999 {
			val = 99999
		}

		if val != currentCount && currentCount > 0 {
			// Lock in the snapshot before the very first modification
			if !isResized {
				preResizeSnapshot = rawText
				isResized = true
			}

			var newWords []string
			for i := 0; i < val; i++ {
				newWords = append(newWords, words[i%currentCount])
			}

			isProgrammaticUpdate = true
			inputArea.SetText(strings.Join(newWords, " "), false)
			actualText = inputArea.GetText()
			isProgrammaticUpdate = false

			refreshUI(currentState)
		}

		pages.RemovePage("numWords")
		app.SetFocus(inputArea)
	})

	form.AddButton("Cancel", func() {
		pages.RemovePage("numWords")
		app.SetFocus(inputArea)
	})

	// Only show the Revert button if we actually have a snapshot memory saved
	if isResized {
		form.AddButton("Undo", func() {
			isProgrammaticUpdate = true
			inputArea.SetText(preResizeSnapshot, false)
			actualText = inputArea.GetText()
			isProgrammaticUpdate = false

			// Clear the memory
			isResized = false
			preResizeSnapshot = ""

			refreshUI(currentState)
			pages.RemovePage("numWords")
			app.SetFocus(inputArea)
		})
	}

	applyFocusStyles(form)
	form.SetBorder(false)

	footerView := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetDynamicColors(true)
	footerView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	footerView.SetText("\n[yellow]ESC to Close[-]\n")

	container := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true).
		AddItem(footerView, 2, 1, false)

	container.SetBorder(true).SetTitle(" Resize Text Input ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	container.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("numWords")
			app.SetFocus(inputArea)
			return nil
		}
		return event
	})

	pages.AddPage("numWords", createModal(container, 45, 12), true, true)
	app.SetFocus(container)
}

func showWaveModal(targetDir string) {
	rawText := inputArea.GetText()

	rawText = parser.NormalizeText(rawText)

	if config.User.UseSkip {
		parser.SetSkipList(config.User.SkipList, morse.ProSignTable)
		rawText = parser.ApplySkip(rawText)
	}

	rawText = parser.FilterValidMorse(rawText, morse.MorseTable)

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
	dirInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	prefixInput := tview.NewInputField().SetLabel("File Prefix:").SetText(filePrefix).SetFieldWidth(40)
	prefixInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

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
	minsInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	numFilesInput := tview.NewInputField().
		SetLabel("Number Of Files:").
		SetFieldWidth(10).
		SetAcceptanceFunc(tview.InputFieldInteger)
	numFilesInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

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
			"[gray]Note: Size will be smaller if text doesn't fill the block.[-]",
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
			statusLine.SetText(" [#FFFF55]Error: Cannot create save directory![-]")
			return
		}

		pages.RemovePage("waveConfig")
		statusLine.SetText(" [yellow]Generating WAV files, please wait...")

		go func() {
			generatedNames, err := morse.ExportWAVBatch(rawText, exportDir, prefix, finalWordsPerBlock, numFiles)

			app.QueueUpdateDraw(func() {
				if err != nil {
					statusLine.SetText(" [#FFFF55]Export failed: " + err.Error())
					app.SetFocus(inputArea)
					return
				}
				statusLine.SetText(" [#FFFF55]WAV files generated successfully!")
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

	container.SetBorder(true).SetTitle(" Generate Wave Files ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	layout := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(container, 18, 1, true).
			AddItem(nil, 0, 4, false),
			65, 1, true).
		AddItem(nil, 0, 1, false)

	pages.AddPage("waveConfig", layout, true, true)
	app.SetFocus(container)
}

func startAudioSequence(iwrMan *morse.IWRManager) {

	// 1. SAFETY LOCK: Prevent playback if the audio engine is dead
	if morse.AudioHardwareDead {
		statusLine.SetText(" [#FFFF55::b]FATAL: Audio hardware lost. Restart app.[::-]")
		return
	}

	// ==========================================
	// 🚦 THE GHOST THREAD ASSASSIN 🚦
	// ==========================================
	// Give the old goroutine 150ms to read IsStopping=true and completely
	// exit its loop before we reset the flag. This guarantees the old
	// thread is dead before the new one starts.
	time.Sleep(150 * time.Millisecond)

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

	// Normalize StartMsg and EndMsg to full uppercase
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

	// DO NOT inject StartMsg/EndMsg here.
	// RunIWR will prepend/append them safely and keep them out of RandomOrder.
	parsedText = parser.CompressSpace(parsedText)

	if config.User.StartDelay > 0 {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					playPauseMu.Lock()
					isCountingDown = false
					playPauseMu.Unlock()

					app.Stop()
					fmt.Printf("\n[FATAL] YAMA Crashed in delayed engine start: %v\n", r)
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
	// Split into words (Fields handles multiple spaces)
	words := strings.Fields(text)

	// Sort shortest → longest
	slices.SortFunc(words, func(a, b string) int {
		return cmp.Compare(len(a), len(b))
	})

	// Return as a single space-separated string
	return strings.Join(words, " ")
}

// Two variables for the two columns
var echoStatsTextView *tview.TextView
var currentSessionStats morse.EchoStats

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

	// Borders and titles are back!
	var title = fmt.Sprintf(" DataStats - Keying Tolerance %2d%% ", config.User.EchoTolerance)
	container.SetBorder(true).
		SetTitle(title).
		SetTitleColor(tcell.ColorYellow)

	echoStatsTextView = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft)

	currentProfile := morse.GetTiming(false, config.User)
	echoStatsTextView.SetText(buildEchoStatsText(currentGroupStats, currentSessionStats, currentProfile))

	container.AddItem(echoStatsTextView, 0, 1, false)
	container.AddItem(footer, 1, 0, false)

	container.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			closeEchoStatsWindow() // Use the new helper!
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

	// 1. Safe calculation helper for Averages
	calcAvg := func(sumMs float64, short, perfect, long int) int {
		totalCount := short + perfect + long
		if totalCount > 0 {
			return int(math.Round(sumMs / float64(totalCount)))
		}
		return 0
	}

	// 2. Calculate Targets in ms
	tgtDit := int(math.Round(tp.DotDuration * 1000))
	tgtDah := int(math.Round(tp.DashDuration * 1000))
	tgtEle := int(math.Round(tp.InterElement * 1000))
	tgtChr := int(math.Round(tp.CharSpace * 1000))
	tgtWrd := int(math.Round(tp.WordSpace * 1000))

	// 3. Calculate Session Actual Averages
	avgDit := calcAvg(ses.SumDitMs, ses.ShortDits, ses.PerfectDits, ses.LongDits)
	avgDah := calcAvg(ses.SumDahMs, ses.ShortDahs, ses.PerfectDahs, ses.LongDahs)
	avgEle := calcAvg(ses.SumElementGapsMs, ses.ShortElementGaps, ses.PerfectElementGaps, ses.LongElementGaps)
	avgChr := calcAvg(ses.SumCharGapsMs, ses.ShortCharGaps, ses.PerfectCharGaps, ses.LongCharGaps)
	avgWrd := calcAvg(ses.SumWordGapsMs, ses.ShortWordGaps, ses.PerfectWordGaps, ses.LongWordGaps)

	// Yellow headers, kept!
	sb.WriteString("\n                     [yellow::b]CURRENT GROUP[-:-:-]                            [yellow::b]SESSION TOTALS[-:-:-]\n\n")

	// Added Target and Avg Headers aligned to the right
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
	sb.WriteString(fmt.Sprintf("  Invalid (*):         [red]%3d[-]                                     [red]%3d[-]\n", grp.InvalidSymbols, ses.InvalidSymbols))
	sb.WriteString(fmt.Sprintf("  Group Retries:       [yellow]%3d[-]                                     [yellow]%3d[-]\n", grp.Retries, ses.Retries))

	return sb.String()
}
