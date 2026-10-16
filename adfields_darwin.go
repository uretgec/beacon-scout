//go:build darwin

package main

import "tinygo.org/x/bluetooth"

// macosExtraFields returns advertisement fields that the generic
// AdvertisementPayload interface does not expose on macOS. The values are
// captured by the Darwin scan bridge (darwinbridge_darwin.go) directly from
// cbgo.AdvFields — CBAdvertisementDataTxPowerLevelKey and
// CBAdvertisementDataConnectableKey — which tiny-go/bluetooth v0.16.0 drops
// during ScanResult conversion. ingest() calls this synchronously right after
// attachDarwinExtras(), so the one-shot handoff is always fresh. shortName and
// appearance have no CoreBluetooth equivalent and stay nil.
func macosExtraFields(_ bluetooth.AdvertisementPayload) (flags *byte, shortName string, txPower *int8, appearance *uint16) {
	return takeDarwinExtras()
}
