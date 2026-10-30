package morse

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"yama/config"

	"go.bug.st/serial"
)

var IsManualOscModeActive bool

func RunManualOscillator(updateUI func(statusMsg, mainMsg string), enableTx bool) {
	IsManualOscModeActive = true

	defer func() {
		IsManualOscModeActive = false
	}()

	if config.User.KeyerPort == "" || config.User.KeyerPort == "None" {
		updateUI(
			" [red]No COM port selected.",
			"\n\n *** MANUAL OSCILLATOR MODE ***\n\n ERROR: No COM port configured.\n\n Press ESC to exit this screen, THEN press Ctrl-K to select a port.",
		)
		return
	}

	oscFreq := float64(config.User.Tone)
	osc, player := StartOscillator(oscFreq)
	if osc == nil || player == nil {
		updateUI(
			" [red]Audio Failure",
			"\n\n *** MANUAL OSCILLATOR MODE ***\n\n CRITICAL FAIL - osc or player is nil!\n\n Press ESC to exit.",
		)
		return
	}

	player.SetVolume(0.0)

	// Clean teardown of both ports and audio
	var txPort serial.Port
	defer func() {
		if player != nil {
			player.SetVolume(0.0)
			time.Sleep(50 * time.Millisecond)
			player.Pause()
			player.Close()
		}
		if ActiveEchoPort != nil {
			ActiveEchoPort.Close()
		}
		if txPort != nil {
			txPort.SetDTR(false)
			txPort.SetRTS(false)
			txPort.Close()
		}
	}()

	mode := &serial.Mode{
		BaudRate: config.User.KeyerPortSpeed,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
		DataBits: 8,
	}

	var monitorPin1, monitorPin2, powerPin string
	// 1. ALWAYS parse the single 3-part configuration string
	pinParts := strings.Split(config.User.KeyLineWireMode, "-")
	if len(pinParts) > 0 {
		monitorPin1 = strings.Split(pinParts[0], ":")[0]
	} // The Dit / Straight Key line
	if len(pinParts) > 1 {
		powerPin = strings.Split(pinParts[1], ":")[0]
	} // The Common / Power line
	if len(pinParts) > 2 {
		monitorPin2 = strings.Split(pinParts[2], ":")[0]
	} // The Dah line (ignored if not needed)

	// 2. Just set a flag so the polling loop knows whether to bother reading monitorPin2
	is3Wire := false
	switch config.User.KeyDevice {
	case "Paddle", "IambicA", "IambicB", "Ultimatic", "Cootie":
		is3Wire = true
	}
	ditPin := monitorPin1
	dahPin := monitorPin2

	if config.User.KeyerReversed && config.User.KeyDevice != "Cootie" {
		ditPin = monitorPin2
		dahPin = monitorPin1
	}

	getMonitorState := func(ms *serial.ModemStatusBits, pin string) bool {
		switch pin {
		case "CD", "DCD":
			return ms.DCD
		case "DSR":
			return ms.DSR
		case "RI":
			return ms.RI
		case "CTS":
			fallthrough
		default:
			return ms.CTS
		}
	}

	idleInputState := config.User.KeyLineIdlePolarity
	isKeyDown := func(currentState bool) bool {
		return currentState != idleInputState
	}

	for !IsStopping {
		var openErr error

		// 1. Open Input Port
		for !IsStopping {
			if ActiveEchoPort != nil {
				ActiveEchoPort.Close()
				ActiveEchoPort = nil
			}

			ActiveEchoPort, openErr = serial.Open(config.User.KeyerPort, mode)

			if openErr != nil {
				updateUI(
					" [yellow]Waiting for key device...",
					fmt.Sprintf("\n\n *** MANUAL OSCILLATOR MODE ***\n\n Waiting for port: %s\n\n Insert your key device and send, or press ESC to exit.", config.User.KeyerPort),
				)
				time.Sleep(1 * time.Second)
				continue
			}

			useDTR := (powerPin == "DTR")
			useRTS := (powerPin == "RTS")

			if config.User.KeyParasiticPower {
				useDTR = true
				useRTS = true
			}

			_ = ActiveEchoPort.SetDTR(useDTR)
			_ = ActiveEchoPort.SetRTS(useRTS)

			time.Sleep(500 * time.Millisecond)
			break
		}

		// 2. Open Transmitter Port (if enabled by user via F4 prompt)
		if enableTx && config.User.XmtrPort != "" && config.User.XmtrPort != "None" {
			txPort, _ = serial.Open(config.User.XmtrPort, mode)
			if txPort != nil {
				txPort.SetDTR(false)
				txPort.SetRTS(false)
			}
		}

		if IsStopping {
			break
		}

		txMsg := ""
		if txPort != nil {
			txMsg = fmt.Sprintf("\n Mirroring output to %s to key transmitter.", config.User.XmtrPort)
		}

		updateUI(
			" [green]Manual Oscillator Active",
			fmt.Sprintf("\n\n *** MANUAL OSCILLATOR MODE ***\n\n Connected to input %s.%s\n\n Freely key now, or press ESC to exit.", config.User.KeyerPort, txMsg),
		)

		tp := GetTiming(false, config.User)
		ditDuration := time.Duration(tp.DitDuration * float64(time.Second))
		dahDuration := time.Duration(tp.DahDuration * float64(time.Second))
		interElementGap := time.Duration(tp.InterElement * float64(time.Second))

		ditdahThreshold := config.User.DitDahThreshold

		var mu sync.Mutex
		var memDit, memDah, lastWasDit, ultimaticLastDit bool
		var realtimeDit, realtimeDah bool
		var elementStartTime time.Time
		var currentElementDur time.Duration
		var isPlaying bool

		stopPolling := make(chan bool)

		// The Background Memory Goroutine (Captures paddle hits & Ultimatic edge transitions)
		go func() {
			var prevDit, prevDah bool // Tracks exact tap moments

			for {
				select {
				case <-stopPolling:
					return
				default:
					if ActiveEchoPort == nil {
						time.Sleep(10 * time.Millisecond)
						continue
					}
					ms, err := ActiveEchoPort.GetModemStatusBits()
					if err == nil {
						currDit := isKeyDown(getMonitorState(ms, ditPin))
						currDah := isKeyDown(getMonitorState(ms, dahPin))

						mu.Lock()
						realtimeDit = currDit
						realtimeDah = currDah

						// Edge detection for Ultimatic: ONLY updates the exact moment the paddle goes down
						if currDit && !prevDit {
							ultimaticLastDit = true
						}
						if currDah && !prevDah {
							ultimaticLastDit = false
						}

						prevDit = currDit
						prevDah = currDah

						if isPlaying {
							elapsed := time.Since(elementStartTime).Seconds()
							threshLimit := currentElementDur.Seconds() * ditdahThreshold
							if elapsed >= threshLimit {
								if currDit {
									memDit = true
								}
								if currDah {
									memDah = true
								}
							}
						} else {
							if currDit {
								memDit = true
							}
							if currDah {
								memDah = true
							}
						}
						mu.Unlock()
					}
					time.Sleep(2 * time.Millisecond)
				}
			}
		}()

		// Unified Element Player (Handles Audio + Tx Keying)
		playPerfectElement := func(duration time.Duration) {
			mu.Lock()
			isPlaying = true
			elementStartTime = time.Now()
			currentElementDur = duration
			mu.Unlock()

			// Key Tx Port & Audio On
			if txPort != nil {
				txPort.SetDTR(true)
				txPort.SetRTS(true)
			}
			player.SetVolume(1.0)

			time.Sleep(duration)

			// Unkey Tx Port & Audio Off
			mu.Lock()
			isPlaying = false
			mu.Unlock()

			if txPort != nil {
				txPort.SetDTR(false)
				txPort.SetRTS(false)
			}
			player.SetVolume(0.0)
			time.Sleep(interElementGap)
		}

		var wasDown bool

		for !IsStopping {
			mu.Lock()
			rdit, rdah := realtimeDit, realtimeDah
			mdit, mdah := memDit, memDah
			uLastDit := ultimaticLastDit
			memDit, memDah = false, false
			mu.Unlock()

			// Disconnect safety check
			_, err := ActiveEchoPort.GetModemStatusBits()
			if err != nil {
				if txPort != nil {
					txPort.SetDTR(false)
					txPort.SetRTS(false)
				}
				player.SetVolume(0.0)
				wasDown = false
				updateUI(
					" [red]Device disconnected...",
					"\n\n *** MANUAL OSCILLATOR MODE ***\n\n Connection lost.\n\n Reinsert your key device, or press ESC to exit.",
				)
				stopPolling <- true
				time.Sleep(500 * time.Millisecond)
				break
			}

			if config.User.KeyDevice == "Cootie" {
				keyDown := rdit || rdah
				if keyDown && !wasDown {
					if txPort != nil {
						txPort.SetDTR(true)
						txPort.SetRTS(true)
					}
					player.SetVolume(1.0)
					wasDown = true
				} else if !keyDown && wasDown {
					if txPort != nil {
						txPort.SetDTR(false)
						txPort.SetRTS(false)
					}
					player.SetVolume(0.0)
					wasDown = false
				}
			} else {
				// Use the unified Paddle Evaluator from keyer.go
				playElement, nextLastWasDit := EvaluatePaddleAction(config.User.KeyDevice, rdit, rdah, mdit, mdah, lastWasDit, uLastDit)

				if playElement == 1 {
					playPerfectElement(ditDuration)
					lastWasDit = nextLastWasDit
				} else if playElement == 2 {
					playPerfectElement(dahDuration)
					lastWasDit = nextLastWasDit
				} else {
					// Handle straight key / bug audio direct lines
					if !is3Wire {
						if rdit && !wasDown {
							if txPort != nil {
								txPort.SetDTR(true)
								txPort.SetRTS(true)
							}
							player.SetVolume(1.0)
							wasDown = true
						} else if !rdit && wasDown {
							if txPort != nil {
								txPort.SetDTR(false)
								txPort.SetRTS(false)
							}
							player.SetVolume(0.0)
							wasDown = false
						}
					}
				}
			}

			time.Sleep(1 * time.Millisecond)
		}
	}

	updateUI("STOP", "")
}
