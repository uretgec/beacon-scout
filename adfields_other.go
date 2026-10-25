//go:build !darwin

package main

import "tinygo.org/x/bluetooth"

// macosExtraFields returns advertisement fields that the generic
// AdvertisementPayload interface does not expose. On Linux/Windows the raw
// on-air bytes are available via Bytes(), so nothing extra is needed here.
func macosExtraFields(_ bluetooth.AdvertisementPayload) (flags *byte, shortName string, txPower *int8, appearance *uint16) {
	return nil, "", nil, nil
}
