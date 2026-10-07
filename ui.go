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
		AddButton("Close", func() {
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
		if event.Key() == tcell.KeyEscape {
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
		if event.Key() == tcell.KeyEscape {
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
			menu = "[#FFFF55]F[white]ile  [#FFFF55]P[white]lay  [#FFFF55]T[white]iming  [#FFFF55]O[white]ption  [#FFFF55]E[white]cho  [#FFFF55]F1[white]help  [#FFFF55]A[white]bout  [#FFFF55]Q[white]uit "

		} else {
			menu = "[#FFFF55]F[white]ile  [#FFFF55]P[white]lay  [#FFFF55]T[white]iming  [#FFFF55]I[white]mpairments  [#FFFF55]O[white]ption  [#FFFF55]E[white]cho  [#FFFF55]F1[white]help  [#FFFF55]A[white]bout  [#FFFF55]Q[white]uit "
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
			menu = "[#FFFF55]P[white]ause  [#FFFF55]S[white]top  [#FFFF55]D[white]ataStats"
		} else {
			menu = "[#FFFF55]P[white]ause  [#FFFF55]S[white]top  [#FFFF55]I[white]mpairments"
		}
	case StatePaused:
		statusLine.SetText(" [#FFFF55]Paused")

		if config.User.Echo {
			menu = "[#FFFF55]R[white]esume  [#FFFF55]S[white]top  [#FFFF55]T[white]iming  [#FFFF55]D[white]ataStats  [#FFFF55]Q[white]uit "
		} else {
			menu = "[#FFFF55]R[white]esume  [#FFFF55]S[white]top  [#FFFF55]T[white]iming  [#FFFF55]I[white]mpairments  [#FFFF55]Q[white]uit "
		}
	}
	header.SetText("[#55FFFF::b] YAMA - Yet Another Morse-code App\nv" + Ver + "[white::-]\n" + menu)
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

func showHelp() {
	app.EnableMouse(true)
	helpText := ` [-:-:-]
Whether you are looking for routine practice, seeking head-copy tools, wanting to test your copying limits against simulated audio impairments, or working on your CW sending skills, YAMA is built to help you.

YAMA features standard input processing, such as: discarding non-Morseable characters, space compression, and case-agnostic input, as well as non-traditional features: selectable ProSign support, selected character filtering, expansion of contractions (e.g., won't to will not), European & Esperanto support, graduating speed, dynamic wave shaping for QRQ, and more. Changes to speed/tone and audio impairments can be made during play. Many operational modes and options can be used in combination, and almost all of them are compatible with the Echo sending feature as well.

YAMA uses a Terminal User Interface (TUI), with some mouse support. Navigation to screens (menus) is performed using key combinations mostly the Control Key plus a letter, though a few Function keys are supported as alternatives. Within screens you have the option to select fields to edit with the mouse, or by TABBING through fields; similarly you can TAB to buttons and hit Enter or mouse click, or Ctrl-S for the SAVE key. Help is available via the standard F1 function key. Note: In the menu screens (Timing, Options, and Audio), using back-tab is often quicker to navigate to a field of interest than pressing tab multiple times forward.

[green::b]High-Level Overview - Please read.[-:-:-]
Your introduction to the app will be greatly enhanced by reviewing the environment briefly before diving into the specific sections below. As the main screen (Input/Output Text) indicates with its prompt, the app is ready to create CW using a number of default options. Type "Hello World" and press Ctrl-P. You will hear 20wpm CW as long as the PC volume is on and you resisted making changes.

Across the top of the screen is a menu bar with names, left to right: [yellow::b]F[white::b]ile, [yellow::b]P[white::b]lay ... [-:-:-]etc. (access and navigation follow below). Some of the menu items cause immediate action (Play, Quit, About...), some collect a few choices and then cause immediate action (File, Wave, Audio...), and three (Options, Echo, Timing) collect input to control the major functional modes of the app (WordBuilder, TextBuilder, FlashCard, Echo, all explained in the tables below). The most significant is the Options screen. This screen serves two functions: First, it enables or activates the modes just listed (indicated by blue-labeled checkboxes on the screen). Second, it collects a long list of options (at the bottom) that impact various modes and the general user experience. All inputs from these screens are permanently saved in a file. 

I've used a lot of words and will elaborate further, but I think you will quickly find it intuitive. Let me clarify two things: First, if your selections are invalid or conflict with another option, a pop-up will warn you and you must correct the problem to save. Second, the Echo mode (for sending practice) is enabled on the Options screen, but its keying behavior is configured on the Echo screen.
A lot more will be said about Echo.

[green::b]Getting Started: Entering Text[-:-:-]
[white]Before YAMA can play anything, it needs text. You have two easy ways to do this:
1. Type or Paste: Simply click into the main Text Input box and type or paste your practice text directly. Cursor keys, Backspace, Delete/Insert, Page Up/Down, and Home/End are supported for editing.
2. File Load: Press [yellow::b]Ctrl-F[-:-:-] (note: Ctrl-F means hold the Control Key and simultaneously press the 'f' key) to open the File Selector and browse for any standard '.txt' file on your PC (e.g., word list, email, a novel). There are no canned or generated input materials.

Once text is on the Text Input/Output screen, you can increase or decrease the amount of text using the NumWords option [yellow::b]Ctrl-N[-:-:-], instead of requiring an external editor.

[green::b]Dynamic Menus & Navigation[-:-:-]
YAMA is operated via keyboard shortcuts and in some case mouse clicks. Keep an eye on the top menu bar, as it is dynamic. YAMA will only show you the shortcuts that make sense for the current context. For example, you cannot open the Options menu while audio is actively playing; therefore, there will not be an Options label and [yellow::b]Ctrl-O[-:-:-] will be ignored. Similarly, the Wave export label will only appear when you actually have text loaded to export.

The method to enter or change an option depends on the option type (similar to a GUI app). First, navigate to the option of interest using TAB or BACKTAB. If the option is a single-character field (like "Use..." or "Random Order"), simply hit Enter or Space to toggle it, then TAB forward. If the option shows a single digit or the name of a timing method (e.g., Farnsworth), the choices are provided in a drop-down menu; use the cursor keys and hit Enter. For multi-digit options like tones or speeds, use Backspace, type the new value, and hit Enter or TAB. For input text boxes (such as start/end messages or skip characters), use Backspace, enter a value, and hit Enter or TAB. 
You can always exit a menu without saving by pressing [yellow::b]ESC[-:-:-] to safely close and return to the prior screen. You can also press [yellow::b]Ctrl-S[-:-:-] to SAVE instantly without navigating to the Save button.

[#FFFF55](Note: insertion or removal of headphones can trigger a Windows hang of the YAMA app requiring an app restart. This is a common Windows issue, not YAMA's.)[-:-:-]

The following keys display sub menus or perform significant actions.

Key            |  Menu Name  | Purpose
---------------|-------------|----------------------------------------------------------------------------------------
Ctrl-A         | About       | App info and License.
Ctrl-D         | DataStats   | Statistics and IWR counts. New input clears old data. During Echo the screen grades
               |             | your timing for dits, dahs, and spaces based on the Tolerance factor you have set.
Ctrl-E         | Echo        | Options for sending practice. (see Echo table)
Ctrl-F, F3     | File        | Open a .txt file for playback.
F4             | (No Menu)   | Turns on a standalone Practice Oscillator.
               |             | ESC exits. (More below)
Ctrl-I         | Impairments | Audio impacting impairments (QRN, QSB, Drift, etc.)
Ctrl-L         | cLear       | Clear the current text input (aka screen clear).
Ctrl-M         | (No Menu)   | This is a Hot-key; use anytime to Mute input sent by YAMA.
               |             | For copy practice features, YAMA continues to consume input 
               |             | but silently. In Echo, the input you are to echo back    
               |             | is written to the screen instantly and silently. It is an   
               |             | On/Off toggle; its state is on the Blue Line.
Ctrl-N         | NumWords    | Iteratively copies or truncates the current text.
Ctrl-O         | Options     | Parser, messaging, and text processing options.
Ctrl-P         | Play/Pause  | Start or pause the current loaded input text.
Ctrl-Q         | Quit        | Exit YAMA. (Or close the parent window.)
Ctrl-S         | Stop        | Halt playback immediately (cannot be resumed).
Ctrl-T         | Timing      | Speed, Tone, and IWR settings.
Ctrl-W         | Wave        | Export current text to .wav file(s).
ESC            | Close       | Cancel/Close menus without saving.
Spacebar       | Hide/Unhide | Toggle text visibility during audio playback.

[green::b]Supported Characters & Punctuation[-:-:-]
YAMA naturally supports standard letters [yellow::b]A-Z[-:-:-] and numbers [yellow::b]0-9[-:-:-].

Basic punctuation: [yellow::b]. , ? /[-:-:-]
Extended punctuation (Enable in Options): [yellow::b]: ; " @ ' ( ) $ ! \ [-:-:-]

[green::b]ProSigns & Equivalents[-:-:-]
Supported ProSigns:[yellow::b] <AR> <AS> <BT> <KA> <SK> <VA> <VE> <SN> <BK> <HH> <DU> <SOS> <CH>[-:-:-].

If "ProSign Support" is disabled in Options ([yellow::b]Ctrl-O[-:-:-]), bracketed ProSigns will be ignored (including their use in the Start/End Message). However, the standard keyboard equivalents [yellow::b]+[-:-:-] (<AR>), [yellow::b]=[-:-:-] (<BT>), and [yellow::b]-[-:-:-] (<DU>) will still play, unless the shortcuts are added to the Skip List in the Options screen. (Note: any other use of the '<' or '>' character within YAMA is ignored.)

[green::b]Options Screen (Ctrl-O) - Settings[-:-:-]
Note: On MAC, and possibly some terminal apps, the Options screen values may not all be visible on screen. If you don't see buttons at the bottom of the page use the TABS to access those fields.

Setting               | Description
----------------------|----------------------------------------------------------------------------------------------
Word Builder Mode     | Plays words progressively (e.g., T, TH, THE) for building head buffer comprehension. 
                      | Mutually exclusive with IWR. IWR speed is used to sound the last word.
Word Separator        | If populated, one character or ProSign in this field separates output.
                      | e.g., A AM AM I IT IT could play as: A AM AM <BT> I IT IT ? if <BT> and ? were in the Word 
                      | Separator field. If IWR is enabled, one more full word is played at IWR speed.
Sort                  | With Text or Word Builder, sorts input words in short-to-long order.
Text Builder          | Plays input words progressively (e.g., 11 22 33 44 plays as: 11 11 22 11 22 33 11 22 33 44).
                      | Mutually exclusive with Word Builder and Randomize Words.
Word Count            | Limits the count of words used by Text Builder (2-25).
Text Separator        | If populated, one character or ProSign in this field separates output iterations. 
                      | e.g., aa bb cc plays as: aa aa bb aa bb cc <BT>, if <BT> was in the Text Separator field.
Flashcard             | For live play except Word Builder, plays a word(s) at current speed and waits for the user to 
                      | recognize and hit Enter to get the next word. Backspace will replay the current word(s).
WordCount             | Number of words per flash (1-20, default 1).
Random Value          | Words per flash, range from 1 to WordCount value.
Echo Mode             | Enables echo-back sending feature. Uses external key device. Text is     
                      | played similarly to Flashcard Mode, then the user keys it back (see below).
Syllablize Words      | Play multi-syllable words by syllable instead of by letter. A head buffer feature similar to 
                      | Word Builder. Approx. 1,000 common words will be syllablized if not attached 
                      | to punctuation. E.g., "COMMON BUT DIFFICULT" plays as "COM MON BUT DIF FI CULT".
Random Word Order     | Shuffles the playback order of the entire document's words.
Randomize Word Chars  | Mutually exclusive with the IWR Mode; scrambles character order in a word.
ProSign Support       | Toggles support for bracketed ProSigns (e.g., <AR>). Does NOT affect [yellow]-+=[-:-:-].
Extended Punctuation  | Toggles support for extended punctuation marks.
European Characters   | Toggles support for European & Esperanto Morse characters ([yellow::b] '\u00C4' '\u0108' etc.[-:-:-])
Use Skip Char List    | Enables the Skip List filtering during playback.
Skip List             | Define specific characters or ProSigns to silently ignore.
Char Repeat Limit     | Caps consecutive repeating characters to prevent runaway use of a character. Default 3.
StartUp Delay         | Adds a countdown timer (in seconds) before playback begins.
Use Start Message     | Toggles injecting a custom message at the beginning of the text.
Message Text          | Specific text to play at the start (e.g., VVV <KA>).
Use End Message       | Toggles injecting a custom message at the end of the text.
Message Text          | The specific text to play at the end (e.g., <AR>).

[green::b]The Skip List & Contractions[-:-:-]
You can define specific characters or ProSigns to silently skip during playback
(Options -> Skip List).
[yellow::b]Important Apostrophe Rule:[-:-:-] If you add the apostrophe [yellow::b](')[-:-:-] to your skip list, YAMA will automatically expand 17 common English contractions before removing the remaining apostrophes (e.g., "DON'T" will becomes "DO NOT").

Note that the bottom of the screen has a [yellow::b]yellow[-:-:-] status line, and below that is a [blue::b]blue[-:-:-] reminder line about your current values from the Timing screen. It may also show the phrase [yellow::b]Input Modified[-:-:-]. This indicates that at least one selection on the Options screen will modify the text input before it becomes audible CW, ensuring you are not surprised if the first sounded string differs from what is displayed.

[green::b]Practice Oscillator Overview[white::-]
This is a simple audio oscillator. It takes its tone from the Timing screen (Ctrl-T) - nothing else; it uses the Echo (Ctrl-E) Echo Port settings at the bottom of the screen - nothing else. Besides sharing the options stated above, the feature is independent of all other features and options.

Use it to troubleshooting or confirming your comport wiring, the Echo Port settings, the tone and volume of your audio. Or naturally for manual keying warm up before Echo or teaching CW. This will not support a keyer or bug becasue there is concept or timing, decoding, grading.

[green::b]Echo Overview[white::-]

This feature is unique within YAMA. It is the only feature that enhances sending (though it can provide copy practice when combined with other modes), requires an external keying device (no mouse or keyboard) and its associated hardware setup, and is fully interactive between the app and the user. 

Echo is designed to actively build and improve your sending skills by having you echo back perfectly timed Morse code generated by YAMA. To align with head word buffering and Instant Word Recognition (IWR) training (NOT YAMA's IWR feature), the interaction is based on [yellow::b]groups of one or more words[-:-:-], not individual characters (e.g., <word1><wordspace><word2>). Of course, you can have a group as short as one character, and a word count per group as low as one as wellΓÇöthis provides rapid two-way interaction between the app and your keying.

The length of these groups is unrestricted and depends entirely on your chosen input stream; it could be "E", "RST", "555-1212", or "MISSISSIPPI". Note that selections on the Options screen ([yellow::b]Ctrl-O[-:-:-]), such as Use Prosigns or Extended Punctuation, act as filters to automatically allow or delete specific characters from your input stream.

To begin, enable Echo in Options ([yellow::b]Ctrl-O[-:-:-]) and start the session with Play ([yellow::b]Ctrl-P[-:-:-]). The timing method is limited to the Standard and Wordsworth options (more on how Wordsworth functions in Echo). (The IWR feature, if enabled on the Timing screen, is ignored during Echo).
- Listen & Echo: YAMA plays the audio (with an optional sidetone available in the [yellow]Ctrl-T[-:-:-] Timing menu), and you echo it back.
- Success: If your input matches exactly, YAMA automatically advances to the next group.
- Mismatch: YAMA pauses. Press Backspace to retry the group, or Enter to skip and advance.
- Live Controls: Press Spacebar at any time to hide or unhide the main YAMA screen, and [yellow::b]Ctrl-D[-:-:-] to toggle the DataStats overlay. The DataStats is not updated until the group is finished so as not to be distracting. The grading of your keying is by elements: dits, dahs, intra-element space, etc. The screen shows the current (or just completed) group on the left, the summary of the session on the right, and a comparison to the standard timing based on the selected character wpm that you are trying to emulate. Note that your grading is based on the Tolerance setting you have configured. 

[green::b]Timing & The Keying Window[-:-:-]
Understanding the timeline of a Echo group is critical for a smooth UX. The sequence flows linearly:
1. YAMA Plays: The application sends the group at your configured speed.
2. Word Space & Alert: YAMA waits one standard word space, then plays a short alert tone (if configured) to signal your turn.
3. Hesitation Window: You have a generous 2-second window to mentally process the word and strike the paddle for your first element. 
4. Keying Window: The moment you close the paddle, the hesitation timer stops, and your performance clock begins.
5. Element Accuracy: Individual elements you key (dits, dahs, and internal spaces) must meet a settable accuracy tolerance to be decoded correctly. 
6. Padding & Evaluation: You must complete the entire group within the exact time YAMA took to send it, plus your chosen Key Padding (Strict, Normal, Relaxed, or Forgiving). When YAMA detects silence equal to 3 standard word spaces, it closes the window and grades your input.

Tip for progression: As your keying practice improves, you can force yourself to match YAMA more strictly by reducing
the element Tolerance, lowering the Key Padding, and increasing the number of words in a group, all set on the 
[green::b]Echo[-:-:-] screen. It may be humbling, or at least frustrating, if you start too aggressively. Since this is a 
PC app, not a dedicated microprocessor, it might be impossible to get down to a 5% tolerance (feedback welcome).

[green::b]Hardware Requirements & Setup[-:-:-]
Echo requires an RS-232 COM port adapter. The physical wiring is radically simple: it requires exactly two wires. No jumpers, no common grounds, and no resistors are needed. You simply wire your CW key to bridge one output pin to one input pin, making a series loop from the DB9 source pin through your key device and back to the monitor pin.

[green::b]DB9 Adapter Connection:[-:-:-]
- Outputs (Power Sources): Pin 4 (DTR) or Pin 7 (RTS)
- Inputs (Listeners): Pin 8 (CTS), Pin 6 (DSR), Pin 1 (CD), or Pin 9 (RI)
Match the two wires on the adapter to the "Key Line Interface" setting on the Echo page.
The Default CTS-DTR is most commonly used.
(Pin numbers are listed on the Echo options page. Viewed from the back (solder) side of a DB9, with the wide edge on top, the top row from LEFT to RIGHT is 1 2 3 4 5; the lower row is 6 7 8 9.) Match your wires to the option setting.
A simpler alternative is to buy a DB9 adapter with a built-in terminal block to screw down the wires; they cost about $5. 

[green::b]Echo Quick Test[-:-:-]
1. Ctrl-T Timing screen: set a Character Speed and Tone. Then SAVE.
2. Ctrl-E Echo screen: set sidetone, set alert, and set KeyLineMode for the wiring/pins you soldered on your
DB9. Set Polarity to standard, uncheck Parasitic Power. SAVE.
3. Ctrl-O Options: Check Echo. SAVE.
4. On Text Input (main screen), type a few words. 
5. Of course, insert your COM port with the key device connected.
6. Ctrl-P to start Play. YAMA should sound and display an input word(s); when it stops the lower left side of
the screen should have a green "Key now" as well as a cursor on the Text Input/Output area, and you should have heard a brief alert tone. Quickly, attempt to key back to YAMA.

If you heard some CW before you got a TIMED OUT message, you have verified the minimum setup.
If you heard a long DAH not in sync with your sending, then the Polarity option on the Echo
screen needs to be changed. If the status line says NO INPUT, you didn't key anything or there's a connectivity issue.
The default COM PORT is 3. Your PC may have others; see the drop-down choices.
Depending on when you insert the COM PORT, it's possible the Echo Port may show none; try the button at the bottom labeled
Refresh Ports.

[green::b]Echo Screen (Ctrl-E) - Settings[-:-:-]
Setting                | Description
-----------------------|---------------------------------------------------------------------------------------------
Tolerance (%)          | % a symbol element (dit, dah, space, etc.) can vary from the expected value.
Echo Word Count (1-20) | Number of words played (default 1) and to key/echo back in each group.
Random Word Count      | Allows count from 1 to Echo Word Count.
Key Now Alert Tone     | Plays a short audible prompt (~.5 dit) for you to begin keying. There is also a Status Line 
                       | prompt "Key now..." as well as a brief cursor below the words from YAMA. Do NOT key before 
                       | the prompt - the decoder will not be active, nor will the sidetone.
Alert Tone Frequency   | Allows the alert tone to differ from audible code.
Error Tone             | Plays Alert Tone Freq for about 0.5 Dah time on a keying error.
Key Padding            | Once you start to key, you have the same amount of time that YAMA took to send you the 
                       | word(s), plus 4 levels of padding given as descriptive names.
SideTone               | Use the PC sidetone for keying using the same tone as set on the Timing screen.
Visual Feedback        | If set, your keyed symbols line up below what YAMA sent. Two identical lines indicate
                       | perfect match; * indicates invalid Morse (i.e., 7 dits) (more on this later).
Perfect Match Message  | Brief pause on match, with a green status message. Helpful is Visual Feedback is off.
Echo Port              | A COM port to connect your device (straight key, keyer, bug).
Key Line Interface     | Which leads in the COM port are being used.
Key Line Polarity      | Whether key up is silent (standard) or plays a tone (down is inverted).
Parasitic Power        | If needed by the COM port cable (e.g., if the cable uses opto-isolators).

[green::b]Timing Screen (Ctrl-T) - Timing[-:-:-]
Setting                | Description
-----------------------|---------------------------------------------------------------------------------------------
Timing Method          | Standard, Farnsworth, or Wordsworth. Choice controls if/when the next few options apply.
Character Speed        | Applies to all of the above methods.      
End Ramp Speed         | If set, must be greater than the above. Will have an audio session increase in speed 
                       | linearly during the session. Speed will not change with a word.
Effective Speed        | A speed lower than Char Speed, used for Farnsworth or Wordsworth spacing.
Tone                   | For YAMA-played audio; for optional sidetone in the Echo Mode.
Use IWR Mode           | An on/off feature toggle, placed here since it relates to Character speed (if the played 
                       | audio sounds your chosen IWR words at a faster rate than other words). 
Speed                  | The character speed for IWR matched words.
Tone                   | Likely best set the same as the previous tone.

[green::b]Impairments Screen (Ctrl-I) - Audio-impacting impairments[-:-:-]
Setting                | Description
-----------------------|-------------------------------------------------------------------------
Static (QRN)           | Injects constant background hiss and random lightning crashes.
Fading (QSB)           | Simulates a slow ionospheric roll, dipping and recovering volume.
Tone Drift             | Simulates an unstable oscillator, bending the pitch up and down.
Speed Drift            | Simulates a tired operator by slowly expanding/contracting the timing.
Key Clicks             | Injects a harsh electrical spark at the start and end of elements.
Brown Noise            | Not an impairment. Can increase mental focus by masking other noise.
Pink Noise             | Not an impairment. Can increase learning (some clinical evidence).

[yellow::b]Note that you can make changes to the currently playing audio via the Timing or Audio screens.[-:-:-]
[yellow::b]Note: the two noise tones are hypothetical, not specific to Morse code.[-:-:-]

[green::b]WAV File Export (Ctrl-W)[-:-:-]
YAMA exports 16-bit Mono audio. The wave screen lets you select a target directory for the created wave files; if the path does not exist, it will create it. The approximate play time for the chosen speed and the corresponding size are shown. You can also specify (actually limit) the number of files. If your input is a large novel, you can certainly limit the output to a handful of practice files. If you are emailing the completed files to yourself so that you can play them on a cell phone, capping the file size near 10MB should be reasonable.

[green::b]IWR - Instant Word Recognition Feature[-:-:-]
This is a head-copy related feature. It matches words (actually any space-separated string of supported characters, e.g., the qsl 73 cul) in the input text, override the chosen timing mode (Standard, Farnsworth, Wordsworth and the associated speed/tone), and plays the matched word (from the yamaIWR.txt file) at an increased speed with standard timing. To do this, you must create a [blue]yamaIWR.txt[-:-:-] file. A sample file has been created in your OS's standard configuration file directory ($HOME\AppData\Roaming\YAMA for Windows). The file will be editable (cursor keys, home/end, pg up/down, backspace, delete/insert) from the Timing screen ([yellow::b]Ctrl-T[-:-:-]), or you may create a local one in the same directory that YAMA is launched from. This one will take priority, but you will have to edit it with Notepad, vi, emacs, or whatever your favorite text editor is (not a word processor, unless it has a save as txt option). The file should list one word per line (any case, any order); a [yellow::b]'#'[-:-:-] at the start of a line tells YAMA to ignore that line. If you choose to also match the word if it's immediately followed by [yellow::b], . ? : [-:-:-] as well as the bare word, this is indicated by a trailing asterisk (e.g., qsl* matches: qsl qsl? qsl. qsl: qsl, Note this is the only supported use of the asterisk in the app). The IWR feature as described is ignored if you have chosen either Word Builder, Randomize Word, or Echo in the Options menu since you would never get a match (or make sense in Echo). A small purposeful interaction with IWR speed is as follows: if you chose Word Builder and have IWR enabled, when Word Builder has completed constructing the word (as in: t te tes test) you will have one more sounding of the final word, but now at IWR speed.

Let's use a more concrete example.  Setting: standard timing method, characters at 20 wpm, and IWR Mode unchecked.

Now with "THE QUICK BROWN FOX JUMPED OVER THE LAZY DOGS BACK" loaded into the Text Input/Output screen, hitting Ctrl-P will PLAY that sentance at 20wpm, standard timing. Now on the Timing screen, check the IWR Mode checkbox, and set the IWR speed to 25 wpm, use the Edit IWR button to open the yamaIWR.txt file, cursor down to a blank line and enter the following 3 words, one per line: THE AND FOX, then SAVE and SAVE on the Timing screen. Now use PLAY and hear the difference. The words shown in [yellow::b]yellow[-:-:-] will be played at the 20 wpm Character Speed. The words in [red::b]red[-:-:-] match the IWR words so they are played at the IWR speed.

[red::b]THE[yellow] QUICK BROWN[red] FOX[yellow] JUMPED OVER[red] THE[yellow] LAZY DOGS BACK[-:-:-]

With a much larger input, and/or multiple practice sessions, the word THE would be more comfortable as a complete new sound at a higher speed, FOX while short and unique would not likely be found very often so not a good candidate, AND had no matches in this small test sample, but it was included to demonstate that an unmatched word is no problem, and it certainly would be matched in other practice. After this practice, the DataStats (Ctrl-D) would show you the counts for all the matches. Why do this? Nobody send like this; thats correct but its a mechanism to start building up a "dictionary" of familar sounds at a much higher speed then your normal practice, just as most hams can recognize CQ, or maybe RST 5NN at a much faster rate.


[yellow::b]Note: Using the high end of the 2K Tone limit may impact the audio profile for QRQ speeds; let your ears guide your choice, rather than letting the app limit you.[-:-:-]

Experiment and you will quickly understand the capabilities. 

You can see the app is not for complete beginners, nor does it attempt to compete with many excellent training apps such as Precision CW Tutor & Fistcheck, G4FON, LCWO.net, LICW.org, etc., but rather bundles a number of practice features in a single app.

You can email me at wa2nfn@gmail.com if you find something that needs clarification, a bug, a typo, or if a numerical limit causes you an issue. The sound library will not work on MAC, so that's not a consideration; a mouse will never be supported by the UI library, so that is not a consideration.

73 and best of luck on your CW journey.
[white::b]WA2NFN
[-:-:-]`

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

	layout.SetBorder(true).SetTitle(" Help ")
	layout.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	pages.AddPage("help", createModal(layout, 120, 26), true, true)
	app.SetFocus(tv)
}

func showAbout() {
	app.EnableMouse(true)
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
func showFile(app *tview.Application, pages *tview.Pages, inputArea *tview.TextArea) {
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	list.SetBorder(false)

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

	onSelect := func(main string) {
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
			app.EnableMouse(true) // EXIT DOOR 1: Error reading dir
			pages.RemovePage("file")
			app.SetFocus(inputArea)
			return
		}

		if info.IsDir() {
			currentDir = full
			populate(currentDir)
			return
		}

		currentInputFile = name
		currentFileDir = currentDir

		app.EnableMouse(true) // EXIT DOOR 2: File selected

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

			txt := parser.NormalizeText(string(data))

			if config.User.UseSkip {
				parser.SetSkipList(config.User.SkipList, morse.ProSignTable)
				txt = parser.ApplySkip(txt)
			}

			actualText := parser.FilterValidMorse(txt, morse.MorseTable)

			normalized := strings.Join(strings.Fields(actualText), " ")
			normalized = strings.ToUpper(normalized)

			app.QueueUpdateDraw(func() {
				inputArea.SetChangedFunc(nil)

				isProgrammaticUpdate = true
				isResized = false
				preResizeSnapshot = ""
				inputArea.SetText(normalized, false)
				isProgrammaticUpdate = false

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
	}

	// Double-click or Enter on a list item
	list.SetSelectedFunc(func(i int, main string, sec string, r rune) {
		onSelect(main)
	})

	form := tview.NewForm().
		AddButton("Cancel", func() {
			app.EnableMouse(true) // EXIT DOOR 3: Cancel button clicked
			pages.RemovePage("file")
			app.SetFocus(inputArea)
		})
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	applyFocusStyles(form)

	footerView := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetDynamicColors(true)
	footerView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	footerView.SetText("[yellow]Cursor Up/Down to Move  •  Enter to Select[-]")

	layout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(list, 0, 1, true).
		AddItem(form, 3, 1, false).
		AddItem(footerView, 1, 1, false)

	layout.SetBorder(true).SetTitle(" Input Files ")
	layout.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
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

	modalGrid := createModal(layout, 55, 26)

	// THE NUCLEAR OPTION
	// createModal natively calls app.EnableMouse(true). We immediately override it 
	// and physically tell the terminal emulator to stop sending mouse clicks.
	app.EnableMouse(false)

	layout.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			app.EnableMouse(true) // EXIT DOOR 4: Escape key
			pages.RemovePage("file")
			app.SetFocus(inputArea)
			return nil
		}
		return event
	})

	pages.AddPage("file", modalGrid, true, true)
	app.SetFocus(list)
}
