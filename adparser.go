package main

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// ADStructure is one Type-Length-Value record from a BLE advertisement payload.
type ADStructure struct {
	Type     uint8  `json:"type"`
	TypeName string `json:"typeName"`
	Bytes    []byte `json:"-"`
	Hex      string `json:"hex"`
	Decoded  string `json:"decoded"` // human-readable interpretation, "" if none
}

// ParsedAdv holds the full structured breakdown of an advertisement payload.
type ParsedAdv struct {
	Structures []ADStructure `json:"structures"`
	Summary    []string      `json:"summary"` // e.g. "iBeacon UUID ... major ... minor ..."
}

var adTypeNames = map[uint8]string{
	0x01: "Flags",
	0x02: "16-bit Service UUIDs (incomplete)",
	0x03: "16-bit Service UUIDs (complete)",
	0x04: "32-bit Service UUIDs (incomplete)",
	0x05: "32-bit Service UUIDs (complete)",
	0x06: "128-bit Service UUIDs (incomplete)",
	0x07: "128-bit Service UUIDs (complete)",
	0x08: "Shortened Local Name",
	0x09: "Complete Local Name",
	0x0A: "Tx Power Level",
	0x0B: "Class of Device",
	0x10: "Security Manager TK Value",
	0x11: "Security Manager Out of Band Flags",
	0x12: "Preferred Connection Parameters",
	0x14: "Discovery Timeout",
	0x15: "Service Data (16-bit UUID)",
	0x16: "Service Data (16-bit UUID)",
	0x17: "Mesh Message (unofficial)",
	0x18: "LE Bluetooth Device Address",
	0x19: "LE Role",
	0x1A: "256-bit EC Key (mesh provisioning)",
	0x1B: "Name / Appearance (unofficial)",
	0x1C: "Mesh Message",
	0x1D: "Mesh Beacon",
	0x1E: "Public Broadcast Anchor",
	0x20: "Service Data (32-bit UUID)",
	0x21: "Service Data (128-bit UUID)",
	0x22: "URI (deprecated)",
	0x23: "3D Information Data",
	0x24: "Appearance",
	0x25: "Advertising Interval Timeout (unofficial)",
	0x26: "LE Bluetooth Device Address (complete)",
	0x27: "LE Role (unofficial)",
	0x28: "Extended Inquiry Response (unofficial)",
	0x29: "Periodic Advertising Responses",
	0x2A: "Adaptive Frequency Hopping Initiation Data",
	0x2B: "Periodic Advertising Sync Parameters",
	0x2C: "Broadcast Name",
	0x2D: "Encrypted Advertising Data",
	0x2E: "Periodic Advertising Response Timing Information",
	0x2F: "Public Broadcast Anchor (unofficial)",
	0x30: "Basic PA (unofficial)",
	0x31: "Node ID (unofficial)",
	0x32: "Advertising Direction (unofficial)",
	0x33: "Configuration Channel (unofficial)",
	0x34: "POS Calibration Notification (unofficial)",
	0x35: "ODM Data (unofficial)",
	0x36: "UWB Radio ID (unofficial)",
	0x37: "Resolvable Set Identifier (unofficial)",
	0x38: "Multiple Advertisement Formats",
	0x39: "Multiple Schedules",
	0x3A: "Extended Appearance",
	0x3B: "Local Name (unofficial)",
	0x3C: "Periodic Advertising Interface Timing",
	0x3D: "Brand Name (unofficial)",
	0x3E: "Broadcast Scan Request (unofficial)",
	0x3F: "Continuity (unofficial)",
	0xFF: "Manufacturer Specific Data",
}

// commonAdTypes is the curated list exposed in the UI filter dropdown.
// Codes follow the Bluetooth Core Supplement "Assigned Numbers" table.
var commonAdTypes = []struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}{
	{0x01, "Flags"},
	{0x02, "16-bit Service UUIDs (more available)"},
	{0x03, "16-bit Service UUIDs (complete list)"},
	{0x04, "32-bit Service UUIDs (more available)"},
	{0x05, "32-bit Service UUIDs (complete list)"},
	{0x06, "128-bit Service UUIDs (more available)"},
	{0x07, "128-bit Service UUIDs (complete list)"},
	{0x08, "Shortened Local Name"},
	{0x09, "Complete Local Name"},
	{0x0A, "Tx Power Level"},
	{0x12, "Preferred Connection Parameters"},
	{0x16, "Service Data (16-bit UUID) — Eddystone etc."},
	{0x18, "LE Bluetooth Device Address"},
	{0x19, "LE Role"},
	{0x20, "Service Data (32-bit UUID)"},
	{0x21, "Service Data (128-bit UUID)"},
	{0x24, "Mesh Message"},
	{0x25, "Mesh Beacon"},
	{0x26, "BigInfo (Isochronous broadcast)"},
	{0x27, "Broadcast Name"},
	{0x28, "Encrypted Advertising Data"},
	{0x29, "Periodic Advertising Responses"},
	{0x2B, "Periodic Advertising Sync Parameters"},
	{0x2C, "Extended Local Name (AD)"},
	{0x30, "Advertising Interval (32-bit AD types)"},
	{0x31, "Resolvable Set Identifier (RSI)"},
	{0x32, "Advertising Direction"},
	{0xFF, "Manufacturer Specific Data — iBeacon/AltBeacon etc."},
}

// AdTypeCatalog returns the curated AD-type list for the frontend filter UI.
func AdTypeCatalog() []map[string]any {
	out := make([]map[string]any, 0, len(commonAdTypes))
	for _, t := range commonAdTypes {
		out = append(out, map[string]any{"code": t.Code, "name": t.Name})
	}
	return out
}

func adTypeName(t uint8) string {
	if n, ok := adTypeNames[t]; ok {
		return n
	}
	return fmt.Sprintf("Unknown (0x%02X)", t)
}

// parseAdvertisement walks the raw AD structures and decodes every field it
// recognises, including iBeacon / AltBeacon / Eddystone payloads.
func parseAdvertisement(raw []byte) ParsedAdv {
	var p ParsedAdv
	i := 0
	for i+1 < len(raw) {
		length := int(raw[i])
		// BLE AD length is at most 31; anything larger or truncated means the
		// payload ends here (some stacks append a trailing 0x00 padding byte).
		if length == 0 || length > 31 || i+1+length > len(raw) {
			break
		}
		t := raw[i+1]
		data := raw[i+2 : i+1+length]
		ad := ADStructure{
			Type:     t,
			TypeName: adTypeName(t),
			Bytes:    data,
			Hex:      strings.ToUpper(hex.EncodeToString(data)),
		}
		switch t {
		case 0x01: // Flags
			ad.Decoded = decodeFlags(data)
		case 0x02, 0x03, 0x04, 0x05, 0x06, 0x07: // service UUID lists
			ad.Decoded = decodeUUIDList(t, data)
		case 0x08, 0x09, 0x1B, 0x2C: // local names (complete / short / unofficial variants)
			if utf8ish(data) {
				ad.Decoded = string(data)
			} else {
				ad.Decoded = "(non-ASCII name bytes)"
			}
		case 0x0A: // tx power
			if len(data) >= 1 {
				ad.Decoded = fmt.Sprintf("%d dBm", int8(data[0]))
			}
		case 0x0D: // class of device (3 octets)
			if len(data) >= 3 {
				cod := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16
				ad.Decoded = fmt.Sprintf("0x%06X (major service bits=0x%05X)", cod, cod&0xF80000>>15)
			}
		case 0x12: // preferred connection parameters
			if len(data) >= 8 {
				minI := uint16(data[0]) | uint16(data[1])<<8
				maxI := uint16(data[2]) | uint16(data[3])<<8
				lat := uint16(data[4]) | uint16(data[5])<<8
				to := uint16(data[6]) | uint16(data[7])<<8
				ad.Decoded = fmt.Sprintf("min=%.1fms max=%.1fms latency=%d timeout=%.1fs",
					float64(minI)*1.25, float64(maxI)*1.25, lat, float64(to)*0.01)
			}
		case 0x14: // discovery timeout
			if len(data) >= 2 {
				ad.Decoded = fmt.Sprintf("%d s", uint16(data[0])|uint16(data[1])<<8)
			}
		case 0x16, 0x20, 0x21, 0x22: // service data (16/32/128-bit UUID)
			ad.Decoded = decodeServiceData(t, data)
		case 0x18: // LE Bluetooth Device Address
			if len(data) >= 7 {
				ad.Decoded = fmt.Sprintf("%s (%s)",
					strings.ToUpper(reverseHex(data[1:6])),
					map[byte]string{0: "public", 1: "random"}[data[6]])
			}
		case 0x19: // LE Role
			if len(data) >= 1 {
				ad.Decoded = map[string]string{"0": "peripheral only", "1": "central only", "2": "both"}[fmt.Sprint(data[0])]
			}
		case 0x1D: // Mesh Beacon
			if len(data) >= 4 {
				ad.Decoded = fmt.Sprintf("mesh netID=%s features=0x%02X IV-index=%d",
					strings.ToUpper(hex.EncodeToString(data[:1])), data[1],
					uint32(data[2])|uint32(data[3])<<8)
			}
		case 0x23: // 3D Information Data
			if len(data) >= 1 {
				ad.Decoded = fmt.Sprintf("3D data (type 0x%02X)", data[0])
			}
		case 0x24: // Appearance — 16-bit little-endian, decoded via embedded GAP catalogue
			if len(data) >= 2 {
				v := uint16(data[0]) | uint16(data[1])<<8
				ad.Decoded = fmt.Sprintf("%s (0x%04X)", decodeAppearance(v), v)
			}
		case 0x27: // Advertising Interval Timeout (16-bit, unit 10 ms)
			if len(data) >= 2 {
				ad.Decoded = fmt.Sprintf("%d ms", (uint16(data[0])|uint16(data[1])<<8)*10)
			}
		case 0x28: // Num Remaining Public Broadcasts
			if len(data) >= 1 {
				ad.Decoded = fmt.Sprintf("%d broadcasts remaining", data[0])
			}
		case 0x29, 0x2A: // Periodic Advertising Sync Info / Responses
			ad.Decoded = fmt.Sprintf("Periodic Adv (effect=%d)", func() int {
				if len(data) > 0 {
					return int(data[0])
				}
				return -1
			}())
		case 0x2B, 0x2D, 0x2E: // Periodic Adv sync params / AD count / other periodic fields
			if len(data) >= 1 {
				ad.Decoded = fmt.Sprintf("periodic value=%d", data[0])
			}
		case 0x31: // Resolvable Set Identifier (RSI)
			ad.Decoded = strings.ToUpper(hex.EncodeToString(data))
		case 0x32: // Advertising Direction
			if len(data) >= 1 {
				ad.Decoded = map[string]string{"0": "from advertiser to scanner", "1": "from scanner to advertiser"}[fmt.Sprint(data[0]&1)]
			}
		case 0x33: // Chi Value
			ad.Decoded = strings.ToUpper(hex.EncodeToString(data))
		case 0x34: // PubBcast Address
			ad.Decoded = strings.ToUpper(hex.EncodeToString(data))
		case 0x35, 0x36: // Range Transfer Time / Tracked Size Update
			if len(data) >= 2 {
				ad.Decoded = fmt.Sprintf("%d ms", uint16(data[0])|uint16(data[1])<<8)
			}
		case 0x38: // Multiple Advertisement Formats
			ad.Decoded = "device supports legacy + extended advertising"
		case 0x39: // Multiple Schedules
			ad.Decoded = "device uses multiple scheduling configurations"
		case 0x3A: // Extended Appearance
			if len(data) >= 3 {
				ad.Decoded = fmt.Sprintf("appearance 0x%04X (icon type %d)",
					uint16(data[0])|uint16(data[1])<<8, data[2])
			}
		case 0x3C: // Physical Environment
			if len(data) >= 1 {
				ad.Decoded = fmt.Sprintf("environment code %d", data[0])
			}
		case 0x3D: // Brand Name
			if utf8ish(data) {
				ad.Decoded = string(data)
			}
		case 0x40: // Beacon UUID (Broadcast Scan Request / PaBw)
			ad.Decoded = strings.ToUpper(hex.EncodeToString(data))
		case 0xFF: // manufacturer data
			ad.Decoded = decodeManufacturerData(data, &p)
		}
		p.Structures = append(p.Structures, ad)
		i += 1 + length
	}
	return p
}

func decodeFlags(d []byte) string {
	if len(d) < 1 {
		return ""
	}
	f := d[0]
	var parts []string
	if f&0x01 != 0 {
		parts = append(parts, "LE Extended Mode")
	}
	if f&0x02 != 0 {
		parts = append(parts, "BR/EDR Not Supported")
	}
	if f&0x04 != 0 {
		parts = append(parts, "Simultaneous LE+BR/EDR (Controller)")
	}
	if f&0x08 != 0 {
		parts = append(parts, "Simultaneous LE+BR/EDR (Host)")
	}
	if f&0x10 != 0 {
		parts = append(parts, "Reserved")
	}
	return strings.Join(parts, ", ")
}

func swap16(u uint16) uint16 { return u<<8 | u>>8 }

func decodeUUIDList(t uint8, d []byte) string {
	var out []string
	switch t {
	case 0x02, 0x03:
		for i := 0; i+2 <= len(d); i += 2 {
			u := swap16(uint16(d[i])<<8 | uint16(d[i+1]))
			out = append(out, fmt.Sprintf("0x%04X", u))
		}
	case 0x16: // handled elsewhere; keep linter happy on odd inputs
	default:
		// 128-bit UUIDs are stored little-endian over the air; render big-endian.
		if (t == 0x06 || t == 0x07) && len(d) >= 16 {
			for i := 0; i+16 <= len(d); i += 16 {
				b := d[i : i+16]
				be := make([]byte, 16)
				for j := 0; j < 16; j++ {
					be[j] = b[15-j]
				}
				h := strings.ToUpper(hex.EncodeToString(be))
				out = append(out, h[:8]+"-"+h[8:12]+"-"+h[12:16]+"-"+h[16:20]+"-"+h[20:])
			}
		} else {
			for i := 0; i+4 <= len(d); i += 4 {
				b := d[i : i+4]
				u := uint32(b[3])<<24 | uint32(b[2])<<16 | uint32(b[1])<<8 | uint32(b[0])
				out = append(out, fmt.Sprintf("0x%08X", u))
			}
		}
	}
	return strings.Join(out, ", ")
}

// reverseHex renders little-endian byte order as a big-endian hex string.
func reverseHex(b []byte) string {
	r := make([]byte, len(b))
	for i := range b {
		r[len(b)-1-i] = b[i]
	}
	return hex.EncodeToString(r)
}

func utf8ish(d []byte) bool {
	for _, c := range d {
		if c < 0x20 || c > 0x7E {
			return false
		}
	}
	return len(d) > 0
}

// Eddystone-URL expansion tables — Google Eddystone spec, Frame Type 0x10.
// Scheme codes and URL suffix codes occupy bytes 0x00-0x07; ASCII '!' is 0x21,
// so any byte < 0x08 is a compressed code, everything else is a literal char.
var edSchemePrefix = [4]string{"http://www.", "https://www.", "http://", "https://"}

var edCodeSuffix = [8]string{".com", ".org", ".net", ".edu", ".com/", ".org/", ".net/", ".edu/"}

func decodeEddystoneURLPayload(d []byte) string {
	// Eddystone-URL frame layout (Google Eddystone spec):
	//   d[0]=frame type (0x10) · d[1]=tx power · d[2]=URL scheme code · d[3..]=URL body
	if len(d) < 4 {
		return ""
	}
	scheme := int(d[2])
	if scheme > 3 {
		return "" // unknown scheme code — not a valid URL frame
	}
	var sb strings.Builder
	sb.WriteString(edSchemePrefix[scheme])
	for _, c := range d[3:] { // URL body
		if c < 0x08 {
			sb.WriteString(edCodeSuffix[c])
		} else if c >= 0x21 && c <= 0x7E {
			sb.WriteByte(c) // printable ASCII literal
		}
		// bytes outside the spec's encodable set are skipped
	}
	return sb.String()
}

func decodeServiceData(t uint8, d []byte) string {
	if len(d) == 0 {
		return ""
	}
	switch t {
	case 0x16: // 16-bit UUID + data
		if len(d) >= 2 {
			uuid := swap16(uint16(d[0])<<8 | uint16(d[1]))
			payload := d[2:]
			if uuid == 0xFEAA && len(payload) >= 1 {
				switch payload[0] {
				case 0x00:
					if len(payload) >= 18 {
						ns := strings.ToUpper(hex.EncodeToString(payload[1:11]))
						inst := strings.ToUpper(hex.EncodeToString(payload[11:17]))
						rbe := int8(payload[17])
						return fmt.Sprintf("Eddystone-UID namespace=%s instance=%s tx@0m=%ddBm", ns, inst, rbe)
					}
				case 0x10:
					if url := decodeEddystoneURLPayload(payload); url != "" {
						return fmt.Sprintf("Eddystone-URL txPower=%ddBm url=%s", int8(payload[1]), url)
					}
				case 0x20:
					if len(payload) >= 9 {
						vb := uint16(payload[1])<<8 | uint16(payload[2])
						return fmt.Sprintf("Eddystone-TLM battery=%dmv temp=%s°C uptime=%ds",
							vb, formatTemp(int16(uint16(payload[3])<<8|uint16(payload[4]))),
							uint32(payload[5])<<24|uint32(payload[6])<<16|uint32(payload[7])<<8|uint32(payload[8]))
					}
				}
				return "Eddystone frame 0x" + hex.EncodeToString(payload[:1])
			}
			return fmt.Sprintf("UUID 0x%04X data=%s", uuid, strings.ToUpper(hex.EncodeToString(payload)))
		}
	case 0x20: // 32-bit UUID
		if len(d) >= 4 {
			uuid := uint32(d[3])<<24 | uint32(d[2])<<16 | uint32(d[1])<<8 | uint32(d[0])
			return fmt.Sprintf("UUID 0x%08X data=%s", uuid, strings.ToUpper(hex.EncodeToString(d[4:])))
		}
	case 0x21: // 128-bit UUID
		if len(d) >= 16 {
			return fmt.Sprintf("UUID %s data=%s", decodeUUIDList(0x06, d[:16]), strings.ToUpper(hex.EncodeToString(d[16:])))
		}
	}
	return ""
}

func formatTemp(t int16) string {
	return strconv.FormatFloat(float64(t)/100.0, 'f', 2, 64)
}

// decodeManufacturerData decodes Apple iBeacon, AltBeacon and generic payloads.
// It also appends high-level summary lines to p.Summary.
func decodeManufacturerData(d []byte, p *ParsedAdv) string {
	if len(d) < 2 {
		return ""
	}
	cid := uint16(d[0]) | uint16(d[1])<<8 // little-endian company id
	body := d[2:]
	name := companyName(cid) // full Bluetooth SIG table (embedded numbers DB)
	if name == "" {
		name = companyNames[int(cid)] // small hand-written fallback
	}
	if name == "" {
		name = fmt.Sprintf("company 0x%04X", cid)
	}
	switch {
	case cid == 0x004C && len(body) >= 23 && body[0] == 0x02 && body[1] == 0x15:
		h := strings.ToUpper(hex.EncodeToString(body[2:18]))
		uuid := h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
		major := uint16(body[18])<<8 | uint16(body[19])
		minor := uint16(body[20])<<8 | uint16(body[21])
		meas := int8(body[22])
		p.Summary = append(p.Summary,
			fmt.Sprintf("iBeacon: UUID=%s Major=%d Minor=%d MeasuredPower=%ddBm", uuid, major, minor, meas))
		return fmt.Sprintf("Apple iBeacon UUID=%s major=%d minor=%d measuredPower=%ddBm", uuid, major, minor, meas)
	case cid == 0x004C && len(body) >= 1 && body[0] == 0x1A:
		// Apple Continuity / Nearby pairing blob — show only length/type bytes.
		if len(body) >= 25 {
			pk := strings.ToUpper(hex.EncodeToString(body[1:25]))
			return fmt.Sprintf("Apple Continuity (type 0x1A) public key=%s…", pk[:16])
		}
		return "Apple Continuity (type 0x1A)"
	case cid == 0x0118 && len(body) >= 23 && body[0] == 0xBE && body[1] == 0xAC:
		h := strings.ToUpper(hex.EncodeToString(body[2:18]))
		id := h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
		major := uint16(body[18])<<8 | uint16(body[19])
		minor := uint16(body[20])<<8 | uint16(body[21])
		meas := int8(body[22])
		p.Summary = append(p.Summary,
			fmt.Sprintf("AltBeacon: ID=%s Major=%d Minor=%d MeasuredPower=%ddBm", id, major, minor, meas))
		return fmt.Sprintf("AltBeacon id=%s major=%d minor=%d measuredPower=%ddBm", id, major, minor, meas)
	case cid == 0x00E0 && len(body) >= 1 && body[0] == 0x02:
		// Google location beacon legacy format.
		if len(body) >= 8 {
			h := strings.ToUpper(hex.EncodeToString(body[1:7]))
			p.Summary = append(p.Summary, fmt.Sprintf("Google Beacon: ID=%s", h))
			return fmt.Sprintf("Google Location Beacon id=%s", h)
		}
	}
	return fmt.Sprintf("%s data=%s", name, strings.ToUpper(hex.EncodeToString(body)))
}

// companyNames is a compact table of common Bluetooth SIG assigned companies.
var companyNames = map[int]string{
	0x0000: "Ericsson", 0x0001: "Nokia", 0x0002: "Intel", 0x0003: "Broadcom",
	0x0006: "Microsoft", 0x000D: "Texas Instruments", 0x000E: "AT&T",
	0x004C: "Apple", 0x0059: "Nordic Semiconductor", 0x0060: "STMicroelectronics",
	0x00E0: "Google", 0x0104: "Philips Lighting (Signify)",
	0x0118: "AltBeacon Consortium", 0x0159: "NRFI (Estimote)", 0x01A7: "Samsung",
	0x025D: "Huawei", 0x02D0: "Xiaomi", 0x038F: "Meta Platforms",
	0x05A7: "Kontakt.io", 0x0688: "HP", 0x0765: "Amazon",
	0x000C: "Cirrus Logic", 0x00AC: "CSR (Qualcomm)",
	0x0120: "Quintic", 0x0131: "Stollmann", 0x0171: "Wunderlist",
	0x0215: "GE Healthcare", 0x0279: "Parrot", 0x0499: "Ruio Tech",
	0x06D9: "Sonos", 0x0DD4: "OZO", 0x11C5: "99Rooftops",
	0x1234: "Minew", 0x1B57: "Transcense", 0x07A2: "Abaxos",
	0x0655: "HUAWEI Technologies",
	0xFFFF: "Test / Unassigned",
}

// advTypesFromRaw returns just the type names (legacy helper used by store).
func advTypesFromRaw(raw []byte) []string {
	p := parseAdvertisement(raw)
	seen := map[string]bool{}
	var out []string
	for _, s := range p.Structures {
		if !seen[s.TypeName] {
			seen[s.TypeName] = true
			out = append(out, s.TypeName)
		}
	}
	return out
}
