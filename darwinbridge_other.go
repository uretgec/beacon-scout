//go:build !darwin

package main

import "tinygo.org/x/bluetooth"

// No Darwin CoreBluetooth bridge on other platforms: Bytes() already carries
// the full on-air AD stream, so there are no extra fields to capture. These
// stubs keep the call sites in scanservice.go platform-neutral.
func attachDarwinExtras(_ bluetooth.ScanResult) {}

func installDarwinBridge(_ *bluetooth.Adapter) {}

func reinstallDarwinBridge(_ *bluetooth.Adapter) {}

func takeDarwinExtras() (*byte, string, *int8, *uint16) {
	return nil, "", nil, nil
}
