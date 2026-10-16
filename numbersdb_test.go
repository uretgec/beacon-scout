package main

import "testing"

func TestHexToUint16(t *testing.T) {
	cases := []struct {
		in   string
		want uint16
	}{
		{"180f", 0x180F},
		{"004C", 0x004C},
		{"FFFF", 0xFFFF},
	}
	for _, tc := range cases {
		got, err := hexToUint16(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("hexToUint16(%q) = %v, err %v; want 0x%04X", tc.in, got, err, tc.want)
		}
	}
	if _, err := hexToUint16("zz"); err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestServiceNamesFor(t *testing.T) {
	// 16-bit UUIDs resolve through the embedded SIG table; 128-bit pass through.
	got := serviceNamesFor([]string{"180f", "12345678-1234-5678-1234-567890abcdef"})
	if got == "" {
		t.Fatal("empty result")
	}
	t.Logf("serviceNamesFor -> %q", got)
}
