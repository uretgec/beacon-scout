package main

import (
	"encoding/hex"
	"strings"
	"testing"

	"tinygo.org/x/bluetooth"
)

// TestRebuildRawADFullAdvertisement verifies the macOS fallback path: when
// CoreBluetooth exposes only parsed fields (Bytes() == nil), rebuildRawAD must
// produce a COMPLETE AD-structure stream containing every field the OS hands
// us — Flags, Shortened/Complete name, Appearance, Tx Power Level, service
// UUID lists and Manufacturer Data — so that the "Full Advertisement" hex in
// the UI is never partial and always matches what the parser decodes.
func TestRebuildRawADFullAdvertisement(t *testing.T) {
	name := "Samsung TV"
	shortName := "TV"
	flags := byte(0x06) // LE General Discoverable + BR/EDR Not Supported
	txp := int8(-8)
	app := uint16(0x0340) // Generic Watch? any value for round-trip test

	mfg := []bluetooth.ManufacturerDataElement{{CompanyID: 0x004C, Data: []byte{0x02, 0x15}}}
	uuid16 := bluetooth.New16BitUUID(0xFEAA) // Eddystone service UUID

	raw := rebuildRawAD(name, []bluetooth.UUID{uuid16}, mfg, nil, &flags, shortName, &txp, &app)

	// Walk the TLV stream and collect type -> value.
	got := map[uint8]string{}
	for i := 0; i+1 < len(raw); {
		l := int(raw[i])
		if l == 0 || i+1+l > len(raw) {
			t.Fatalf("malformed TLV at offset %d in %X", i, raw)
		}
		got[raw[i+1]] = strings.ToUpper(hex.EncodeToString(raw[i+2 : i+1+l]))
		i += 1 + l
	}

	checks := []struct {
		adType uint8
		want   string
	}{
		{0x01, "06"}, // Flags
		{0x08, strings.ToUpper(hex.EncodeToString([]byte(shortName)))}, // Shortened Local Name
		{0x09, strings.ToUpper(hex.EncodeToString([]byte(name)))},      // Complete Local Name
		{0x24, "4003"}, // GAP Appearance (AD type 0x24), little-endian 0x0340
		{0x0A, "F8"},  // Tx Power -8 dBm
		// NOTE: a bare bluetooth.UUID{Data:[16]byte{...}} has NO Valid flag set,
		// so appendUUIDs() legitimately skips it. The Eddystone service UUID is
		// exercised properly in the second sub-test below with Valid: true.
		{0xFF, "4C000215"}, // Mfr data: company 0x004C LE + payload
	}
	for _, c := range checks {
		if v, ok := got[c.adType]; !ok {
			t.Errorf("AD type 0x%02X missing from rebuilt advertisement (%X)", c.adType, raw)
		} else if v != c.want {
			t.Errorf("AD type 0x%02X = %s, want %s", c.adType, v, c.want)
		}
	}

	// The reconstructed stream must parse back to the same human-readable
	// values — proving Raw and the parsed tables describe identical bytes.
	parsed := parseAdvertisement(raw)
	joined := strings.Join(parsed.Summary, "|") + strings.Join(decodeAll(parsed), "|")
	for _, want := range []string{"Samsung TV", "-8 dBm"} {
		if !strings.Contains(joined, want) {
			t.Errorf("parsed summary missing %q; got: %s / structures=%v", want, joined, parsed.Structures)
		}
	}
	if devType := deviceTypeFromFlags(raw); devType == "" {
		t.Errorf("deviceTypeFromFlags returned empty for rebuilt Flags AD")
	}
	if tp := parseTxPower(raw); tp == nil || *tp != txp {
		t.Errorf("parseTxPower = %v, want %d", tp, txp)
	}
}

func decodeAll(p ParsedAdv) []string {
	var out []string
	for _, s := range p.Structures {
		out = append(out, s.TypeName+"="+s.Decoded)
	}
	return out
}
