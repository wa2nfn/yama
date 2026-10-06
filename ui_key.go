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

func showKeyEchoOptions() {
	form := tview.NewForm()
	form.SetItemPadding(0) // Removes the blank lines between form fields
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	// --- Echo Port ---
	ports, err := morse.GetSerialPorts()
	if err != nil || len(ports) == 0 {
		ports = []string{"None"}
	}
	clean := []string{}
	for _, p := range ports {
		if strings.TrimSpace(p) != "" {
			clean = append(clean, p)
		}
	}
	ports = clean

	echoPortDrop := tview.NewDropDown().
		SetLabel("Echo Port").
		SetOptions(ports, nil)
	echoPortDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	echoPortDrop.SetFieldTextColor(tcell.ColorWhite)

	// --- Tolerance ---
	tolDrop := tview.NewDropDown().SetLabel("Tolerance (%)")
	tolOptions := []string{}
	for i := 5; i <= 45; i++ {
		tolOptions = append(tolOptions, fmt.Sprintf("%d", i))
	}
	tolDrop.SetOptions(tolOptions, nil)

	// --- Echo Word Count ---
	wcDrop := tview.NewDropDown().SetLabel("Echo Word Count")
	wcOptions := []string{}
	for i := 1; i <= 25; i++ {
		wcOptions = append(wcOptions, fmt.Sprintf("%d", i))
	}
	wcDrop.SetOptions(wcOptions, nil)

	// --- Random Word Count ---
	echoRandomCountCb := tview.NewCheckbox().
		SetLabel("   Random Word Count")

	// --- Alert Tone Dropdown ---
	alertDrop := tview.NewDropDown().
		SetLabel("Key Now Alert Tone").
		SetOptions([]string{"First Group", "None", "All Groups"}, nil)

	// --- Alert Tone Freq ---
	acceptDigits := func(textToCheck string, lastChar rune) bool {
		if textToCheck == "" {
			return true
		}
		_, err := strconv.Atoi(textToCheck)
		return err == nil
	}
	alertFreqInput := tview.NewInputField().
		SetLabel("   Alert Tone Freq (Hz)").
		SetFieldWidth(6).
		SetAcceptanceFunc(acceptDigits)

	// --- ErrorTone ---
	errorCb := tview.NewCheckbox().
		SetLabel("   Error Tone")

	// --- Key Time Padding ---
	paddingDrop := tview.NewDropDown().
		SetLabel("Key Time Padding").
		SetOptions([]string{"Strict", "Normal", "Relaxed", "Generous"}, nil).
		SetCurrentOption(1)

	// --- SideTone ---
	sideCb := tview.NewCheckbox().
		SetLabel("SideTone")

	// --- Visual Feedback ---
	visualCb := tview.NewCheckbox().
		SetLabel("Visual Feedback")

	// --- PerfectPause ---
	perfectPauseCb := tview.NewCheckbox().
		SetLabel("Perfect Match Message")

	// --- Key Line Mode ---
	keyLineOptions := []string{"CTS:8-DTR:4", "DSR:6-DTR:4", "CD:1-DTR:4", "RI:9-DTR:4", "CTS:8-RTS:7", "DSR:6-RTS:7", "CD:1-RTS:7", "RI:9-RTS:7"}
	lineModeDrop := tview.NewDropDown().
		SetLabel("   Key Line Interface").
		SetOptions(keyLineOptions, nil).
		SetCurrentOption(0)
	lineModeDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	lineModeDrop.SetFieldTextColor(tcell.ColorWhite)

	// --- Parasitic Power ---
	parasiticCb := tview.NewCheckbox().
		SetLabel("   Parasitic Power")

	// --- Idle Polarity ---
	idlePolDrop := tview.NewDropDown().
		SetLabel("   Key Line Polarity").
		SetOptions([]string{"Standard", "Inverted"}, nil).
		SetCurrentOption(0)
	idlePolDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	idlePolDrop.SetFieldTextColor(tcell.ColorWhite)

	// RESET (init + Reset button)
	resetState := func() {
		// Tolerance
		tol := config.User.EchoTolerance
		if tol < 5 || tol > 45 {
			tol = 5
			config.User.EchoTolerance = tol
		}
		tolDrop.SetCurrentOption(tol - 5)

		// Word Count
		wc := config.User.EchoWordCount
		if wc < 1 || wc > 25 {
			wc = 1
			config.User.EchoWordCount = wc
		}
		wcDrop.SetCurrentOption(wc - 1)

		// Random Count
		echoRandomCountCb.SetChecked(config.User.EchoRandomCount)

		// Alert
		alert := config.User.Alert
		if alert < 0 || alert > 2 {
			alert = 0
			config.User.Alert = alert
		}
		alertDrop.SetCurrentOption(alert)

		// Alert Tone Freq
		at := config.User.AlertTone
		if at < 400 || at > 1200 {
			at = 500
			config.User.AlertTone = at
		}
		alertFreqInput.SetText(fmt.Sprintf("%d", at))

		// ErrorTone
		errorCb.SetChecked(config.User.ErrorTone)

		// Key Time Padding
		padding := config.User.KeyTimePadding
		if padding < 0 || padding > 3 {
			padding = 1
		}
		paddingDrop.SetCurrentOption(padding)

		// SideTone
		sideCb.SetChecked(config.User.SideTone)

		// Visual Feedback
		visualCb.SetChecked(config.User.VisualFeedback)
		// PerfectPause Feedback
		perfectPauseCb.SetChecked(config.User.PerfectPause)

		// Key Line Mode
		lmIndex := 0
		for i, opt := range keyLineOptions {
			if config.User.KeyLineMode == opt {
				lmIndex = i
				break
			}
		}
		lineModeDrop.SetCurrentOption(lmIndex)

		// Parasitic Power
		parasiticCb.SetChecked(config.User.KeyParasiticPower)

		// Polarity: false maps to Standard (0), true maps to Inverted (1)
		if config.User.KeyLineIdlePolarity {
			idlePolDrop.SetCurrentOption(1) // Inverted
		} else {
			idlePolDrop.SetCurrentOption(0) // Standard
		}

		// Port
		if len(ports) > 0 {
			idx := 0
			for i, p := range ports {
				if p == config.User.KeyerPort {
					idx = i
					break
				}
			}
			echoPortDrop.SetCurrentOption(idx)
		}
	}

	// Add items in order
	form.AddFormItem(tolDrop)
	form.AddTextView("", "", 0, 1, false, false)
	form.AddFormItem(wcDrop)
	form.AddFormItem(echoRandomCountCb)
	form.AddTextView("", "", 0, 1, false, false)
	form.AddFormItem(alertDrop)
	form.AddFormItem(alertFreqInput)
	form.AddFormItem(errorCb)
	form.AddTextView("", "", 0, 1, false, false)
	form.AddFormItem(paddingDrop)
	form.AddFormItem(sideCb)
	form.AddFormItem(visualCb)
	form.AddFormItem(perfectPauseCb)
	form.AddTextView("", "", 0, 1, false, false)
	form.AddFormItem(echoPortDrop)
	form.AddFormItem(lineModeDrop)
	form.AddFormItem(parasiticCb)
	form.AddFormItem(idlePolDrop)

	// Init UI
	resetState()

	// SAVE
	onSave := func() {
		var errors []string

		alertTone, _ := strconv.Atoi(alertFreqInput.GetText())
		if alertTone < 400 || alertTone > 1200 {
			errors = append(errors, "Alert Tone Frequency must be between 400 and 1200 Hz")
		}

		tolIndex, _ := tolDrop.GetCurrentOption()
		tol := tolIndex + 5

		wcIndex, _ := wcDrop.GetCurrentOption()
		wc := wcIndex + 1

		if len(errors) > 0 {
			showErrorModal(errors, func() { app.SetFocus(form) })
			return
		}

		alertIdx, _ := alertDrop.GetCurrentOption()
		config.User.Alert = alertIdx

		paddingIdx, _ := paddingDrop.GetCurrentOption()
		config.User.KeyTimePadding = paddingIdx

		config.User.SideTone = sideCb.IsChecked()
		config.User.ErrorTone = errorCb.IsChecked()
		config.User.VisualFeedback = visualCb.IsChecked()
		config.User.PerfectPause = perfectPauseCb.IsChecked()

		lmIdx, _ := lineModeDrop.GetCurrentOption()
		if lmIdx >= 0 && lmIdx < len(keyLineOptions) {
			config.User.KeyLineMode = keyLineOptions[lmIdx]
		} else {
			config.User.KeyLineMode = "CTS:8-DTR:4"
		}

		config.User.KeyParasiticPower = parasiticCb.IsChecked()

		// Save polarity as boolean (Standard/index 0 = false, Inverted/index 1 = true)
		ipIdx, _ := idlePolDrop.GetCurrentOption()
		config.User.KeyLineIdlePolarity = (ipIdx == 1)

		config.User.AlertTone = alertTone
		config.User.EchoRandomCount = echoRandomCountCb.IsChecked()
		config.User.EchoTolerance = tol
		config.User.EchoWordCount = wc

		// Save selected port string
		_, port := echoPortDrop.GetCurrentOption()
		config.User.KeyerPort = port

		config.SaveConfig()

		if !config.User.Echo {
			closeEchoStatsWindow()
		}

		updateBlueLine()
		pages.RemovePage("keyEcho")
		app.SetFocus(inputArea)
	}

	onRefreshPorts := func() {
		newPorts, err := morse.GetSerialPorts()
		if err != nil || len(newPorts) == 0 {
			newPorts = []string{"None"}
		}
		var clean []string
		for _, p := range newPorts {
			if strings.TrimSpace(p) != "" {
				clean = append(clean, p)
			}
		}

		echoPortDrop.SetOptions(clean, nil)

		// Try to preserve the currently selected port if it still exists
		currentPort := config.User.KeyerPort
		idx := 0
		for i, p := range clean {
			if p == currentPort {
				idx = i
				break
			}
		}
		echoPortDrop.SetCurrentOption(idx)
	}

	form.AddButton("Save", onSave)
	form.AddButton("Reset", resetState)
	form.AddButton("Refresh Ports", onRefreshPorts)
	form.AddButton("Cancel", func() {
		pages.RemovePage("keyEcho")
		app.SetFocus(inputArea)
	})

	applyFocusStyles(form)
	form.SetBorder(false)

	footerView := tview.NewTextView().SetTextAlign(tview.AlignCenter).SetDynamicColors(true)
	footerView.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	// 1. Update the footer to advertise the new hotkey
	footerView.SetText("\n[yellow]ESC to Close  -  Ctrl-S to Save[-]\n")

	// Explicitly defining the container here to ensure the compiler sees it
	var container *tview.Flex = tview.NewFlex()
	container.SetDirection(tview.FlexRow)
	container.AddItem(form, 0, 1, true)
	container.AddItem(footerView, 1, 0, false)

	container.SetBorder(true).SetTitle(" Echo ")
	container.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))

	container.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEscape {
			pages.RemovePage("keyEcho")
			app.SetFocus(inputArea)
			return nil
		}
		// 2. Catch Ctrl-S and trigger the exact same save logic as the button
		if event.Key() == tcell.KeyCtrlS {
			onSave()
			return nil
		}
		return event
	})

	// Widened from 42 to 55 to comfortably fit all 4 buttons in a single row
	pages.AddPage("keyEcho", createModal(container, 65, 26), true, true)
	app.SetFocus(container)
}

