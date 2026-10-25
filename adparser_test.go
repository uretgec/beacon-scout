package main

import (
	"encoding/hex"
	"strings"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseIBeacon(t *testing.T) {
	raw := mustHex(t, "02 01 06 1a ff 4c 00 02 15 fb0b8cf5 029e 4e59 be3b 27bbf248d5bc 0001 0002 c5")
	p := parseAdvertisement(raw)
	if len(p.Summary) != 1 || !strings.Contains(p.Summary[0], "UUID=FB0B8CF5-029E-4E59-BE3B-27BBF248D5BC") {
		t.Fatalf("bad summary: %#v", p.Summary)
	}
	if !strings.Contains(p.Summary[0], "Major=1 Minor=2 MeasuredPower=-59dBm") {
		t.Fatalf("bad major/minor/power: %s", p.Summary[0])
	}
	found := false
	for _, s := range p.Structures {
		if s.Type == 0xFF && strings.HasPrefix(s.Decoded, "Apple iBeacon") {
			found = true
		}
	}
	if !found {
		t.Fatal("no decoded mfg struct")
	}
}

func TestParseEddystoneURL(t *testing.T) {
	// frame 0x10, tx -20, scheme https://www. (0x01), "example", .com (0x00)
	raw := mustHex(t, "02 01 06 12 16 aa fe 10 ec ff 01 65 78 61 6d 70 6c 65 00")
	p := parseAdvertisement(raw)
	ok := false
	for _, s := range p.Structures {
		if strings.Contains(s.Decoded, "Eddystone-URL") && strings.Contains(s.Decoded, "https://www.example.com") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("URL not decoded: %#v", p.Structures)
	}
}

func TestParseAltBeacon(t *testing.T) {
	raw := mustHex(t, "1b ff 18 01 be ac fb0b8cf5029e4e59be3b27bbf248d5bc 0001 0002 c5 00")
	p := parseAdvertisement(raw)
	if len(p.Summary) != 1 || !strings.Contains(p.Summary[0], "AltBeacon:") {
		t.Fatalf("altbeacon not parsed: %#v", p.Summary)
	}
}

func TestParseFlagsAndName(t *testing.T) {
	name := append([]byte{0x06, 0x09}, []byte("MyDev")...)
	raw := append([]byte{0x02, 0x01, 0x06}, name...)
	p := parseAdvertisement(raw)
	var flags, nm bool
	for _, s := range p.Structures {
		if s.Type == 0x01 && strings.Contains(s.Decoded, "BR/EDR Not Supported") {
			flags = true
		}
		if s.Type == 0x09 && s.Decoded == "MyDev" {
			nm = true
		}
	}
	if !flags || !nm {
		t.Fatalf("flags=%v name=%v", flags, nm)
	}
}

func TestTruncatedPayloadNoPanic(t *testing.T) {
	raw := mustHex(t, "ff 4c 00 02 15 fb")
	_ = parseAdvertisement(raw) // must not panic
}

// TestRebuildRawAD verifies the macOS fallback that reconstructs a TLV AD
// stream from parsed fields. It exercises the low-level encoders directly
// (no bluetooth types needed): 16-bit UUID little-endian, manufacturer data
// with LE company ID, and standard TLV framing.
func TestRebuildRawAD(t *testing.T) {
	// appendAD: length byte = payload + type byte.
	buf := appendAD(nil, 0x09, []byte("Test"))
	want := "0509" + hex.EncodeToString([]byte("Test"))
	if got := hex.EncodeToString(buf); got != want {
		t.Fatalf("appendAD = %s, want %s", got, want)
	}

	// appendUUIDs with a Base-UUID short form is impossible without the
	// bluetooth package here, but the 128-bit path only depends on
	// uuidBytes; check TLV framing for a known mfg-style blob instead.
	d := append([]byte{0x4c, 0x00}, []byte{0x02, 0x15}...) // Apple, LE
	out := appendAD(nil, 0xFF, d)
	if hex.EncodeToString(out) != "05ff4c000215" {
		t.Fatalf("mfg TLV = %s, want 05ff4c000215", hex.EncodeToString(out))
	}

	// Round-trip through the full parser: rebuilt bytes must parse back to
	// recognizable structures.
	raw := append(appendAD(nil, 0x01, []byte{0x06}), out...)
	parsed := parseAdvertisement(raw)
	if len(parsed.Structures) != 2 {
		t.Fatalf("expected 2 structures, got %d", len(parsed.Structures))
	}
}
