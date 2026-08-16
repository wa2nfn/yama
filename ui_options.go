package main

import (
	"fmt"
	"strconv"
	"strings"
	"yama/config"
	"yama/morse"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// OPTIONS_MENU
func showOptions() {
	form := tview.NewForm()
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	form.SetItemPadding(0)

	useProsignsCb := tview.NewCheckbox().SetLabel("Play ProSigns")
	extendedPuncCb := tview.NewCheckbox().SetLabel("Extended Punctuation")
	europeanCharsCb := tview.NewCheckbox().SetLabel("European & Esparanto Chars")
	useSkipCb := tview.NewCheckbox().SetLabel("Use Skip")

	skipListInput := tview.NewInputField().SetLabel("    Skip List").SetFieldWidth(35)
	skipListInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	skipListInput.SetPlaceholder("e.g. XYZ789<SK>").SetPlaceholderTextColor(tcell.ColorYellow)
	skipListInput.SetInputCapture(forceUppercaseInputCapture())

	delayOptions := []string{"0", "1", "2", "3", "4", "5"}
	delayDropDown := tview.NewDropDown().SetLabel("Start Delay (sec)").SetOptions(delayOptions, nil)

	repeatOptions := []string{"2", "3", "4", "5", "6", "7", "8", "9"}
	repeatDropDown := tview.NewDropDown().SetLabel("Repeat Limit").SetOptions(repeatOptions, nil)

	wordOrderCb := tview.NewCheckbox().SetLabel("Random Order")
	randomizeWordsCb := tview.NewCheckbox().SetLabel("Randomize Words")

	wordBuilderCb := tview.NewCheckbox().SetLabel("Word Builder")
	wordSeparatorInput := tview.NewInputField().SetLabel("    Word Separator").SetFieldWidth(35)
	wordSeparatorInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	wordSeparatorInput.SetPlaceholder("e.g. <BT>.,+").SetPlaceholderTextColor(tcell.ColorYellow)
	wordSeparatorInput.SetInputCapture(forceUppercaseInputCapture())
	wordBuilderSortCb := tview.NewCheckbox().SetLabel("    Sort")
	syllableExpansionCb := tview.NewCheckbox().SetLabel("Syllablize Words")

	textBuilderCb := tview.NewCheckbox().SetLabel("Text Builder")
	textSeparatorInput := tview.NewInputField().SetLabel("    Text Separator").SetFieldWidth(35)
	textSeparatorInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	textSeparatorInput.SetPlaceholder("e.g. <BT>.,+").SetPlaceholderTextColor(tcell.ColorYellow)
	textSeparatorInput.SetInputCapture(forceUppercaseInputCapture())
	textBuilderSortCb := tview.NewCheckbox().SetLabel("    Sort")

	textWordCnt := tview.NewInputField().
		SetLabel("    Word Count (2-25) ").
		SetFieldWidth(5).
		SetText("2").
		SetAcceptanceFunc(tview.InputFieldInteger)
	textWordCnt.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	// ONLY THIS: restore default when leaving the field empty
	textWordCnt.SetDoneFunc(func(key tcell.Key) {
		if strings.TrimSpace(textWordCnt.GetText()) == "" {
			textWordCnt.SetText("2")
		}
	})

	flashcardCb := tview.NewCheckbox().SetLabel("Flashcard")
	flashWordCountList := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}
	flashWordCountDrop := tview.NewDropDown().SetLabel("    Word Count (1-20)").SetOptions(flashWordCountList, nil)
	flashRandomCountCb := tview.NewCheckbox().SetLabel("    Random Value")
	echoList := []string{"0", "15", "25", "35", "45"}
	echoDrop := tview.NewDropDown().SetLabel("    Echo Tolerance(%) (0,15,25,35,45)").SetOptions(echoList, nil)

	startMsgCb := tview.NewCheckbox().SetLabel("Use Start Msg")
	startMsgInput := tview.NewInputField().SetLabel("    Start Msg Text").SetFieldWidth(30)
	startMsgInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	startMsgInput.SetPlaceholder("VVV <KA>").SetPlaceholderTextColor(tcell.ColorYellow)
	startMsgInput.SetInputCapture(forceUppercaseInputCapture())

	endMsgCb := tview.NewCheckbox().SetLabel("Use End Msg")
	endMsgInput := tview.NewInputField().SetLabel("    End Msg Text").SetFieldWidth(30)
	endMsgInput.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)
	endMsgInput.SetPlaceholder("<AR>").SetPlaceholderTextColor(tcell.ColorYellow)

	endMsgInput.SetInputCapture(forceUppercaseInputCapture())
	euroSkipList := config.User.EuropeanSkipList

	// RESET
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
		// Trim spaces to ensure empty strings trigger the placeholder
		wordSeparatorInput.SetText(strings.TrimSpace(config.User.WordSeparator))
		textBuilderCb.SetChecked(config.User.TextBuilder)
		textSeparatorInput.SetText(config.User.TextSeparator)
		textWordCnt.SetText(fmt.Sprintf("%d", config.User.TextWordCount))
		textSeparatorInput.SetText(strings.TrimSpace(config.User.TextSeparator))
		textBuilderCb.SetChecked(config.User.TextBuilder)
		syllableExpansionCb.SetChecked(config.User.SyllableExpansion)
		textBuilderSortCb.SetChecked(config.User.TextBuilderSort)
		flashcardCb.SetChecked(config.User.Flashcard)
		flashRandomCountCb.SetChecked(config.User.FlashRandomCount)

		// Set the count field to the config value, but default to "2" if the config is 0 (unsaved)
		if config.User.TextWordCount <= 0 {
			textWordCnt.SetText("1")
		} else {
			textWordCnt.SetText(fmt.Sprintf("%d", config.User.TextWordCount))
		}

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

		eIdx := 0
		for i, opt := range echoList {
			if opt == fmt.Sprintf("%d", config.User.EchoTolerance) {
				eIdx = i
				break
			}
		}
		echoDrop.SetCurrentOption(eIdx)

		euroSkipList = config.User.EuropeanSkipList
	}

	resetState()

	form.AddFormItem(useProsignsCb)
	form.AddFormItem(extendedPuncCb)
	form.AddFormItem(europeanCharsCb)
	form.AddFormItem(useSkipCb)
	form.AddFormItem(skipListInput)
	form.AddFormItem(delayDropDown)
	form.AddFormItem(repeatDropDown)
	form.AddFormItem(syllableExpansionCb)
	form.AddFormItem(wordOrderCb)
	form.AddFormItem(randomizeWordsCb)
	form.AddFormItem(wordBuilderCb)
	form.AddFormItem(wordSeparatorInput)
	form.AddFormItem(wordBuilderSortCb)
	form.AddFormItem(textBuilderCb)
	form.AddFormItem(textSeparatorInput)
	form.AddFormItem(textWordCnt)
	form.AddFormItem(textBuilderSortCb)
	form.AddFormItem(flashcardCb)
	form.AddFormItem(flashWordCountDrop)
	form.AddFormItem(flashRandomCountCb)
	form.AddFormItem(echoDrop)
	form.AddFormItem(startMsgCb)
	form.AddFormItem(startMsgInput)
	form.AddFormItem(endMsgCb)
	form.AddFormItem(endMsgInput)

	var optionsContainer *tview.Flex

	onSave := func() {
		if strings.TrimSpace(textWordCnt.GetText()) == "" {
			textWordCnt.SetText("2")
		}

		var errors []string
		isSyl := syllableExpansionCb.IsChecked()
		isRandomizeWords := randomizeWordsCb.IsChecked()
		isWB := wordBuilderCb.IsChecked()
		isTB := textBuilderCb.IsChecked()
		isFC := flashcardCb.IsChecked()
		isSkip := useSkipCb.IsChecked()

		count, err := strconv.Atoi(textWordCnt.GetText())
		if err != nil || count < 2 || count > 25 {
			errors = append(errors, "    Text Builder Word Count must be a number between 2 and 25.")
		}

		// Mutual Exclusivity Checks
		if isRandomizeWords && (isWB || isTB || isFC || isSyl) {
			errors = append(errors, "Randomize Word is not compatiable with Word Builder, Text Builder, Syllablize Words or Flashcard.")
		}

		if isTB && isWB {
			errors = append(errors, "Text Builder and Word Builder are mutually exclusive.")
		}
		if isFC && isWB {
			errors = append(errors, "Flashcard and Word Builder are mutually exclusive")
		}
		if isFC && isTB {
			errors = append(errors, "Flashcard and Text Builder are mutually exclusive")
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

		_, wc := flashWordCountDrop.GetCurrentOption()
		val, _ := strconv.Atoi(wc)
		if isFC && val < 1 {
			errors = append(errors, "Flashcard Word Count must be 1-20.")
		}
		config.User.FlashWordCount, _ = strconv.Atoi(wc)

		_, wc = echoDrop.GetCurrentOption()
		config.User.EchoTolerance, _ = strconv.Atoi(wc)
		if isFC && val < 1 {
			errors = append(errors, "Echo Tolerance must be: \"5,15,25,35,45\"" )
		}
		config.User.EchoTolerance, _ = strconv.Atoi(wc)

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
			config.User.TextBuilderSort = isTB

			config.User.WordSeparator = wordSeparatorInput.GetText()
			config.User.TextSeparator = textSeparatorInput.GetText()
			if count, err := strconv.Atoi(textWordCnt.GetText()); err == nil {
				config.User.TextWordCount = count
			}

			config.User.StartMsg = startMsgCb.IsChecked()
			config.User.StartMsgText = startMsgInput.GetText()
			config.User.EndMsg = endMsgCb.IsChecked()
			config.User.EndMsgText = endMsgInput.GetText()
			config.User.SyllableExpansion = isSyl

			dIdx, _ := delayDropDown.GetCurrentOption()
			config.User.StartDelay, _ = strconv.Atoi(delayOptions[dIdx])

			rIdx, _ := repeatDropDown.GetCurrentOption()
			config.User.RepeatLimit, _ = strconv.Atoi(repeatOptions[rIdx])
			fIdx, _ := flashWordCountDrop.GetCurrentOption()
			eIdx, _ := echoDrop.GetCurrentOption()

			config.User.Flashcard = flashcardCb.IsChecked()
			config.User.FlashWordCount, _ = strconv.Atoi(flashWordCountList[fIdx])
			config.User.FlashRandomCount = flashRandomCountCb.IsChecked()

			config.User.WordBuilderSort = wordBuilderCb.IsChecked()
			if isFC && config.User.EchoTolerance > 0 {
				config.User.Echo = true
				config.User.EchoTolerance = eIdx
			} else {
				config.User.Echo = false
				config.User.EchoTolerance = eIdx
			}
					
			config.SaveConfig()

			morse.RebuildMorseTable(config.User.UseExtendedPunctuation, config.User.UseEuropeanChars, config.User.UseSkip, config.User.SkipList, config.User.EuropeanSkipList)

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
	form.AddButton("European & Esparanto Char Skip", func() {
		showEuropeanCharSelector(optionsContainer, &euroSkipList, onSave)
	})
	form.AddButton("Cancel", func() {
		// close window like ESC
		pages.RemovePage("options")
	})

	optionsContainer = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(form, 0, 1, true)

	optionsContainer.SetBorder(true).SetTitle(" Options ")
	optionsContainer.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	pages.AddPage("options", createModal(optionsContainer, 70, 31), true, true)
	app.SetFocus(optionsContainer)
}
