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

	// --- Port Setup ---
	ports, _ := morse.GetSerialPorts()
	clean := []string{"None"}
	for _, p := range ports {
		if strings.TrimSpace(p) != "" && strings.TrimSpace(p) != "None" {
			clean = append(clean, p)
		}
	}
	ports = clean

	keyDeviceLabels := []string{"Straight/Bug/Cootie (2 wire)", "Paddle", "Cootie (3 wire)", "IambicA", "IambicB", "Ultimatic"}
	keyDeviceValues := []string{"Straight/Bug", "Paddle", "Cootie", "IambicA", "IambicB", "Ultimatic"}

	keyDeviceDrop := tview.NewDropDown().
		SetLabel("Key Device Type").
		SetOptions(keyDeviceLabels, nil).
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

	xmtrPortDrop := tview.NewDropDown().
		SetLabel("Transmitter Port").
		SetOptions(ports, nil)
	xmtrPortDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	xmtrPortDrop.SetFieldTextColor(tcell.ColorWhite)

	xmtrInterfaceDrop := tview.NewDropDown().
		SetLabel("Transmitter Interface").
		SetOptions([]string{"DTR:4", "RTS:7", "Both"}, nil).
		SetCurrentOption(0)
	xmtrInterfaceDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	xmtrInterfaceDrop.SetFieldTextColor(tcell.ColorWhite)

	keyLineWireDrop := tview.NewDropDown().
		SetLabel("Key Adapter Interface").
		SetOptions(keyLineWireOptions, nil).
		SetCurrentOption(0)
	keyLineWireDrop.SetFieldBackgroundColor(tcell.ColorBlue)
	keyLineWireDrop.SetFieldTextColor(tcell.ColorWhite)

	// --- SMART TRANSMITTER INTERFACE FILTERING ---
	isInitializing := true

	updateXmtrInterface := func(targetSelection string) {
		_, echoPort := echoPortDrop.GetCurrentOption()
		_, xmtrPort := xmtrPortDrop.GetCurrentOption()
		_, adapterWire := keyLineWireDrop.GetCurrentOption()

		if targetSelection == "" {
			_, targetSelection = xmtrInterfaceDrop.GetCurrentOption()
		}

		availableOptions := []string{"DTR:4", "RTS:7", "Both"}

		if echoPort == xmtrPort && echoPort != "None" && echoPort != "" {
			if strings.Contains(adapterWire, "DTR") {
				availableOptions = []string{"RTS:7"}
			} else if strings.Contains(adapterWire, "RTS") {
				availableOptions = []string{"DTR:4"}
			}
		}

		xmtrInterfaceDrop.SetOptions(availableOptions, nil)

		var newIdx int
		for i, opt := range availableOptions {
			if opt == targetSelection {
				newIdx = i
				break
			}
		}
		xmtrInterfaceDrop.SetCurrentOption(newIdx)
	}

	echoPortDrop.SetSelectedFunc(func(text string, index int) {
		if !isInitializing {
			updateXmtrInterface("")
		}
	})

	xmtrPortDrop.SetSelectedFunc(func(text string, index int) {
		if !isInitializing {
			updateXmtrInterface("")
		}
	})

	keyLineWireDrop.SetSelectedFunc(func(text string, index int) {
		if !isInitializing {
			updateXmtrInterface("")
		}
	})

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

	// RESET
	resetState := func() {
		isInitializing = true

		var kdIndex int
		for i, opt := range keyDeviceValues {
			if config.User.KeyDevice == opt {
				kdIndex = i
				break
			}
		}
		keyDeviceDrop.SetCurrentOption(kdIndex)

		var ddIndex int = 5
		for i, val := range ditdahThresholdValues {
			if config.User.DitDahThreshold == val {
				ddIndex = i
				break
			}
		}
		ditdahThresholdDrop.SetCurrentOption(ddIndex)

		var lmIndex int
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

		var echoIdx int
		for i, p := range ports {
			if p == config.User.KeyerPort {
				echoIdx = i
				break
			}
		}
		echoPortDrop.SetCurrentOption(echoIdx)

		var xmtrIdx int
		for i, p := range ports {
			if p == config.User.TransmitterPort {
				xmtrIdx = i
				break
			}
		}
		xmtrPortDrop.SetCurrentOption(xmtrIdx)

		updateXmtrInterface(config.User.TransmitterInterface)
		isInitializing = false
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
	form.AddFormItem(xmtrInterfaceDrop)
	form.AddTextView("", "", 0, 1, false, false)
	form.AddFormItem(keyerReversedCb)
	form.AddFormItem(ditdahThresholdDrop)

	resetState()

	// SAVE
	onSave := func() {
		var errors []string

		_, ePort := echoPortDrop.GetCurrentOption()
		_, xPort := xmtrPortDrop.GetCurrentOption()

		_, adapterWire := keyLineWireDrop.GetCurrentOption()
		if adapterWire == "" {
			adapterWire = "CTS:8-DTR:4-DSR:6"
		}

		_, txIfc := xmtrInterfaceDrop.GetCurrentOption()
		if txIfc == "" {
			txIfc = "DTR"
		}

		if ePort == xPort && ePort != "None" && ePort != "" {
			if txIfc == "Both" {
				errors = append(errors, "COM Port Conflict: You cannot use 'Both' for Transmitter Interface when sharing a single COM port.")
			} else if strings.Contains(adapterWire, "DTR") && txIfc == "DTR" {
				errors = append(errors, "COM Port Conflict: DTR is used by your Key Adapter. Please select RTS for the Transmitter Interface.")
			} else if strings.Contains(adapterWire, "RTS") && txIfc == "RTS" {
				errors = append(errors, "COM Port Conflict: RTS is used by your Key Adapter. Please select DTR for the Transmitter Interface.")
			}
		}

		apply := func() {
			config.User.KeyLineWireMode = adapterWire

			kdIdx, _ := keyDeviceDrop.GetCurrentOption()
			if kdIdx >= 0 && kdIdx < len(keyDeviceValues) {
				config.User.KeyDevice = keyDeviceValues[kdIdx]
			} else {
				config.User.KeyDevice = "Straight/Bug"
			}

			var ddIdx int
			ddIdx, _ = ditdahThresholdDrop.GetCurrentOption()
			if ddIdx >= 0 && ddIdx < len(ditdahThresholdValues) {
				config.User.DitDahThreshold = ditdahThresholdValues[ddIdx]
			} else {
				config.User.DitDahThreshold = 0.50
			}

			config.User.KeyParasiticPower = parasiticCb.IsChecked()
			config.User.KeyerReversed = keyerReversedCb.IsChecked()

			var ipIdx int
			ipIdx, _ = idlePolDrop.GetCurrentOption()
			config.User.KeyLineIdlePolarity = (ipIdx == 1)

			config.User.KeyerPort = ePort
			config.User.TransmitterPort = xPort
			config.User.TransmitterInterface = txIfc

			config.SaveConfig()

			if !config.User.Echo {
				closeEchoStatsWindow()
			}

			updateBlueLine()
			pages.RemovePage("keyEcho")
			app.SetFocus(inputArea)
		}

		if len(errors) > 0 {
			showErrorModal(errors, func() { app.SetFocus(form) })
		} else {
			apply()
		}
	}

	onRefreshPorts := func() {
		_, currentMonitorText := echoPortDrop.GetCurrentOption()
		_, currentXmtrText := xmtrPortDrop.GetCurrentOption()

		freshPorts, _ := morse.GetSerialPorts()
		freshClean := []string{"None"}
		for _, p := range freshPorts {
			if strings.TrimSpace(p) != "" && strings.TrimSpace(p) != "None" {
				freshClean = append(freshClean, p)
			}
		}

		echoPortDrop.SetOptions(freshClean, nil)
		xmtrPortDrop.SetOptions(freshClean, nil)

		restoreSelection := func(dd *tview.DropDown, targetText string) {
			var restoredIdx int
			for i, opt := range freshClean {
				if opt == targetText {
					restoredIdx = i
					break
				}
			}
			dd.SetCurrentOption(restoredIdx)
		}

		restoreSelection(echoPortDrop, currentMonitorText)
		restoreSelection(xmtrPortDrop, currentXmtrText)

		_, currentXmtrIfc := xmtrInterfaceDrop.GetCurrentOption()
		updateXmtrInterface(currentXmtrIfc)
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
