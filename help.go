package main

import (
	"fmt"
	"os"
	"strings"
	"yama/morse"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func showHelp() {
	app.EnableMouse(true)
	helpText := ` [-:-:-]                    
                                                YAMA Quick Reference

[green::b]Getting Started[-:-:-]
1. Type or paste text directly into the main Input/Output box, OR press [yellow::b]Ctrl-F[-:-:-] to load a .txt file.
2. Press [yellow::b]Ctrl-P[-:-:-] to begin Playback (audio starts immediately at default 20wpm).
3. Navigate menus using TAB, BACKTAB, and the Cursor keys.
4. Press [yellow::b]ESC[-:-:-] at any time to safely cancel/close a menu, or [yellow::b]Ctrl-S[-:-:-] to Save.

[green::b]Global Keyboard Shortcuts[-:-:-]
Key            | Menu Name   | Purpose
---------------|-------------|----------------------------------------------------------------------------------------
Ctrl-A         | About       | App info and License.
Ctrl-D         | DataStats   | Statistics and IWR counts. In Echo, this screen grades your keying timing.
Ctrl-E         | Echo        | Settings for interactive Echo sending practice.
Ctrl-F, F3     | File        | Open a .txt file for playback.
F4             | (No Menu)   | Local Practice Oscillator.
F5             | (No Menu)   | Live Transmitter Mode.
Ctrl-I         | Impairments | Audio impacting impairments (QRN, QSB, Drift, etc.)
Ctrl-K         | KeyDevice   | Hardware/interface routing for keying devices.
Ctrl-L         | cLear       | Clear the current text input.
Ctrl-M         | (No Menu)   | Hot-key to instantly Mute/Unmute YAMA's audio output.
Ctrl-N         | NumWords    | Iteratively copies or truncates the current text length.
Ctrl-O         | Options     | Parser, messaging, and text processing mode toggles.
Ctrl-P         | Play/Pause  | Start or pause the current loaded input text.
Ctrl-Q         | Quit        | Exit YAMA completely.
Ctrl-S         | Stop        | Halt playback immediately (cannot be resumed).
Ctrl-T         | Timing      | Speed, Tone, and IWR (Instant Word Recognition) settings.
Ctrl-W         | Wave        | Export current text to .wav file(s).
ESC            | Close       | Cancel/Close menus without saving.
Spacebar       | Hide/Unhide | Toggle text visibility during audio playback.

[green::b]Supported Characters & ProSigns[-:-:-]
Letters [yellow::b]A-Z[-:-:-], Numbers [yellow::b]0-9[-:-:-], Basic Punctuation: [yellow::b]. , ? /[-:-:-]
Extended Punctuation (Enable in Options): [yellow::b]: ; " @ ' ( ) $ ! \ [-:-:-]
Supported ProSigns: [yellow::b]<AR> <AS> <BT> <KA> <SK> <VA> <VE> <SN> <BK> <HH> <DU> <SOS> <CH>[-:-:-]
(Keyboard equivalents [yellow::b]+ = -[-:-:-] will always play regardless of the ProSign toggle).

[green::b]Options Screen (Ctrl-O)[-:-:-]
Note: On some terminals, bottom fields may be hidden. Use TAB to scroll down.
Setting               | Description
----------------------|----------------------------------------------------------------------------------------------
Word Builder Mode     | Plays words progressively (e.g., T, TH, THE) for building head buffer comprehension. 
Word Separator        | Specific character/ProSign injected between words.
Sort                  | Sorts input words in short-to-long order.
Text Builder          | Plays input words progressively (e.g., 11 22 33 44 plays as: 11 11 22 11 22 33...).
Word Count            | Limits the count of words used by Text Builder (2-25).
Text Separator        | Specific character/ProSign injected between text iterations.
Flashcard             | Plays word(s) and pauses. Hit Enter for next word, Backspace to replay.
WordCount/Random      | Number of words per flash (1-20). Randomizes up to this count.
Echo Mode             | Enables echo-back sending practice using external key device (See Ctrl-E).
Syllablize Words      | Breaks multi-syllable words into separate elements (e.g., DIF FI CULT).
Random Word Order     | Shuffles the playback order of the entire document's words.
Randomize Word Chars  | Scrambles character order within a word.
ProSign / Ext. Punct. | Toggles support for bracketed ProSigns and extended punctuation.
European Characters   | Toggles support for European & Esperanto characters.
Use Skip Char List    | Enable to silently ignore characters defined in the Skip List.
Skip List             | Define specific characters to skip. [yellow::b]Note:[-:-:-] Adding (') auto-expands contractions.
Char Repeat Limit     | Caps consecutive repeating characters to prevent runaway text.
StartUp Delay         | Countdown timer (seconds) before playback begins.
Start/End Message     | Toggles/Defines custom text to play at the very start or end of the session.

[green::b]Echo Mode & Keying (Ctrl-E & Ctrl-K)[-:-:-]
Requires an RS-232 COM port adapter connected to a key device. (See full HTML manual for wiring).
1. YAMA sends a group of words at your configured speed.
2. YAMA waits one standard word space, plays an alert tone, and starts the clock.
3. You echo the group back perfectly. YAMA grades your elements (dits, dahs, spaces) against your Tolerance %.
4. If successful, YAMA advances. If not, press Backspace to retry or Enter to skip.

Echo Screen Settings (Ctrl-E):
Setting                | Description
-----------------------|---------------------------------------------------------------------------------------------
Tolerance (%)          | Allowable variation for a symbol element (dit, dah, space) before it is marked invalid.
Echo Word Count        | Number of words played (and expected back) per group.
Key Now Alert / Freq   | Short audible prompt signaling the start of your keying window.
Error Tone             | Audible buzz played immediately upon a keying error.
Key Padding            | Grace time added to the window allowed to complete the group (Strict to Forgiving).
SideTone               | Toggle PC audio sidetone while keying.
Visual Feedback        | Displays your decoded characters live on screen underneath YAMA's text.
Perfect Match Message  | Brief pause and green status message on success.

KeyDevice Screen Settings (Ctrl-K):
Setting                 | Description
------------------------|---------------------------------------------------------------------------------------------
Key Device Type         | Input hardware: Straight/Bug, Cootie, Paddle, Iambic, Ultimatic.
Key / Transmitter Port  | Hardware COM port selection for input (reading) and output (keying radio).
Key Adapter Interface   | Selects active COM port pins (e.g., CTS-DTR).
Polarity / Parasitic    | Inverts key-down logic / supplies constant power for opto-isolators.

[green::b]Timing Screen (Ctrl-T)[-:-:-]
Setting                | Description
-----------------------|---------------------------------------------------------------------------------------------
Timing Method          | Standard, Farnsworth, or Wordsworth.
Character Speed        | Base speed applied to all methods.
End Ramp Speed         | Linearly increases speed during the session to reach this target.
Effective Speed        | Stretches character/word spacing for Farnsworth/Wordsworth modes.
Tone                   | YAMA audio frequency (also used for Echo sidetone).
Use IWR Mode           | Enables Instant Word Recognition (matches words in yamaIWR.txt to play at IWR Speed).
IWR Speed / Tone       | Dedicated speed and tone for recognized IWR words.

[green::b]Impairments (Ctrl-I)[-:-:-]
Simulates real-world RF conditions: Static/QRN (hiss/crashes), Fading/QSB (volume dipping), Tone Drift, Speed Drift, Key Clicks. Brown & Pink noise options are also available for mental focus masking.

[green::b]IWR (Instant Word Recognition) Details[-:-:-]
To use IWR, define your target words in [blue]yamaIWR.txt[-:-:-].
- One word per line.
- Use a [yellow::b]#[-:-:-] at the start of a line to comment/ignore it.
- Use a trailing [yellow::b]*[-:-:-] (e.g., [blue]qsl*[-:-:-]) to match the word even if it has trailing punctuation (qsl. qsl?).
IWR is disabled if Word Builder, Randomize Word, or Echo are active.
`

	tv := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText).
		SetScrollable(true)

	tv.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	form := tview.NewForm().
		AddButton("Close", func() {
			pages.RemovePage("help")
			app.SetFocus(inputArea)
		}).
		AddButton("Save to yama_help.html", func() {
			app.EnableMouse(false)
			defer app.EnableMouse(true)

			// 1. Escape HTML brackets
			htmlText := strings.ReplaceAll(helpText, "<", "&lt;")
			htmlText = strings.ReplaceAll(htmlText, ">", "&gt;")

			// 2. Convert the 4 remaining tview color tags to HTML spans
			htmlText = strings.ReplaceAll(htmlText, "[yellow::b]", "<span style='color: #FFD700; font-weight: bold;'>")
			htmlText = strings.ReplaceAll(htmlText, "[green::b]", "<span style='color: #00AA00; font-weight: bold;'>")
			htmlText = strings.ReplaceAll(htmlText, "[red::b]", "<span style='color: #FF0000; font-weight: bold;'>")
			htmlText = strings.ReplaceAll(htmlText, "[blue]", "<span style='color: #00BFFF;'>")
			htmlText = strings.ReplaceAll(htmlText, "[-:-:-]", "</span>")

			finalHTML := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>YAMA Quick Reference</title>
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

        /* Cleaned up print CSS for only active colors */
        @media print {
            body, .container { background-color: white; color: black; box-shadow: none; border: none; padding: 0; }
            span { font-weight: bold !important; }
            span[style*="color: #FFD700"] { color: #8B8B00 !important; }
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
				// Note: We leave the #FFFF55 tag here because it's rendering to the TUI status line, not the HTML file!
				statusLine.SetText(" [#FFFF55]Quick Reference exported to " + exportPath + ".[-]")
			} else {
				statusLine.SetText(" [#FFFF55]Failed to export HTML file.[-]")
			}
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
