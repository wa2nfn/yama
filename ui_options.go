package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"yama/config"
	"yama/morse"
	"yama/parser"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// OPTIONS_MENU
func showOptions() {
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	form.SetItemPadding(0)

	form.SetBorderPadding(0, 0, 1, 1)

	useProsignsCb := tview.NewCheckbox().SetLabel("ProSign Support")
	extendedPuncCb := tview.NewCheckbox().SetLabel("Extended Punctuation")
	europeanCharsCb := tview.NewCheckbox().SetLabel("European & Esperanto Chars")
	useSkipCb := tview.NewCheckbox().SetLabel("Use Skip Chars List")

	skipListInput := tview.NewInputField().SetLabel("    Skip List").SetFieldWidth(35)
	skipListInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	skipListInput.SetPlaceholder("e.g. XYZ789<SK>").SetPlaceholderTextColor(tcell.ColorYellow)
	skipListInput.SetInputCapture(forceUppercaseInputCapture())

	delayOptions := []string{"0", "1", "2", "3", "4", "5"}
	delayDropDown := tview.NewDropDown().SetLabel("Start-Up Delay (sec)").SetOptions(delayOptions, nil)

	repeatOptions := []string{"2", "3", "4", "5", "6", "7", "8", "9"}
	repeatDropDown := tview.NewDropDown().SetLabel("Char Repeat Limit").SetOptions(repeatOptions, nil)

	wordOrderCb := tview.NewCheckbox().SetLabel("Random Word Order")
	randomizeWordsCb := tview.NewCheckbox().SetLabel("Randomize Word Chars")

	wordBuilderCb := tview.NewCheckbox().SetLabel("[#00BFFF::b]Word Builder Mode[::-]")

	wordSeparatorInput := tview.NewInputField().SetLabel("    Word Separator").SetFieldWidth(15)
	wordSeparatorInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	wordSeparatorInput.SetPlaceholder("e.g. . or ,").SetPlaceholderTextColor(tcell.ColorYellow)
	wordSeparatorInput.SetInputCapture(forceUppercaseInputCapture())
	wordBuilderSortCb := tview.NewCheckbox().SetLabel("    Sort: Short First")
	syllableExpansionCb := tview.NewCheckbox().SetLabel("Syllablize Words")

	textBuilderCb := tview.NewCheckbox().SetLabel("[#00BFFF]Text Builder Mode[::-]")
	textSeparatorInput := tview.NewInputField().SetLabel("    Text Separator").SetFieldWidth(15)
	textSeparatorInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	textSeparatorInput.SetPlaceholder("e.g. <BT>.,+").SetPlaceholderTextColor(tcell.ColorYellow)
	textSeparatorInput.SetInputCapture(forceUppercaseInputCapture())
	textBuilderSortCb := tview.NewCheckbox().SetLabel("    Sort: Short First")

	textWordCountList := []string{"2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20", "21", "22", "23", "24", "25"}
	textWordCntDrop := tview.NewDropDown().SetLabel("    Word Count (2-25)").SetOptions(textWordCountList, nil)

	flashcardCb := tview.NewCheckbox().SetLabel("[#00BFFF]Flashcard Mode[::-]")
	flashWordCountList := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20"}
	flashWordCountDrop := tview.NewDropDown().SetLabel("    Word Count (1-20)").SetOptions(flashWordCountList, nil)
	flashRandomCountCb := tview.NewCheckbox().SetLabel("    Random Value")

	echoCb := tview.NewCheckbox().SetLabel("[#00BFFF]Echo Sending Mode[::-]")

	startMsgCb := tview.NewCheckbox().SetLabel("Use Start Message")
	startMsgInput := tview.NewInputField().SetLabel("    Message Text").SetFieldWidth(35)
	startMsgInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	startMsgInput.SetPlaceholder("VVV <KA>").SetPlaceholderTextColor(tcell.ColorYellow)
	startMsgInput.SetInputCapture(forceUppercaseInputCapture())

	endMsgCb := tview.NewCheckbox().SetLabel("Use End Message")
	endMsgInput := tview.NewInputField().SetLabel("    Message Text").SetFieldWidth(35)
	endMsgInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	endMsgInput.SetPlaceholder("<AR>").SetPlaceholderTextColor(tcell.ColorYellow)

	endMsgInput.SetInputCapture(forceUppercaseInputCapture())
	euroSkipList := config.User.EuropeanSkipList

	resetState := func() {
		startMsgCb.SetChecked(config.User.StartMsg)

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
		europeanCharsCb.SetChecked(config.User.UseEuropeanChars)
		useSkipCb.SetChecked(config.User.UseSkip)
		skipListInput.SetText(config.User.SkipList)
		wordOrderCb.SetChecked(config.User.WordOrder)
		randomizeWordsCb.SetChecked(config.User.RandomizeWords)
		wordBuilderCb.SetChecked(config.User.WordBuilder)
		wordBuilderSortCb.SetChecked(config.User.WordBuilderSort)
		wordSeparatorInput.SetText(config.User.WordSeparator)

		wordSeparatorInput.SetText(strings.TrimSpace(config.User.WordSeparator))
		textBuilderCb.SetChecked(config.User.TextBuilder)
		textSeparatorInput.SetText(config.User.TextSeparator)
		textSeparatorInput.SetText(strings.TrimSpace(config.User.TextSeparator))
		syllableExpansionCb.SetChecked(config.User.SyllableExpansion)
		textBuilderSortCb.SetChecked(config.User.TextBuilderSort)
		flashcardCb.SetChecked(config.User.Flashcard)
		flashRandomCountCb.SetChecked(config.User.FlashRandomCount)

		echoCb.SetChecked(config.User.Echo)

		twIdx := 0
		for i, opt := range textWordCountList {
			if opt == fmt.Sprintf("%d", config.User.TextWordCount) {
				twIdx = i
				break
			}
		}
		textWordCntDrop.SetCurrentOption(twIdx)

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

		fIdx := 0
		for i, opt := range flashWordCountList {
			if opt == fmt.Sprintf("%d", config.User.FlashWordCount) {
				fIdx = i
				break
			}
		}
		flashWordCountDrop.SetCurrentOption(fIdx)

		euroSkipList = config.User.EuropeanSkipList
	}

	resetState()

	form.AddFormItem(wordBuilderCb)
	form.AddFormItem(wordSeparatorInput)
	form.AddFormItem(wordBuilderSortCb)
	form.AddFormItem(textBuilderCb)
	form.AddFormItem(textSeparatorInput)
	form.AddFormItem(textWordCntDrop)
	form.AddFormItem(textBuilderSortCb)
	form.AddFormItem(flashcardCb)
	form.AddFormItem(flashWordCountDrop)
	form.AddFormItem(flashRandomCountCb)
	form.AddFormItem(echoCb)
	form.AddTextView(" ", "", 0, 1, false, false)

	form.AddFormItem(syllableExpansionCb)
	form.AddFormItem(wordOrderCb)
	form.AddFormItem(randomizeWordsCb)

	form.AddFormItem(useProsignsCb)
	form.AddFormItem(extendedPuncCb)
	form.AddFormItem(europeanCharsCb)
	form.AddFormItem(useSkipCb)
	form.AddFormItem(skipListInput)
	form.AddFormItem(repeatDropDown)
	form.AddFormItem(delayDropDown)

	form.AddFormItem(startMsgCb)
	form.AddFormItem(startMsgInput)
	form.AddFormItem(endMsgCb)
	form.AddFormItem(endMsgInput)

	var optionsContainer *tview.Flex

	onSave := func() {
		var errors []string
		isSyl := syllableExpansionCb.IsChecked()
		isRandomizeWords := randomizeWordsCb.IsChecked()
		isWB := wordBuilderCb.IsChecked()
		isTB := textBuilderCb.IsChecked()
		isFC := flashcardCb.IsChecked()
		isSkip := useSkipCb.IsChecked()

		if isRandomizeWords && (isWB || isTB || isFC || isSyl) {
			errors = append(errors, "Randomize Word is not compatiable with Word Builder, Text Builder, Syllablize Words or Flashcard.")
		}
		if isTB && isWB {
			errors = append(errors, "Text Builder and Word Builder are mutually exclusive.")
		}
		if isFC && isWB {
			errors = append(errors, "Flashcard and Word Builder are mutually exclusive.")
		}
		if isFC && isTB {
			errors = append(errors, "Flashcard and Text Builder are mutually exclusive.")
		}
		if isFC && echoCb.IsChecked() {
			errors = append(errors, "Flashcard and Echo are mutually exclusive.")
		}

		rawSkipFields := strings.Fields(skipListInput.GetText())
		skipMap := make(map[string]bool)
		var cleanSkips []string
		for _, w := range rawSkipFields {
			lw := strings.ToUpper(w)
			if !skipMap[lw] {
				skipMap[lw] = true
				cleanSkips = append(cleanSkips, lw)
			}
		}
		formattedSkipList := strings.Join(cleanSkips, " ")

		_, fcStr := flashWordCountDrop.GetCurrentOption()
		fcVal, _ := strconv.Atoi(fcStr)
		if isFC && fcVal < 1 {
			errors = append(errors, "Flashcard Word Count must be 1-25.")
		}

		apply := func() {
			config.User.Playprosigns = useProsignsCb.IsChecked()
			config.User.UseExtendedPunctuation = extendedPuncCb.IsChecked()
			config.User.UseEuropeanChars = europeanCharsCb.IsChecked()
			config.User.UseSkip = isSkip
			config.User.SkipList = formattedSkipList
			config.User.EuropeanSkipList = euroSkipList
			config.User.WordOrder = wordOrderCb.IsChecked()
			config.User.RandomizeWords = isRandomizeWords
			config.User.WordBuilder = isWB
			config.User.TextBuilder = isTB
			config.User.WordBuilderSort = wordBuilderSortCb.IsChecked()
			config.User.TextBuilderSort = textBuilderSortCb.IsChecked()
			config.User.WordSeparator = wordSeparatorInput.GetText()
			config.User.TextSeparator = textSeparatorInput.GetText()

			_, twStr := textWordCntDrop.GetCurrentOption()
			config.User.TextWordCount, _ = strconv.Atoi(twStr)

			config.User.StartMsg = startMsgCb.IsChecked()
			config.User.StartMsgText = startMsgInput.GetText()
			config.User.EndMsg = endMsgCb.IsChecked()
			config.User.EndMsgText = endMsgInput.GetText()
			config.User.SyllableExpansion = isSyl

			dIdx, _ := delayDropDown.GetCurrentOption()
			config.User.StartDelay, _ = strconv.Atoi(delayOptions[dIdx])

			rIdx, _ := repeatDropDown.GetCurrentOption()
			config.User.RepeatLimit, _ = strconv.Atoi(repeatOptions[rIdx])
			config.User.Flashcard = flashcardCb.IsChecked()
			fIdx, _ := flashWordCountDrop.GetCurrentOption()
			config.User.FlashWordCount, _ = strconv.Atoi(flashWordCountList[fIdx])
			config.User.FlashRandomCount = flashRandomCountCb.IsChecked()

			config.User.WordBuilderSort = wordBuilderSortCb.IsChecked()
			config.User.Echo = echoCb.IsChecked()
			config.SaveConfig()

			morse.RebuildMorseTable(config.User.UseExtendedPunctuation, config.User.UseEuropeanChars, config.User.UseSkip, config.User.SkipList, config.User.EuropeanSkipList)

			updateBlueLine()
			pages.RemovePage("options")
			app.SetFocus(inputArea)
		}

		if len(errors) > 0 {
			showErrorModal(errors, func() { app.SetFocus(form) })
		} else {
			if !echoCb.IsChecked() {
				// The actual closeEchoStatsWindow function lives in ui.go
				closeEchoStatsWindow()
			}
			apply()
		}
		refreshUI(currentState)
	}

	onReset := func() {
		resetState()
	}

	form.AddButton("Save", onSave)
	form.AddButton("Reset", onReset)
	form.AddButton("European & Esperanto Char Skip", func() {
		showEuropeanCharSelector(optionsContainer, &euroSkipList, onSave)
	})
	form.AddButton("Cancel", func() {
		pages.RemovePage("options")
		app.SetFocus(inputArea)
	})

	applyFocusStyles(form)
	form.SetBorder(false)

	optionsContainer = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true)

	optionsContainer.SetBorder(true).SetTitle(" Options ")
	optionsContainer.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	optionsContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("options")
			app.SetFocus(inputArea)
			return nil
		}
		if event.Key() == tcell.KeyCtrlS {
			onSave()
			return nil
		}
		return event
	})

	pages.AddPage("options", createModal(optionsContainer, 66, 30), true, true)
	app.SetFocus(optionsContainer)
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
		{'\u00C4', "Ä (Euro: A-umlaut)"},
		{'\u00D6', "Ö (Euro: O-umlaut)"},
		{'\u00DC', "Ü (Euro: U-umlaut)"},
		{'\u00C9', "É (Euro: E-acute)"},
		{'\u00C1', "Á (Euro: A-acute)"},
		{'\u00C5', "Å (Euro: A-ring)"},
		{'\u00C7', "Ç (Euro: C-cedilla)"},
		{'\u00D1', "Ñ (Euro: N-tilde)"},
		{'\u00C0', "À (Euro: A-grave)"},
		{'\u00C8', "È (Euro: E-grave)"},
		{'\u0108', "Ĉ (Esp: C-circumflex)"},
		{'\u011C', "Ĝ (Esp: G-circumflex)"},
		{'\u0124', "Ĥ (Esp: H-circumflex)"},
		{'\u0134', "Ĵ (Esp: J-circumflex)"},
		{'\u015C', "Ŝ (Esp: S-circumflex)"},
		{'\u016C', "Ŭ (Esp: U-breve)"},
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

	container := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true)

	container.SetBorder(true).SetTitle(" European & Esperanto Skips ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	container.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("extCharSelector")
			app.SetFocus(parentContainer)
			return nil
		}
		if event.Key() == tcell.KeyCtrlS {
			onSave()
			return nil
		}
		return event
	})

	pages.AddPage("extCharSelector", createModal(container, 45, 20), true, true)
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
		app.SetFocus(parentContainer)
	}

	form.AddButton("Save", onSave)
	form.AddButton("Cancel", func() {
		pages.RemovePage("iwredit")
		app.SetFocus(parentContainer)
	})

	textArea.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyTab:
			app.SetFocus(form)
			return nil
		case tcell.KeyCtrlS:
			onSave()
			return nil
		case tcell.KeyEsc:
			pages.RemovePage("iwredit")
			app.SetFocus(parentContainer)
			return nil
		}

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

	pages.AddPage("iwredit", createModal(layout, 75, 20), true, true)
}

func showTiming() {
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	form.SetItemPadding(0)

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

	modeDropDown := tview.NewDropDown().SetLabel("Timing Method").SetOptions([]string{"Standard", "Farnsworth", "Wordsworth"}, nil)

	charInput := tview.NewInputField().SetLabel("Character Speed (wpm)").
		SetFieldWidth(3).SetAcceptanceFunc(acceptFloat)
	charInput.SetFieldBackgroundColor(tcell.ColorBlue).
		SetFieldTextColor(tcell.ColorWhite)

	endSpeedInput := tview.NewInputField().SetLabel("    End Ramp Speed (wpm)").
		SetFieldWidth(3).SetAcceptanceFunc(acceptFloat)
	endSpeedInput.SetFieldBackgroundColor(tcell.ColorBlue).
		SetFieldTextColor(tcell.ColorWhite)

	effInput := tview.NewInputField().SetLabel("Effective Speed (wpm)").
		SetFieldWidth(3).SetAcceptanceFunc(acceptFloat)
	effInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	toneInput := tview.NewInputField().SetLabel("Tone (Hz)").
		SetFieldWidth(4).SetAcceptanceFunc(acceptDigits)
	toneInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	iwrCheckbox := tview.NewCheckbox().SetLabel("[#00BFFF]Use IWR Mode[::-]")

	iwrSpdInput := tview.NewInputField().SetLabel("    Speed (wpm)").
		SetFieldWidth(3).SetAcceptanceFunc(acceptFloat)
	iwrSpdInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	iwrToneInput := tview.NewInputField().SetLabel("    Tone (Hz)").
		SetFieldWidth(4).SetAcceptanceFunc(acceptDigits)
	iwrToneInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	formatFloatOnExit := func(input *tview.InputField) {
		input.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			k := event.Key()
			if k == tcell.KeyTab || k == tcell.KeyEnter || k == tcell.KeyBacktab || k == tcell.KeyDown || k == tcell.KeyUp {
				valStr := input.GetText()
				if valStr != "" && valStr != "." {
					if val, err := strconv.ParseFloat(valStr, 64); err == nil {
						rounded := math.Round(val*10) / 10
						input.SetText(strconv.FormatFloat(rounded, 'f', -1, 64))
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

		charInput.SetText(strconv.FormatFloat(config.User.CharacterSpeed, 'f', -1, 64))
		effInput.SetText(strconv.FormatFloat(config.User.EffectiveSpeed, 'f', -1, 64))
		toneInput.SetText(strconv.Itoa(config.User.Tone))
		iwrCheckbox.SetChecked(config.User.IWREnabled)
		iwrSpdInput.SetText(strconv.FormatFloat(config.User.IWRSpeed, 'f', -1, 64))
		iwrToneInput.SetText(strconv.Itoa(config.User.IWRTone))
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

		if optIdx != 0 {
			if effSpd < float64(config.MinEffSpeed) || effSpd > float64(config.MaxEffSpeed) {
				errors = append(errors, fmt.Sprintf("Effective Speed clamped to %d-%d", config.MinEffSpeed, config.MaxEffSpeed))
				if effSpd < float64(config.MinEffSpeed) {
					effSpd = float64(config.MinEffSpeed)
				} else {
					effSpd = float64(config.MaxEffSpeed)
				}
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
			if optIdx != 0 {
				config.User.EffectiveSpeed = effSpd
			}
			config.User.CharacterSpeed = charSpd
			config.User.EndSpeed = endSpd
			config.User.Tone = tone
			config.User.IWREnabled = iwrCheckbox.IsChecked()
			config.User.IWRSpeed = iwrSpd
			config.User.IWRTone = iwrTone
			config.SaveConfig()

			updateBlueLine()
			pages.RemovePage("timing")
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
	form.AddButton("Cancel", func() {
		pages.RemovePage("timing")
		app.SetFocus(inputArea)
	})

	applyFocusStyles(form)
	form.SetBorder(false)

	timingContainer = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true)

	timingContainer.SetBorder(true).SetTitle(" Timing ")
	timingContainer.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	timingContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("timing")
			app.SetFocus(inputArea)
			return nil
		}
		if event.Key() == tcell.KeyCtrlS {
			onSave()
			return nil
		}
		return event
	})

	pages.AddPage("timing", createModal(timingContainer, 42, 13), true, true)
	app.SetFocus(form)
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
		brownLvl, _ := brownNoiseDropDown.GetCurrentOption()
		pinkLvl, _ := pinkNoiseDropDown.GetCurrentOption()

		if brownLvl > 0 && pinkLvl > 0 {
			errorModal := tview.NewModal().
				SetText("Brown Noise and Pink Noise cannot be active at the same time.\n\nPlease set one of them to 'Off'.").
				AddButtons([]string{"OK"}).
				SetDoneFunc(func(buttonIndex int, buttonLabel string) {
					pages.RemovePage("noise_error")
					app.SetFocus(container)
				})

			pages.AddPage("noise_error", errorModal, true, true)
			return
		}

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
	form.AddButton("Cancel", func() {
		pages.RemovePage("impairments")
		app.SetFocus(inputArea)
	})

	applyFocusStyles(form)
	form.SetBorder(false)

	container = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true)

	container.SetBorder(true).SetTitle(" Impairments ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	container.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("impairments")
			app.SetFocus(inputArea)
			return nil
		}
		if event.Key() == tcell.KeyCtrlS {
			onSave()
			return nil
		}
		return event
	})

	pages.AddPage("impairments", createModal(container, 40, 13), true, true)
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

	var randomize bool
	form.AddCheckbox("Randomize:", false, func(checked bool) {
		randomize = checked
	})

	onSave := func() {
		val, err := strconv.Atoi(input.GetText())

		if err != nil || val < 1 {
			val = 1
		} else if val > 99999 {
			val = 99999
		}

		if (val != currentCount || randomize) && currentCount > 0 {
			if !isResized {
				preResizeSnapshot = rawText
				isResized = true
			}

			var newWords []string
			for i := 0; i < val; i++ {
				newWords = append(newWords, words[i%currentCount])
			}

			if randomize {
				if len(newWords) > 1 {
					for i := len(newWords) - 1; i > 0; i-- {
						j := morse.FastIntn(i + 1)
						newWords[i], newWords[j] = newWords[j], newWords[i]
					}
				}
			}

			inputArea.SetText(strings.Join(newWords, " "), false)
			actualText = inputArea.GetText()

			refreshUI(currentState)
		}

		pages.RemovePage("numWords")
		app.SetFocus(inputArea)
	}

	form.AddButton("Save", onSave)

	form.AddButton("Cancel", func() {
		pages.RemovePage("numWords")
		app.SetFocus(inputArea)
	})

	if isResized {
		form.AddButton("Undo", func() {
			inputArea.SetText(preResizeSnapshot, false)
			actualText = inputArea.GetText()

			isResized = false
			preResizeSnapshot = ""

			refreshUI(currentState)
			pages.RemovePage("numWords")
			app.SetFocus(inputArea)
		})
	}

	applyFocusStyles(form)
	form.SetBorder(false)

	container := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true)

	container.SetBorder(true).SetTitle(" Resize Text Input ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	container.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("numWords")
			app.SetFocus(inputArea)
			return nil
		}
		if event.Key() == tcell.KeyCtrlS {
			onSave()
			return nil
		}
		return event
	})

	pages.AddPage("numWords", createModal(container, 45, 10), true, true)
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
				// showSuccessModal is in ui.go
				showSuccessModal(exportDir, generatedNames)
			})
		}()
	}

	cancelFunc := func() {
		pages.RemovePage("waveConfig")
		app.SetFocus(inputArea)
	}

	form.AddButton("Save", saveFunc)
	form.AddButton("Cancel", cancelFunc)

	applyFocusStyles(form)
	form.SetBorder(false)
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	container := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(infoTextView, 5, 1, false).
		AddItem(form, 0, 1, true)

	container.SetBorder(true).SetTitle(" Generate Wave Files ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	container.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			cancelFunc()
			return nil
		}
		if event.Key() == tcell.KeyCtrlS {
			saveFunc()
			return nil
		}
		return event
	})

	// Height bumped to 20 so the buttons aren't squished off screen
	layout := tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(container, 20, 1, true).
			AddItem(nil, 0, 4, false),
			65, 1, true).
		AddItem(nil, 0, 1, false)

	pages.AddPage("waveConfig", layout, true, true)
	app.SetFocus(container)
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
