package main

import (
	"strings"
	"yama/config"
	"yama/morse"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func showKeyDeviceOptions() {
	form := tview.NewForm()
	form.SetItemPadding(0) // Removes the blank lines between form fields
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	// paddles
	keyLine3WireOptions := []string{
		"CTS:8-DTR:4-DSR:6",
		"DSR:6-DTR:4-CD:1",
		"CD:1-DTR:4-RI:9",
		"CTS:8-DTR:4-RI:9",
		"CTS:8-RTS:7-DSR:6",
		"DSR:6-RTS:7-CD:1",
		"CD:1-RTS:7-RI:9",
		"CTS:8-RTS:7-RI:9"}

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

	// physical HW
	keyDeviceOptions := []string{"Key/bug", "Paddle", "Cootie", "IambicA"}

	keyDeviceDrop := tview.NewDropDown().
		SetLabel("Key Device ").
		SetOptions(keyDeviceOptions, nil).
		SetCurrentOption(0)
	keyDeviceDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	keyDeviceDrop.SetFieldTextColor(tcell.ColorWhite)

	echoPortDrop := tview.NewDropDown().
		SetLabel("Key Device Port").
		SetOptions(ports, nil)
	echoPortDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	echoPortDrop.SetFieldTextColor(tcell.ColorWhite)

	// --- 2 Wire Key Line Mode ---
	keyLine2WireOptions := []string{
		"CTS:8-DTR:4",
		"DSR:6-DTR:4",
		"CD:1-DTR:4",
		"RI:9-DTR:4",
		"CTS:8-RTS:7",
		"DSR:6-RTS:7",
		"CD:1-RTS:7",
		"RI:9-RTS:7"}

	keyLine2WireDrop := tview.NewDropDown().
		SetLabel("   2 Wire Interface").
		SetOptions(keyLine2WireOptions, nil).
		SetCurrentOption(0)
	keyLine2WireDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	keyLine2WireDrop.SetFieldTextColor(tcell.ColorWhite)

	keyLine3WireDrop := tview.NewDropDown().
		SetLabel("   3 Wire Interface").
		SetOptions(keyLine3WireOptions, nil).
		SetCurrentOption(0)
	keyLine3WireDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	keyLine3WireDrop.SetFieldTextColor(tcell.ColorWhite)

	// --- Side operation
	keyerReversedCb := tview.NewCheckbox().
		SetLabel("   Dit/Dah Side Reverse")

	// --- Parasitic Power ---
	parasiticCb := tview.NewCheckbox().
		SetLabel("Parasitic Power")

	// --- Idle Polarity ---
	idlePolDrop := tview.NewDropDown().
		SetLabel("Keyed Line Polarity").
		SetOptions([]string{"Standard", "Inverted"}, nil).
		SetCurrentOption(0)
	idlePolDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	idlePolDrop.SetFieldTextColor(tcell.ColorWhite)

	// RESET (init + Reset button)
	resetState := func() {
		// Key Device
		kdIndex := 0
		for i, opt := range keyDeviceOptions {
			if config.User.KeyDevice == opt {
				kdIndex = i
				break
			}
		}

		keyDeviceDrop.SetCurrentOption(kdIndex)
		// Key Line Mode
		lmIndex := 0
		for i, opt := range keyLine2WireOptions {
			if config.User.KeyLine2WireMode == opt {
				lmIndex = i
				break
			}
		}
		keyLine2WireDrop.SetCurrentOption(lmIndex)

		for i, opt := range keyLine3WireOptions {
			if config.User.KeyLine3WireMode == opt {
				lmIndex = i
				break
			}
		}
		keyLine3WireDrop.SetCurrentOption(lmIndex)

		// Side Revers
		keyerReversedCb.SetChecked(config.User.KeyerReversed)

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
	form.AddTextView("", "", 0, 1, false, false)
	form.AddFormItem(keyDeviceDrop)
	form.AddFormItem(echoPortDrop)
	form.AddTextView("", "", 0, 1, false, false)
	form.AddTextView("", "", 0, 1, false, false)
	form.AddTextView("For: key, bug, cootie, external keyer,", "", 0, 1, false, false)
	form.AddFormItem(keyLine2WireDrop)
	form.AddTextView("", "", 0, 1, false, false)
	form.AddTextView("For: paddle, IambicA", "", 0, 1, false, false)
	form.AddFormItem(keyLine3WireDrop)
	form.AddFormItem(keyerReversedCb)

	form.AddTextView("", "", 0, 1, false, false)
	form.AddFormItem(parasiticCb)
	form.AddFormItem(idlePolDrop)

	// Init UI
	resetState()

	// SAVE
	onSave := func() {
		//var errors []string

		lmIdx, _ := keyLine2WireDrop.GetCurrentOption()
		if lmIdx >= 0 && lmIdx < len(keyLine2WireOptions) {
			config.User.KeyLine2WireMode = keyLine2WireOptions[lmIdx]
		} else {
			config.User.KeyLine2WireMode = "CTS:8-DTR:4"
		}

		lmIdx, _ = keyLine3WireDrop.GetCurrentOption()
		if lmIdx >= 0 && lmIdx < len(keyLine3WireOptions) {
			config.User.KeyLine3WireMode = keyLine3WireOptions[lmIdx]
		} else {
			config.User.KeyLine3WireMode = "CTS:8-DTR:4-DSR:6"
		}

		lmIdx, _ = keyDeviceDrop.GetCurrentOption()
		if lmIdx >= 0 && lmIdx < len(keyDeviceOptions) {
			config.User.KeyDevice = keyDeviceOptions[lmIdx]
		} else {
			config.User.KeyDevice = "Key/bug"
		}

		config.User.KeyParasiticPower = parasiticCb.IsChecked()
		config.User.KeyerReversed = keyerReversedCb.IsChecked()

		// Save polarity as boolean (Standard/index 0 = false, Inverted/index 1 = true)
		ipIdx, _ := idlePolDrop.GetCurrentOption()
		config.User.KeyLineIdlePolarity = (ipIdx == 1)

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

	container.SetBorder(true).SetTitle(" KeyDevice ")
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

	pages.AddPage("keyEcho", createModal(container, 65, 21), true, true)
	app.SetFocus(container)
}
