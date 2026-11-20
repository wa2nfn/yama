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
var txActive bool
var txLightMu sync.Mutex
var txLastHit time.Time

func RunManualOscillator(updateUI func(statusMsg, mainMsg string), enableTx bool) {
	IsManualOscModeActive = true

	defer func() {
		IsManualOscModeActive = false
	}()

	if config.User.KeyerPort == "" || config.User.KeyerPort == "None" {
		updateUI(
			" [red]No COM port selected.",
			"\n\n *** PRACTICE OSCILLATOR MODE ***\n\n ERROR: No COM port configured.\n\n Press ESC to exit this screen, THEN press Ctrl-K for KeyDevice to select a port.",
		)
		return
	}

	oscFreq := float64(config.User.Tone)
	osc, player := StartOscillator(oscFreq)
	if osc == nil || player == nil {
		updateUI(
			" [red]Audio Failure",
			"\n\n *** PRACTICE OSCILLATOR MODE ***\n\n CRITICAL FAIL - osc or player is nil!\n\n Press ESC to exit.",
		)
		return
	}

	player.SetVolume(0.0)

	// --- STRICT LOCAL STATE FOR RAPID F4 RESTARTS ---
	var mu sync.Mutex
	var localPort serial.Port
	var txPort serial.Port

	// Clean teardown of both ports and audio
	defer func() {
		if player != nil {
			player.SetVolume(0.0)
			time.Sleep(50 * time.Millisecond)
			player.Pause()
		}

		mu.Lock()
		if localPort != nil {
			localPort.Close()
		}
		mu.Unlock()

		if txPort != nil {
			txPort.SetDTR(false)
			txPort.SetRTS(false)
			txLightMu.Lock()
			txActive = false
			txLightMu.Unlock()
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

	// ALWAYS parse the single 3-part configuration string
	pinParts := strings.Split(config.User.KeyLineWireMode, "-")
	if len(pinParts) > 0 {
		monitorPin1 = strings.Split(pinParts[0], ":")[0]
	}
	if len(pinParts) > 1 {
		powerPin = strings.Split(pinParts[1], ":")[0]
	}
	if len(pinParts) > 2 {
		monitorPin2 = strings.Split(pinParts[2], ":")[0]
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

	// --- GLOBAL STATE FOR THE GOROUTINE ---
	var memDit, memDah, lastWasDit, ultimaticLastDit bool
	var realtimeDit, realtimeDah bool
	var elementStartTime time.Time
	var currentElementDur time.Duration
	var isPlaying bool

	// BULLETPROOF GOROUTINE CLEANUP
	stopPolling := make(chan struct{})
	defer close(stopPolling)

	// The Background Memory Goroutine (Spawned exactly ONCE)
	go func() {
		var prevDit, prevDah bool
		for {
			select {
			case <-stopPolling:
				return
			default:
				// STRICT LOCK: Prevents Torn Interface Panic during F4 toggles
				mu.Lock()
				port := localPort
				mu.Unlock()

				if port == nil {
					time.Sleep(10 * time.Millisecond)
					continue
				}

				ms, err := port.GetModemStatusBits()
				if err == nil {
					currDit := isKeyDown(getMonitorState(ms, ditPin))
					currDah := isKeyDown(getMonitorState(ms, dahPin))

					mu.Lock()
					realtimeDit = currDit
					realtimeDah = currDah

					// Edge detection for Ultimatic
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
						// Avoid division by zero panic on init
						if currentElementDur.Seconds() > 0 {
							threshLimit := currentElementDur.Seconds() * config.User.DitDahThreshold
							if elapsed >= threshLimit {
								if currDit {
									memDit = true
								}
								if currDah {
									memDah = true
								}
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

	for !IsStopping {
		var openErr error

		// 1. Open Input Port
		for !IsStopping {
			mu.Lock()
			if localPort != nil {
				localPort.Close()
				localPort = nil
			}
			mu.Unlock()

			newPort, err := serial.Open(config.User.KeyerPort, mode)
			openErr = err

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
			_ = newPort.SetDTR(useDTR)
			_ = newPort.SetRTS(useRTS)

			// Safely assign back to local port
			mu.Lock()
			localPort = newPort
			ActiveEchoPort = newPort // Kept to satisfy global dependencies, but safely overwritten
			mu.Unlock()

			time.Sleep(500 * time.Millisecond)
			break
		}

		// 2. Open Transmitter Port (if enabled)
		if enableTx && config.User.TransmitterPort != "" && config.User.TransmitterPort != "None" {
			// THE FIX: If they are using the exact same COM port for both input and output,
			// just share the already-open localPort handle!
			if strings.EqualFold(config.User.TransmitterPort, config.User.KeyerPort) {
				txPort = localPort
			} else {
				txPort, _ = serial.Open(config.User.TransmitterPort, mode)
			}

			if txPort != nil {
				txPort.SetDTR(false)
				txPort.SetRTS(false)
			}
		}

		if IsStopping {
			break
		}

		// Build the status messages based strictly on the user's intent
		if enableTx {
			if txPort != nil {
				// Do not overwrite the scary warning, so pass empty strings
				updateUI("", "")
			} else {
				updateUI(" [red]WARNING: TX Port failed to open! TX Disabled.[-]", "")
			}
		} else {
			updateUI(
				"",
				fmt.Sprintf("\n\n *** PRACTICE OSCILLATOR MODE ***\n\n Connected to input %s.\n\n Freely key now, or press ESC to exit.", config.User.KeyerPort),
			)
		}

		// --- TX HANG-LIGHT GOROUTINE ---
		// (The shadowed 'var txActive bool' that was here has been deleted!)

		if enableTx && txPort != nil {
			// Force the initial "STANDBY" state to paint immediately
			updateUI(" [white]STANDBY[-] LIVE ON AIR", "")

			go func() {
				isLit := false
				for !IsStopping {
					txLightMu.Lock()
					active := txActive // Now safely reads the global variable!
					last := txLastHit
					txLightMu.Unlock()

					// 150ms hang time keeps the light solid between fast dits/dahs
					shouldBeLit := active || time.Since(last) < 180*time.Millisecond

					if shouldBeLit && !isLit {
						isLit = true
						// Pass an empty string so it doesn't accidentally overwrite the main screen
						updateUI(" [red:white:b] \u2588 TX \u2588 [-:-:-] LIVE ON AIR", "")
					} else if !shouldBeLit && isLit {
						isLit = false
						updateUI(" [white]STANDBY[-] LIVE ON AIR", "")
					}
					time.Sleep(20 * time.Millisecond) // 50 FPS refresh
				}
			}()
		}

		// Unified Element Player (Handles Audio + Tx Keying)
		playPerfectElement := func(duration, gap time.Duration) {
			mu.Lock()
			isPlaying = true
			elementStartTime = time.Now()
			currentElementDur = duration
			mu.Unlock()

			if txPort != nil {
				txPort.SetDTR(true)
				txPort.SetRTS(true)
				txLightMu.Lock()
				txActive = true
				txLastHit = time.Now()
				txLightMu.Unlock()
			}
			player.SetVolume(1.0)

			time.Sleep(duration)

			mu.Lock()
			isPlaying = false
			mu.Unlock()

			if txPort != nil {
				txPort.SetDTR(false)
				txPort.SetRTS(false)
				txLightMu.Lock()
				txActive = false
				txLightMu.Unlock()
			}
			player.SetVolume(0.0)
			time.Sleep(gap)
		}

		var wasDown bool

		for !IsStopping {
			mu.Lock()
			rdit, rdah := realtimeDit, realtimeDah
			mdit, mdah := memDit, memDah
			uLastDit := ultimaticLastDit
			memDit, memDah = false, false

			port := localPort // Strictly locked local read
			mu.Unlock()

			// Disconnect safety check
			if port == nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}

			_, err := port.GetModemStatusBits()
			if err != nil {
				if txPort != nil {
					txPort.SetDTR(false)
					txPort.SetRTS(false)
					txLightMu.Lock()
					txActive = false
					txLightMu.Unlock()
				}
				player.SetVolume(0.0)
				wasDown = false
				updateUI(
					" [red]Device disconnected...",
					"\n\n *** %s ***\n\n Connection lost.\n\n Reinsert your key device, or press ESC to exit.",
				)
				time.Sleep(500 * time.Millisecond)
				break
			}

			// --- DEVICE LOGIC ---
			dev := strings.ToLower(config.User.KeyDevice)
			isContinuousTone := (dev == "cootie" || dev == "key/bug" || strings.Contains(dev, "straight") || dev == "bug" || dev == "key")

			if isContinuousTone {
				// True Continuous Audio Tone (Raw lines, no forced elements)
				keyDown := rdit
				if dev == "cootie" {
					keyDown = rdit || rdah
				}

				if keyDown && !wasDown {
					if txPort != nil {
						txPort.SetDTR(true)
						txPort.SetRTS(true)
						txLightMu.Lock()
						txActive = true
						txLastHit = time.Now()
						txLightMu.Unlock()
					}
					player.SetVolume(1.0)
					wasDown = true
				} else if !keyDown && wasDown {
					if txPort != nil {
						txPort.SetDTR(false)
						txPort.SetRTS(false)
						txLightMu.Lock()
						txActive = false
						txLightMu.Unlock()
					}
					player.SetVolume(0.0)
					wasDown = false
				}
			} else {
				// PADDLES (Iambic A, Iambic B, Ultimatic)
				playElement, nextLastWasDit := EvaluatePaddleAction(config.User.KeyDevice, rdit, rdah, mdit, mdah, lastWasDit, uLastDit)

				if playElement == 1 || playElement == 2 {
					tp := GetTiming(false, config.User)
					gap := time.Duration(tp.InterElement * float64(time.Second))
					if playElement == 1 {
						playPerfectElement(time.Duration(tp.DitDuration*float64(time.Second)), gap)
					} else {
						playPerfectElement(time.Duration(tp.DahDuration*float64(time.Second)), gap)
					}
					lastWasDit = nextLastWasDit
				}
			}

			time.Sleep(1 * time.Millisecond)
		}
	}

	updateUI("STOP", "")
}
