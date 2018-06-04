package main

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gvalkov/golang-evdev"
)

// Need to set a duration of no activity after which we assume that the scan completed. The
// way the input from the scanner is set up we don't really know when a scan finishes, so we
// assume a timeout. 10ms seemed about right in testing.
const (
	timerDuration = 10 * time.Millisecond
)

// processBarcodes is just a base for a process that waits for a barcode to be broadcast on
// the channel and prints it to the terminal. Not particular useful in most use cases, but helps
// with testing.
func processBarcodes(barcode chan string) {
	var code string
	for {
		code = <-barcode
		fmt.Println("Scanned: " + code)
	}
}

// processCharacter handles translating of keycodes to characters and determines state of
// shift keys and other modifiers.
func processCharacter(key string, capNext bool) (string, bool) {
	if strings.Contains(key, "LEFTSHIFT") || strings.Contains(key, "RIGHTSHIFT") {
		capNext = true
		key = ""
	} else {
		key = strings.TrimPrefix(key, "KEY_")
		if !capNext {
			key = strings.ToLower(key)
		} else {
			capNext = false
		}
		switch key {
		case "space":
			key = " "
		case "slash":
			key = "/"
		case "minus":
			key = "-"
		case "dot":
			key = "."
		case "comma":
			key = ","
		case "SEMICOLON":
			key = ":"
		case "semicolon":
			key = ";"
			// TODO: Add more if we need to decode additional characters
		}
	}
	return key, capNext
}

// processEvents is run as a process waiting for events to be broadcast. Once an event appears
// the keycode map is consulted for the character and processCharacter is called to handle whatever
// character the keycode corresponds to. processEvents also handles the timeout of when a scan
// is completed; when this happens the buffer that accumulates the processed characters from a
// given event is sent through a channel elsewhere
func processEvents(event chan evdev.InputEvent, scannedBarcode chan string, timeout *time.Timer) {
	var barcode bytes.Buffer
	var capNext bool
	var key string
	for {
		select {
		case ev := <-event:
			// Ignore key-ups and statuses. Also ignore anything that isn't a key
			if ev.Value == 1 && ev.Type == evdev.EV_KEY {
				val, haskey := evdev.KEY[int(ev.Code)]
				if haskey {
					key = val
				} else { // can't find the key in our map
					key = "?"
				}
				key, capNext = processCharacter(key, capNext)
				barcode.WriteString(key)
				timeout.Reset(timerDuration)
			}
		case <-timeout.C: // assuming no more characters coming in this barcode
			if barcode.Len() > 0 {
				capNext = false
				scannedBarcode <- barcode.String() // pass it along elsewhere
				barcode.Reset()                    // reset for next round
			}
		}
	}
}

func main() {
	devices, _ := evdev.ListInputDevices()

	// TODO: This currently assumes a single barcode scanner from Zebra (aka Symbol Technologies)
	// We may need to expand this, as some stations might have multiple wireless scanners.
	// TODO: Add support for badge reader
	scannerLoc := ""
	for _, dev := range devices {
		if strings.Contains(dev.Name, "Symbol Technologies") {
			scannerLoc = dev.Fn
			break
		}
	}
	if scannerLoc == "" {
		fmt.Println("Cound not find a scanner, error.")
		os.Exit(1)
	} else {
		fmt.Printf("Found scanner at %s\n", scannerLoc)
	}

	device, err := evdev.Open(scannerLoc)
	if err != nil {
		panic(err)
	}

	// Need to grab the device so that we don't get additional input from the HID
	// portion of the scanner connection
	err = device.Grab()
	if err != nil {
		panic(err)
	}
	defer device.Release()

	// Ran into some trouble during testing when closing out through ctrl+c with the input not being
	// released. The code below cleans up on a terminate signal.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for range c {
			err = device.Release()
			if err != nil {
				panic(err)
			}
			os.Exit(1)
		}
	}()

	event := make(chan evdev.InputEvent, 256)
	timeout := time.NewTimer(timerDuration)
	scannedBarcode := make(chan string, 8)

	// processBarcodes is only dumping received barcodes to the terminal. For other usage this should probably
	// be something else
	go processBarcodes(scannedBarcode)
	go processEvents(event, scannedBarcode, timeout)

	var events []evdev.InputEvent
	fmt.Printf("Listening for events ...\n")

	for {
		events, err = device.Read()
		for i := range events {
			/*str := format_event(&events[i])
			if str != "" {
				fmt.Println(str)
			}*/
			event <- events[i]
		}
	}
}
