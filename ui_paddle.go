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
	form.SetItemPadding(0)
	form.SetBackgroundColor(tcell.GetColor(AppBackgroundColor))
	form.SetFieldBackgroundColor(tcell.ColorBlue).SetFieldTextColor(tcell.ColorWhite)

	keyLineWireOptions := []string{
		"CTS:8-DTR:4-DSR:6",
		"DSR:6-DTR:4-CD:1",
		"CD:1-DTR:4-RI:9",
		"CTS:8-DTR:4-RI:9",
		"CTS:8-RTS:7-DSR:6",
		"DSR:6-RTS:7-CD:1",
		"CD:1-RTS:7-RI:9",
		"CTS:8-RTS:7-RI:9"}

	// --- Port Setup (Ensure "None" is ALWAYS index 0) ---
	ports, err := morse.GetSerialPorts()
	clean := []string{"None"}
	if err == nil {
		for _, p := range ports {
			if strings.TrimSpace(p) != "" && strings.TrimSpace(p) != "None" {
				clean = append(clean, p)
			}
		}
	}
	ports = clean

	keyDeviceOptions := []string{"Straight/bug", "Paddle", "Cootie", "IambicA", "IambicB", "Ultimatic"}

	keyDeviceDrop := tview.NewDropDown().
		SetLabel("Key Device Type").
		SetOptions(keyDeviceOptions, nil).
		SetCurrentOption(0)
	keyDeviceDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	keyDeviceDrop.SetFieldTextColor(tcell.ColorWhite)

	ditdahThresholdValues := []float64{0.25, 0.30, 0.35, 0.40, 0.45, 0.50, 0.55, 0.60, 0.65, 0.70, 0.75}
	ditdahThresholdOptions := []string{"25%", "30%", "35%", "40%", "45%", "50%", "55%", "60%", "65%", "70%", "75%"}

	ditdahThresholdDrop := tview.NewDropDown().
		SetLabel("Dit/Dah Memory Threshold").
		SetOptions(ditdahThresholdOptions, nil).
		SetCurrentOption(5)
	ditdahThresholdDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	ditdahThresholdDrop.SetFieldTextColor(tcell.ColorWhite)

	echoPortDrop := tview.NewDropDown().
		SetLabel("Key Device Port").
		SetOptions(ports, nil)
	echoPortDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	echoPortDrop.SetFieldTextColor(tcell.ColorWhite)

	// --- Transmitter Port Definition ---
	xmtrPortDrop := tview.NewDropDown().
		SetLabel("Transmitter Port").
		SetOptions(ports, nil)
	xmtrPortDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	xmtrPortDrop.SetFieldTextColor(tcell.ColorWhite)

	// --- MUTUAL EXCLUSIVITY LOGIC ---
	echoPortDrop.SetSelectedFunc(func(text string, index int) {
		if text == "None" {
			return
		} // Do nothing if they select None
		_, currXmtr := xmtrPortDrop.GetCurrentOption()
		if text == currXmtr {
			xmtrPortDrop.SetCurrentOption(0) // Force Xmtr Port back to "None"
		}
	})

	xmtrPortDrop.SetSelectedFunc(func(text string, index int) {
		if text == "None" {
			return
		}
		_, currEcho := echoPortDrop.GetCurrentOption()
		if text == currEcho {
			echoPortDrop.SetCurrentOption(0) // Force Echo Port back to "None"
		}
	})

	keyLineWireDrop := tview.NewDropDown().
		SetLabel("Key Adapter Interface").
		SetOptions(keyLineWireOptions, nil).
		SetCurrentOption(0)
	keyLineWireDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	keyLineWireDrop.SetFieldTextColor(tcell.ColorWhite)

	keyerReversedCb := tview.NewCheckbox().
		SetLabel("Dit/Dah Side Reverse")

	parasiticCb := tview.NewCheckbox().
		SetLabel("Parasitic Power")

	idlePolDrop := tview.NewDropDown().
		SetLabel("Keyed Line Polarity").
		SetOptions([]string{"Standard", "Inverted"}, nil).
		SetCurrentOption(0)
	idlePolDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	idlePolDrop.SetFieldTextColor(tcell.ColorWhite)

	// RESET (init + Reset button)
	resetState := func() {
		kdIndex := 0
		for i, opt := range keyDeviceOptions {
			if config.User.KeyDevice == opt {
				kdIndex = i
				break
			}
		}
		keyDeviceDrop.SetCurrentOption(kdIndex)

		ddIndex := 5
		for i, val := range ditdahThresholdValues {
			if config.User.DitDahThreshold == val {
				ddIndex = i
				break
			}
		}
		ditdahThresholdDrop.SetCurrentOption(ddIndex)

		lmIndex := 0
		for i, opt := range keyLineWireOptions {
			if config.User.KeyLineWireMode == opt {
				lmIndex = i
				break
			}
		}
		keyLineWireDrop.SetCurrentOption(lmIndex)

		keyerReversedCb.SetChecked(config.User.KeyerReversed)
		parasiticCb.SetChecked(config.User.KeyParasiticPower)

		if config.User.KeyLineIdlePolarity {
			idlePolDrop.SetCurrentOption(1)
		} else {
			idlePolDrop.SetCurrentOption(0)
		}

		// Restore Echo Port
		echoIdx := 0
		for i, p := range ports {
			if p == config.User.KeyerPort {
				echoIdx = i
				break
			}
		}
		echoPortDrop.SetCurrentOption(echoIdx)

		// Restore Xmtr Port
		xmtrIdx := 0
		for i, p := range ports {
			if p == config.User.XmtrPort {
				xmtrIdx = i
				break
			}
		}
		xmtrPortDrop.SetCurrentOption(xmtrIdx)
	}

	form.AddTextView("", "", 0, 1, false, false)
	form.AddFormItem(keyDeviceDrop)
	form.AddTextView("", "", 0, 1, false, false)
	form.AddFormItem(echoPortDrop)
	form.AddFormItem(keyLineWireDrop)
	form.AddFormItem(parasiticCb)
	form.AddFormItem(idlePolDrop)
	form.AddTextView("", "", 0, 1, false, false)
	form.AddFormItem(xmtrPortDrop)
	form.AddTextView("", "", 0, 1, false, false)
	form.AddFormItem(keyerReversedCb)
	form.AddFormItem(ditdahThresholdDrop)

	resetState()

	// SAVE
	onSave := func() {
		lmIdx, _ := keyLineWireDrop.GetCurrentOption()
		if lmIdx >= 0 && lmIdx < len(keyLineWireOptions) {
			config.User.KeyLineWireMode = keyLineWireOptions[lmIdx]
		} else {
			config.User.KeyLineWireMode = "CTS:8-DTR:4-DSR:6"
		}

		lmIdx, _ = keyDeviceDrop.GetCurrentOption()
		if lmIdx >= 0 && lmIdx < len(keyDeviceOptions) {
			config.User.KeyDevice = keyDeviceOptions[lmIdx]
		} else {
			config.User.KeyDevice = "Key/bug"
		}

		ddIdx, _ := ditdahThresholdDrop.GetCurrentOption()
		if ddIdx >= 0 && ddIdx < len(ditdahThresholdValues) {
			config.User.DitDahThreshold = ditdahThresholdValues[ddIdx]
		} else {
			config.User.DitDahThreshold = 0.50
		}

		config.User.KeyParasiticPower = parasiticCb.IsChecked()
		config.User.KeyerReversed = keyerReversedCb.IsChecked()

		ipIdx, _ := idlePolDrop.GetCurrentOption()
		config.User.KeyLineIdlePolarity = (ipIdx == 1)

		// Save both ports
		_, ePort := echoPortDrop.GetCurrentOption()
		config.User.KeyerPort = ePort

		_, xPort := xmtrPortDrop.GetCurrentOption()
		config.User.XmtrPort = xPort

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
		var clean []string = []string{"None"}
		if err == nil {
			for _, p := range newPorts {
				if strings.TrimSpace(p) != "" && strings.TrimSpace(p) != "None" {
					clean = append(clean, p)
				}
			}
		}

		echoPortDrop.SetOptions(clean, nil)
		xmtrPortDrop.SetOptions(clean, nil)

		// Try to preserve Echo
		eIdx := 0
		for i, p := range clean {
			if p == config.User.KeyerPort {
				eIdx = i
				break
			}
		}
		echoPortDrop.SetCurrentOption(eIdx)

		// Try to preserve Xmtr
		xIdx := 0
		for i, p := range clean {
			if p == config.User.XmtrPort {
				xIdx = i
				break
			}
		}
		// Failsafe: If somehow they both ended up on the same valid port, clear Xmtr
		if eIdx == xIdx && eIdx != 0 {
			xIdx = 0
		}
		xmtrPortDrop.SetCurrentOption(xIdx)
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
	footerView.SetText("\n[yellow]ESC to Close  -  Ctrl-S to Save[-]\n")

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
		if event.Key() == tcell.KeyCtrlS {
			onSave()
			return nil
		}
		return event
	})

	pages.AddPage("keyEcho", createModal(container, 65, 20), true, true)
	app.SetFocus(container)
}
