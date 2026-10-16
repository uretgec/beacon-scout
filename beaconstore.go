package main

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

// BeaconRecord is the JSON-serializable representation of a discovered device.
type BeaconRecord struct {
	Address  string  `json:"address"`
	IsRandom bool    `json:"isRandom"`
	Name     string  `json:"name"`
	RSSI     int16   `json:"rssi"`
	MaxRSSI  int16   `json:"maxRssi"`
	MinRSSI  int16   `json:"minRssi"`
	AvgRSSI  float64 `json:"avgRssi"`
	TxPower  *int8   `json:"txPower,omitempty"`
	// Device type decoded from the AD Flags structure (Core Spec):
	// "Classic & LE", "LE Only" or "" when no Flags AD is present.
	DeviceType   string            `json:"deviceType,omitempty"`
	FirstSeen    time.Time         `json:"firstSeen"`
	LastSeen     time.Time         `json:"lastSeen"`
	Count        int               `json:"count"`
	ServiceUUIDs []string          `json:"serviceUuids"`
	CompanyIDs   []uint16          `json:"companyIds"`
	AdvTypes     []string          `json:"advTypes"`
	Manufacturer map[string]string `json:"manufacturerData"`     // companyID hex -> data hex
	ServiceData  map[string]string `json:"serviceData"`          // uuid -> data hex
	Raw          string            `json:"raw"`                  // full advertisement AD-structure hex
	RawRebuilt   bool              `json:"rawRebuilt,omitempty"` // true when reconstructed from OS fields (macOS)
	BeaconType   string            `json:"beaconType"`           // iBeacon / Eddystone / AltBeacon / ...

	// Full Bluetooth Core Spec AD-structure breakdown of the latest payload.
	Parsed ParsedAdv `json:"parsed"`

	// Decoded beacon-specific fields (populated when the format is recognised).
	IBeaconUUID    string `json:"ibeaconUuid,omitempty"`
	IBeaconMajor   int    `json:"ibeaconMajor,omitempty"`
	IBeaconMinor   int    `json:"ibeaconMinor,omitempty"`
	EdNamespace    string `json:"eddystoneNamespace,omitempty"`
	EdInstance     string `json:"eddystoneInstance,omitempty"`
	EdURL          string `json:"eddystoneUrl,omitempty"`
	GoogleBeaconID string `json:"googleBeaconId,omitempty"`

	// RssiTrend: "up" | "down" | "flat" — direction of the latest RSSI sample
	// versus the previous one (updated on every ingest).
	RssiTrend string `json:"rssiTrend,omitempty"`

	// Radio-level metadata decoded from the AD structures (Core Spec r5.x):
	// Connectability comes from the Flags AD type (0x01); periodic-advertising
	// info from the "Periodic Advertising Interface Timing" / extended headers
	// where available. AdvSID is not exposed by tinygo's ScanResult on all
	// platforms, so it stays optional (-1 = unknown).
	Connectable      bool `json:"connectable,omitempty"`
	ConnectableKnown bool `json:"connectableKnown,omitempty"`
	AdvSid           int  `json:"advSid,omitempty"`              // -1 or omitted = unknown
	PeriodicInterval int  `json:"periodicAdvInterval,omitempty"` // in 1.25 ms units
	DirectRSSI       int  `json:"directRssi,omitempty"`          // dBm, if the radio reported it

	// GAP Appearance (AD type 0x24), decoded via the embedded SIG catalogue.
	Appearance *uint16 `json:"appearance,omitempty"` // raw 16-bit value, nil when absent
	// Human-readable appearance name + category, e.g. "Laptop (Computer)".
	AppearanceName string `json:"appearanceName,omitempty"`
}

// FilterOptions describes advanced filtering criteria applied on the Go side.
// Zero values mean "disabled" unless documented otherwise.
type FilterOptions struct {
	MinRSSI      int    `json:"minRssi"`      // e.g. -80; <= -200 disables the floor
	MaxRSSI      int    `json:"maxRssi"`      // >= 200 disables the ceiling (UI default: 200)
	NameContains string `json:"nameContains"` // case-insensitive substring match on advertised name
	NameExact    string `json:"nameExact"`    // case-insensitive exact name match
	MacContains  string `json:"macContains"`  // case-insensitive substring match on address
	UUIDContains string `json:"uuidContains"` // substring match on any service UUID / iBeacon UUID (hex, no dashes needed)
	CompanyID    string `json:"companyId"`    // hex like "0x004c" or "4c" — manufacturer must contain this company
	BeaconType   string `json:"beaconType"`   // "any", "ibeacon", "eddystone-url", "eddystone-uid", "eddystone-tlm", "altbeacon", "google", "named", "unnamed", "unknown"
	HasName      bool   `json:"hasName"`      // only devices that advertise a name
	ActiveOnly   bool   `json:"activeOnly"`   // only devices seen in the last N seconds
	ActiveWindow int    `json:"activeWindow"` // seconds for ActiveOnly window

	// ---- Bluetooth Core Spec v5.x (Part C, Ad Structures) filters ----

	// AdTypes: keep records whose advertisement contains ALL listed AD type codes.
	// Codes per Core Supplement assignment table, e.g. 0xFF Manufacturer Data,
	// 0x16 Service Data (16-bit UUID), 0x09 Complete Local Name.
	AdTypes []int `json:"adTypes,omitempty"`

	// MajorExact / MinorExact: iBeacon|AltBeacon major/minor exact match (0 = disabled).
	MajorExact int `json:"majorExact,omitempty"`
	MinorExact int `json:"minorExact,omitempty"`

	// EddystoneURLContains: substring match (case-insensitive) on the decoded URL frame.
	EddystoneURLContains string `json:"eddystoneUrlContains,omitempty"`

	// NamespaceHex / InstanceHex: Eddystone-UID namespace (10 hex chars) or
	// instance (6 hex chars) prefix match.
	NamespaceHex string `json:"namespaceHex,omitempty"`
	InstanceHex  string `json:"instanceHex,omitempty"`

	// Connectable: "" any | "yes" connectable | "no" non-connectable (from AD Types flags).
	Connectable string `json:"connectable,omitempty"`

	// AddressRandom: "" any | "yes" random-resolvable/public | "no" public.
	AddressRandom string `json:"addressRandom,omitempty"`

	// MinTxPower / MaxTxPower: dBm bounds on advertised Tx Power Level; 0/unset = disabled pair.
	MinTxPower int `json:"minTxPower,omitempty"`
	MaxTxPower int `json:"maxTxPower,omitempty"`

	// MinHits: exclude devices seen fewer than N times (0 = disabled).
	MinHits int `json:"minHits,omitempty"`

	// DeviceType: "" any | "le" LE Only | "dual" Classic & LE |
	// "noflags" devices that carry no AD Flags structure. Decoded from the
	// Flags AD type (0x01) per Core Spec.
	DeviceType string `json:"deviceType,omitempty"`

	// ---- Additional Core-Spec-level filters ----

	// RawContains: substring match (hex, case-insensitive, dashes/spaces ignored)
	// against the complete raw advertisement payload — lets users search any
	// byte pattern defined by the spec without knowing which AD type carries it.
	RawContains string `json:"rawContains,omitempty"`

	// ServiceUUIDExact: 16-bit ("0x180F") or full 128-bit UUID that must appear
	// in the advertised service UUID lists.
	ServiceUUIDExact string `json:"serviceUuidExact,omitempty"`

	// NamePrefix / NameSuffix: case-insensitive starts-with / ends-with on name.
	NamePrefix string `json:"namePrefix,omitempty"`
	NameSuffix string `json:"nameSuffix,omitempty"`

	// RssiTrend: "" any | "up" last RSSI improved vs previous | "down" worsened.
	RssiTrend string `json:"rssiTrend,omitempty"`

	// AppearanceExact / AppearanceCategory: GAP Appearance filter backed by the
	// embedded bluetooth-numbers-database (gap_appearance.json). Exact is a hex
	// value ("0x0080"); Category matches the SIG category name case-insensitively
	// ("phone", "keyboard", …).
	AppearanceExact    string `json:"appearanceExact,omitempty"`
	AppearanceCategory string `json:"appearanceCategory,omitempty"`
}

type beaconEntry struct {
	mu        sync.Mutex
	record    BeaconRecord
	rssiSum   float64
	prevRssi  int16 // RSSI of the sample before the current one (for trend)
	rawFields []string
}

// BeaconStore keeps de-duplicated scan results and applies filters.
type BeaconStore struct {
	mu      sync.RWMutex
	entries map[string]*beaconEntry
	dropped int64
}

func NewBeaconStore() *BeaconStore {
	return &BeaconStore{entries: make(map[string]*beaconEntry)}
}

func (s *BeaconStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string]*beaconEntry)
	s.dropped = 0
}

func (s *BeaconStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// ingest processes one scan result into the store.
func (s *BeaconStore) ingest(addr bluetooth.Address, rssi int16, payload bluetooth.AdvertisementPayload) {
	key := addrKey(addr)

	// Bytes() returns the true on-air AD structure stream when the platform
	// exposes it (Linux BlueZ / Windows WinRT). On macOS CoreBluetooth only
	// hands us *parsed* fields and Bytes() returns nil — in that case we
	// reconstruct the complete advertisement image (ADV_IND + Scan Response
	// merged into one TLV stream) from every field the OS provides: Flags,
	// names, service UUID lists, Tx Power, appearance, service data and
	// manufacturer data. This reconstruction is what feeds the parser below,
	// so parse results and the Raw hex always describe the same bytes.
	raw := payload.Bytes()
	name := payload.LocalName()
	uuids := payload.ServiceUUIDs()
	mfg := payload.ManufacturerData()
	svcData := payload.ServiceData()
	flags, shortName, advTxPower, appearance := macosExtraFields(payload)

	rawRebuilt := false
	if len(raw) == 0 {
		raw = rebuildRawAD(name, uuids, mfg, svcData, flags, shortName, advTxPower, appearance)
		rawRebuilt = true
	} else {
		// Bytes() is non-nil (Linux/Windows full on-air stream — or macOS with a
		// platform that exposes raw bytes). Merge any AD structure the bridge
		// captured but the stream lacks (Tx Power 0x0A, Flags 0x01, Shortened
		// Name 0x08, Appearance 0x24) so Raw and the parse tables always
		// describe the same complete set.
		if advTxPower != nil && findADFlag(raw, 0x0A) == nil {
			raw = appendAD(raw, 0x0A, []byte{byte(*advTxPower)})
			rawRebuilt = true
		}
		if flags != nil && findADFlag(raw, 0x01) == nil {
			raw = appendAD(raw, 0x01, []byte{*flags})
			rawRebuilt = true
		}
		if shortName != "" && findADFlag(raw, 0x08) == nil {
			raw = appendAD(raw, 0x08, []byte(shortName))
			rawRebuilt = true
		}
		if appearance != nil && findADFlag(raw, 0x24) == nil {
			raw = appendAD(raw, 0x24, []byte{byte(*appearance), byte(*appearance >> 8)})
			rawRebuilt = true
		}
	}

	txPower := parseTxPower(raw)
	appearanceVal := parseAppearance(raw)
	if appearanceVal == nil && appearance != nil {
		appearanceVal = appearance // Darwin bridge captured it directly
	}
	// On macOS the raw on-air bytes are unavailable, so parseTxPower(raw) can
	// only see a value if we reconstructed the 0x0A AD structure ourselves.
	// The Darwin bridge captures CBAdvertisementDataTxPowerLevelKey directly
	// from CoreBluetooth — prefer that authoritative value whenever the raw
	// stream didn't yield one (and merge it into the record below).
	if txPower == nil && advTxPower != nil {
		txPower = advTxPower
	}
	beaconType := detectBeaconType(mfg, svcData)
	advTypes := advTypesFromRaw(raw)
	deviceType := deviceTypeFromFlags(raw)
	parsed := parseAdvertisement(raw)
	dec := decodeBeaconFields(mfg, svcData) // iBeacon/AltBeacon/Eddystone/Google specifics

	rec := BeaconRecord{
		Address:      key,
		IsRandom:     addr.IsRandom(),
		Name:         name,
		RSSI:         rssi,
		MaxRSSI:      rssi,
		MinRSSI:      rssi,
		AvgRSSI:      float64(rssi),
		TxPower:      txPower,
		DeviceType:   deviceType,
		FirstSeen:    time.Now(),
		LastSeen:     time.Now(),
		Count:        1,
		ServiceUUIDs: nil,
		CompanyIDs:   nil,
		AdvTypes:     advTypes,
		Manufacturer: map[string]string{},
		ServiceData:  map[string]string{},
		Raw:          strings.ToUpper(hex.EncodeToString(raw)),
		RawRebuilt:   rawRebuilt,
		BeaconType:   beaconType,
		Parsed:       parsed,
	}
	rec.IBeaconUUID = dec.iBeaconUUID
	rec.IBeaconMajor = dec.iBeaconMajor
	rec.IBeaconMinor = dec.iBeaconMinor
	rec.EdNamespace = dec.edNamespace
	rec.EdInstance = dec.edInstance
	rec.EdURL = dec.edURL
	rec.GoogleBeaconID = dec.googleID
	if appearanceVal != nil {
		rec.Appearance = appearanceVal
		rec.AppearanceName = decodeAppearance(*appearanceVal)
	}
	for _, u := range uuids {
		rec.ServiceUUIDs = append(rec.ServiceUUIDs, u.String())
	}
	for _, m := range mfg {
		rec.CompanyIDs = append(rec.CompanyIDs, m.CompanyID)
		rec.Manufacturer[fmt.Sprintf("0x%04X", m.CompanyID)] = strings.ToUpper(hex.EncodeToString(m.Data))
	}
	for _, sd := range svcData {
		rec.ServiceData[sd.UUID.String()] = strings.ToUpper(hex.EncodeToString(sd.Data))
	}

	// Connectability per Core Spec: the Flags AD type (0x01) advertises
	// "LE General Discoverable" (bit 1) / "Simultaneous LE+BR/EDR" modes.
	// BLE beacons that omit Flags are effectively non-connectable adverts.
	if fl := findADFlag(raw, 0x01); fl != nil {
		rec.ConnectableKnown = true
		rec.Connectable = *fl&0x06 != 0 // bits 1-2: general/simultaneous discoverable
	} else {
		rec.ConnectableKnown = true // absence is itself informative
	}

	s.mu.Lock()
	e, ok := s.entries[key]
	if !ok {
		// Cap the table so a long-running scan cannot exhaust memory.
		if len(s.entries) >= maxBeacons {
			s.dropped++
			s.mu.Unlock()
			return
		}
		e = &beaconEntry{record: rec, rssiSum: float64(rssi)}
		s.entries[key] = e
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	r := &e.record
	if name != "" {
		r.Name = name
	}
	if txPower != nil {
		r.TxPower = txPower
	}
	if appearanceVal != nil {
		r.Appearance = appearanceVal
		r.AppearanceName = decodeAppearance(*appearanceVal)
	}
	if deviceType != "" {
		r.DeviceType = deviceType
	}
	if beaconType != "" && beaconType != "Unknown" {
		r.BeaconType = beaconType
	}
	if len(rec.ServiceUUIDs) > 0 {
		r.ServiceUUIDs = rec.ServiceUUIDs
	}
	if len(rec.CompanyIDs) > 0 {
		r.CompanyIDs = rec.CompanyIDs
	}
	if len(rec.Manufacturer) > 0 {
		r.Manufacturer = rec.Manufacturer
	}
	if len(rec.ServiceData) > 0 {
		r.ServiceData = rec.ServiceData
	}
	if len(raw) > 0 {
		r.Raw = rec.Raw
		r.RawRebuilt = rawRebuilt
	}
	if len(advTypes) > 0 {
		r.AdvTypes = advTypes
	}
	r.Parsed = parsed
	if dec.iBeaconUUID != "" {
		r.IBeaconUUID, r.IBeaconMajor, r.IBeaconMinor = dec.iBeaconUUID, dec.iBeaconMajor, dec.iBeaconMinor
	}
	if dec.edNamespace != "" {
		r.EdNamespace, r.EdInstance = dec.edNamespace, dec.edInstance
	}
	if dec.edURL != "" {
		r.EdURL = dec.edURL
	}
	if dec.googleID != "" {
		r.GoogleBeaconID = dec.googleID
	}
	e.prevRssi = r.RSSI
	r.RSSI = rssi
	switch {
	case rssi > e.prevRssi:
		r.RssiTrend = "up"
	case rssi < e.prevRssi:
		r.RssiTrend = "down"
	default:
		r.RssiTrend = "flat"
	}
	if rssi > r.MaxRSSI {
		r.MaxRSSI = rssi
	}
	if rssi < r.MinRSSI {
		r.MinRSSI = rssi
	}
	r.Count++
	e.rssiSum += float64(rssi)
	r.AvgRSSI = e.rssiSum / float64(r.Count)
	r.LastSeen = time.Now()
}

const maxBeacons = 5000

func addrKey(addr bluetooth.Address) string {
	// Address embeds MACAddress on Linux/Windows (a [6]byte MAC) and a UUID
	// (4x uint32) on macOS. Handle both generically via reflection-free
	// formatting of the underlying arrays.
	v := reflect.ValueOf(addr)
	if v.Kind() == reflect.Struct && v.NumField() > 0 {
		inner := v.Field(0) // MACAddress or UUID
		if inner.Kind() == reflect.Struct && inner.NumField() > 0 {
			leaf := inner.Field(0)
			switch leaf.Kind() {
			case reflect.Array: // [6]byte MAC
				if leaf.Len() == 6 && leaf.Type().Elem().Kind() == reflect.Uint8 {
					b := make([]byte, 6)
					for i := 0; i < 6; i++ {
						b[i] = byte(leaf.Index(i).Uint())
					}
					// Print in over-the-air order (little-endian stored).
					return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
						b[5], b[4], b[3], b[2], b[1], b[0])
				}
			case reflect.Uint32: // UUID ([4]uint32)
				if leaf.Len() == 4 {
					var sb strings.Builder
					for i := 0; i < 4; i++ {
						fmt.Fprintf(&sb, "%08x", leaf.Index(i).Uint())
					}
					s := strings.ToUpper(sb.String())
					if len(s) == 32 {
						return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
					}
					return s
				}
			}
		}
	}
	s := fmt.Sprintf("%v", addr)
	if s == "" || s == "{}" {
		s = "UNKNOWN"
	}
	return strings.ToUpper(s)
}

// snapshot returns all records matching the filter.
func (s *BeaconStore) snapshot(f FilterOptions) []BeaconRecord {
	now := time.Now()
	out := make([]BeaconRecord, 0, 64)

	s.mu.RLock()
	entries := make([]*beaconEntry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}
	s.mu.RUnlock()

	for _, e := range entries {
		e.mu.Lock()
		r := e.record
		e.mu.Unlock()

		if matches(&r, f, now) {
			out = append(out, r)
		}
	}
	return out
}

// snapshotFilteredSorted returns the filtered and sorted list of records.
func (s *BeaconStore) snapshotFilteredSorted(f FilterOptions, sortBy, dir string) []BeaconRecord {
	records := s.snapshot(f)
	sortRecords(records, sortBy, dir)
	return records
}

func matches(r *BeaconRecord, f FilterOptions, now time.Time) bool {
	if f.MinRSSI > -200 && int(r.RSSI) < f.MinRSSI {
		return false
	}
	// Ceiling disabled when unset (0) or set to the sentinel 200.
	if f.MaxRSSI != 0 && f.MaxRSSI < 200 && int(r.RSSI) > f.MaxRSSI {
		return false
	}
	if f.HasName && r.Name == "" {
		return false
	}
	if f.MinHits > 0 && r.Count < f.MinHits {
		return false
	}
	switch strings.ToLower(f.DeviceType) {
	case "le":
		if !strings.HasPrefix(r.DeviceType, "LE Only") {
			return false
		}
	case "dual":
		if r.DeviceType != "Classic & LE" {
			return false
		}
	case "noflags":
		if r.DeviceType != "" {
			return false
		}
	}
	switch strings.ToLower(f.Connectable) {
	case "yes":
		if !recordConnectable(r) {
			return false
		}
	case "no":
		if recordConnectable(r) {
			return false
		}
	}
	switch strings.ToLower(f.AddressRandom) {
	case "yes":
		if !r.IsRandom {
			return false
		}
	case "no":
		if r.IsRandom {
			return false
		}
	}
	if f.MinTxPower != 0 || f.MaxTxPower != 0 {
		if r.TxPower == nil {
			return false
		}
		tp := int(*r.TxPower)
		if f.MinTxPower != 0 && tp < f.MinTxPower {
			return false
		}
		if f.MaxTxPower != 0 && tp > f.MaxTxPower {
			return false
		}
	}
	if len(f.AdTypes) > 0 {
		for _, want := range f.AdTypes {
			found := false
			for _, s := range r.Parsed.Structures {
				if int(s.Type) == want {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	if f.MajorExact > 0 && r.IBeaconMajor != f.MajorExact {
		return false
	}
	if f.MinorExact > 0 && r.IBeaconMinor != f.MinorExact {
		return false
	}
	if f.EddystoneURLContains != "" {
		if !strings.Contains(strings.ToLower(r.EdURL), strings.ToLower(f.EddystoneURLContains)) {
			return false
		}
	}
	if f.NamespaceHex != "" {
		needle := strings.ToUpper(strings.ReplaceAll(f.NamespaceHex, " ", ""))
		if !strings.HasPrefix(strings.ToUpper(r.EdNamespace), needle) {
			return false
		}
	}
	if f.InstanceHex != "" {
		needle := strings.ToUpper(strings.ReplaceAll(f.InstanceHex, " ", ""))
		if !strings.HasPrefix(strings.ToUpper(r.EdInstance), needle) {
			return false
		}
	}
	if f.NameContains != "" && !strings.Contains(strings.ToLower(r.Name), strings.ToLower(f.NameContains)) {
		return false
	}
	if f.NameExact != "" && !strings.EqualFold(r.Name, f.NameExact) {
		return false
	}
	if f.MacContains != "" && !strings.Contains(strings.ToLower(r.Address), strings.ToLower(f.MacContains)) {
		return false
	}
	if f.UUIDContains != "" {
		needle := strings.ToLower(strings.ReplaceAll(f.UUIDContains, "-", ""))
		found := false
		for _, u := range r.ServiceUUIDs {
			cand := strings.ToLower(strings.ReplaceAll(u, "-", ""))
			if strings.Contains(cand, needle) {
				found = true
				break
			}
		}
		// Also match iBeacon UUID and Eddystone namespace/instance hex.
		if !found && strings.Contains(strings.ToLower(strings.ReplaceAll(r.IBeaconUUID, "-", "")), needle) {
			found = true
		}
		if !found && strings.Contains(strings.ToUpper(r.EdNamespace+r.EdInstance), strings.ToUpper(needle)) {
			found = true
		}
		if !found {
			return false
		}
	}
	if f.CompanyID != "" {
		// Normalize the user input: strip 0x/x prefixes, spaces and underscores,
		// left-pad to 4 hex digits (so "4c" == "0x004C"), lowercase.
		needle := strings.ToLower(strings.TrimSpace(f.CompanyID))
		needle = strings.TrimPrefix(needle, "0x")
		needle = strings.TrimPrefix(needle, "x")
		needle = strings.ReplaceAll(needle, " ", "")
		needle = strings.ReplaceAll(needle, "_", "")
		if len(needle) > 0 && len(needle) < 4 {
			needle = strings.Repeat("0", 4-len(needle)) + needle
		}
		found := false
		// Preferred source: parsed company IDs as uint16 (exact match).
		for _, cid := range r.CompanyIDs {
			if fmt.Sprintf("%04x", cid) == needle {
				found = true
				break
			}
		}
		// Fallback: manufacturer data map keys ("0x004C" style), case-insensitive.
		if !found {
			for cid := range r.Manufacturer {
				hexPart := strings.ToLower(strings.TrimPrefix(cid, "0x"))
				if len(hexPart) > 0 && len(hexPart) < 4 {
					hexPart = strings.Repeat("0", 4-len(hexPart)) + hexPart
				}
				if hexPart == needle {
					found = true
					break
				}
			}
		}
		if !found {
			return false
		}
	}
	if f.BeaconType != "" && !strings.EqualFold(f.BeaconType, "any") {
		switch strings.ToLower(f.BeaconType) {
		case "named":
			if r.Name == "" {
				return false
			}
		case "unnamed":
			if r.Name != "" {
				return false
			}
		case "unknown":
			if r.BeaconType != "Unknown" {
				return false
			}
		default:
			want := strings.ToLower(f.BeaconType)
			// "google" matches any Eddystone frame (Eddystone is Google's protocol).
			if want == "google" {
				if !strings.HasPrefix(strings.ToLower(r.BeaconType), "eddystone") {
					return false
				}
				break
			}
			// Normalise both sides so "ibeacon" matches "iBeacon", and
			// "altbeacon" matches labels like "AltBeacon (0x0118)".
			norm := strings.NewReplacer(" ", "", "-", "")
			fam := norm.Replace(strings.ToLower(r.BeaconType))
			w := norm.Replace(want)
			if !strings.HasPrefix(fam, w) {
				return false
			}
		}
	}
	if f.RawContains != "" {
		needle := strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(f.RawContains))
		if !strings.Contains(strings.ToUpper(r.Raw), needle) {
			return false
		}
	}
	if f.ServiceUUIDExact != "" {
		want := strings.ToLower(strings.NewReplacer("-", "", "0x", "").Replace(f.ServiceUUIDExact))
		found := false
		for _, u := range r.ServiceUUIDs {
			cand := strings.ToLower(strings.ReplaceAll(u, "-", ""))
			if cand == want || (want != "" && strings.TrimLeft(cand, "0") == strings.TrimLeft(want, "0")) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if f.NamePrefix != "" && !strings.HasPrefix(strings.ToLower(r.Name), strings.ToLower(f.NamePrefix)) {
		return false
	}
	if f.NameSuffix != "" && !strings.HasSuffix(strings.ToLower(r.Name), strings.ToLower(f.NameSuffix)) {
		return false
	}
	if f.RssiTrend != "" && !strings.EqualFold(f.RssiTrend, "any") {
		if !strings.EqualFold(r.RssiTrend, f.RssiTrend) {
			return false
		}
	}
	// GAP Appearance filters (embedded bluetooth-numbers-database).
	// Exact: hex code like "0x0080" or "80" — matches the advertised value.
	if f.AppearanceExact != "" {
		needle := strings.ToLower(strings.TrimSpace(f.AppearanceExact))
		needle = strings.TrimPrefix(needle, "0x")
		needle = strings.ReplaceAll(needle, " ", "")
		if needle != "" {
			if r.Appearance == nil {
				return false
			}
			want := fmt.Sprintf("%04x", *r.Appearance)
			if len(needle) < 4 {
				needle = strings.Repeat("0", 4-len(needle)) + needle
			}
			if want != needle {
				return false
			}
		}
	}
	// Category: SIG category name substring, e.g. "phone" matches any phone
	// subcategory (iPhone, Android…). Case-insensitive.
	if f.AppearanceCategory != "" {
		if r.Appearance == nil {
			return false
		}
		cat := strings.ToLower(appearanceCategory(*r.Appearance))
		if cat == "" || !strings.Contains(cat, strings.ToLower(strings.TrimSpace(f.AppearanceCategory))) {
			return false
		}
	}
	// "Only active" window. ActiveWindow <= 0 falls back to a sane 10 s
	// instead of silently dropping every record (a zero-value filter must
	// never mean "show nothing").
	if f.ActiveOnly {
		win := f.ActiveWindow
		if win <= 0 {
			win = 10
		}
		if now.Sub(r.LastSeen) > time.Duration(win)*time.Second {
			return false
		}
	}
	return true
}

// sortRecords sorts by the given key ("rssi", "name", "lastseen", "firstseen",
// "count", "address") with the given direction ("asc"/"desc").
func sortRecords(records []BeaconRecord, sortBy, dir string) {
	less := func(a, b *BeaconRecord) bool {
		switch sortBy {
		case "name":
			na, nb := strings.ToLower(a.Name), strings.ToLower(b.Name)
			if na == "" {
				return false
			}
			if nb == "" {
				return true
			}
			return na < nb
		case "avgrssi":
			return a.AvgRSSI < b.AvgRSSI
		case "lastseen":
			return a.LastSeen.Before(b.LastSeen)
		case "firstseen":
			return a.FirstSeen.Before(b.FirstSeen)
		case "count":
			return a.Count < b.Count
		case "address":
			return a.Address < b.Address
		case "beacontype":
			return a.BeaconType < b.BeaconType
		case "rssi":
			fallthrough
		default:
			return a.RSSI < b.RSSI
		}
	}
	sort.SliceStable(records, func(i, j int) bool {
		if dir == "asc" {
			return less(&records[i], &records[j])
		}
		return less(&records[j], &records[i])
	})
}

// ---------- advertisement parsing helpers ----------

// beaconDecoded holds the structured, filterable fields extracted from a payload.
type beaconDecoded struct {
	iBeaconUUID  string
	iBeaconMajor int
	iBeaconMinor int
	edNamespace  string
	edInstance   string
	edURL        string
	googleID     string
}

// decodeBeaconFields extracts iBeacon / AltBeacon / Eddystone / Google specifics
// from manufacturer & service data elements so they can be displayed and filtered.
func decodeBeaconFields(mfg []bluetooth.ManufacturerDataElement, svc []bluetooth.ServiceDataElement) beaconDecoded {
	var d beaconDecoded
	for _, m := range mfg {
		switch {
		case m.CompanyID == 0x004C && len(m.Data) >= 23 && m.Data[0] == 0x02 && m.Data[1] == 0x15:
			h := strings.ToUpper(hex.EncodeToString(m.Data[2:18]))
			d.iBeaconUUID = h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
			d.iBeaconMajor = int(m.Data[18])<<8 | int(m.Data[19])
			d.iBeaconMinor = int(m.Data[20])<<8 | int(m.Data[21])
		case m.CompanyID == 0x0118 && len(m.Data) >= 23 && m.Data[0] == 0xBE && m.Data[1] == 0xAC:
			h := strings.ToUpper(hex.EncodeToString(m.Data[2:18]))
			d.iBeaconUUID = h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
			d.iBeaconMajor = int(m.Data[18])<<8 | int(m.Data[19])
			d.iBeaconMinor = int(m.Data[20])<<8 | int(m.Data[21])
		case m.CompanyID == 0x00E0 && len(m.Data) >= 7 && m.Data[0] == 0x02:
			d.googleID = strings.ToUpper(hex.EncodeToString(m.Data[1:7]))
		}
	}
	for _, s := range svc {
		u := strings.ToUpper(s.UUID.String())
		if !strings.Contains(u, "AAFE") && !strings.Contains(u, "AA60") {
			continue
		}
		b := s.Data
		if len(b) < 1 {
			continue
		}
		switch b[0] {
		case 0x00: // UID frame
			if len(b) >= 18 {
				d.edNamespace = strings.ToUpper(hex.EncodeToString(b[1:11]))
				d.edInstance = strings.ToUpper(hex.EncodeToString(b[11:17]))
			}
		case 0x10: // URL frame
			d.edURL = decodeEddystoneURLPayload(b)
		}
	}
	return d
}

// recordConnectable derives connectability from the Flags AD structure
// (Core Spec Vol 3, Part C, 2.3.1 "Flags" / LE General Discoverable Mode bit).
// Devices without a Flags AD default to connectable (most adverts are).
func recordConnectable(r *BeaconRecord) bool {
	for _, s := range r.Parsed.Structures {
		if s.Type == 0x01 && len(s.Bytes) >= 1 {
			// Bit 0 = LE Extended Mode, Bit 1 = BR/EDR Not Supported.
			// Connectable advertising is signalled by the PDU type which is
			// not exposed here; the Flags byte's discoverable bits are the
			// best available hint in a passive scan.
			return s.Bytes[0]&0x06 != 0 || s.Bytes[0]&0x01 == 0
		}
	}
	return true
}

// detectBeaconType inspects manufacturer/service data for common beacon formats.
func detectBeaconType(mfg []bluetooth.ManufacturerDataElement, svc []bluetooth.ServiceDataElement) string {
	for _, m := range mfg {
		if m.CompanyID == 0x004C && len(m.Data) >= 23 && m.Data[0] == 0x02 && m.Data[1] == 0x15 {
			return "iBeacon"
		}
		if m.CompanyID == 0x0118 && len(m.Data) >= 2 && m.Data[0] == 0xBE && m.Data[1] == 0xAC {
			return "AltBeacon"
		}
	}
	for _, s := range svc {
		u := strings.ToUpper(s.UUID.String())
		if strings.Contains(u, "AA60") { // 0xFEAA Eddystone-TDV
			if len(s.Data) >= 1 {
				switch s.Data[0] {
				case 0x00:
					return "Eddystone-UID"
				case 0x10:
					return "Eddystone-URL"
				case 0x20:
					return "Eddystone-TLM"
				}
			}
			return "Eddystone"
		}
	}
	return "Unknown"
}

// findADFlag returns the data byte of the first AD structure with the given
// type in the raw advertisement stream, or nil when absent. Standard TLV walk:
// [length][type][data...] repeated.
func findADFlag(raw []byte, adType byte) *byte {
	i := 0
	for i+1 < len(raw) {
		length := int(raw[i])
		if length == 0 || i+1+length > len(raw) {
			break
		}
		typ := raw[i+1]
		if typ == adType && length >= 2 {
			v := raw[i+2]
			return &v
		}
		i += 1 + length
	}
	return nil
}

// deviceTypeFromFlags decodes the AD Flags structure (type 0x01) into a human
// readable device type per Core Spec Supplement:
//   - LE General/Simultaneous Discoverable bit set together with BR/EDR Not
//     Supported → "LE Only"; without it → "Classic & LE".
//   - No Flags AD present → "" (unknown; many beacons omit Flags).
func deviceTypeFromFlags(raw []byte) string {
	fl := findADFlag(raw, 0x01)
	if fl == nil {
		return ""
	}
	discoverable := *fl&0x06 != 0 // bits 1-2: general / simultaneous discoverable
	brEdrNotSupported := *fl&0x04 != 0
	switch {
	case brEdrNotSupported && discoverable:
		return "LE Only"
	case discoverable:
		return "Classic & LE"
	case brEdrNotSupported:
		return "LE Only (non-discoverable)"
	default:
		return "Undirected"
	}
}

// parseTxPower extracts the Tx Power AD structure (type 0x0A) from raw bytes.
func parseTxPower(raw []byte) *int8 {
	i := 0
	for i+1 < len(raw) {
		length := int(raw[i])
		if length == 0 || i+1+length > len(raw) {
			break
		}
		adType := raw[i+1]
		if adType == 0x0A && length >= 2 {
			v := int8(raw[i+2])
			return &v
		}
		i += 1 + length
	}
	return nil
}

// parseAppearance scans the raw AD stream for the GAP Appearance structure
// (AD type 0x24, 16-bit little-endian per Core Spec). Returns nil when absent.
func parseAppearance(raw []byte) *uint16 {
	i := 0
	for i+1 < len(raw) {
		length := int(raw[i])
		if length == 0 || i+1+length > len(raw) {
			break
		}
		adType := raw[i+1]
		if adType == 0x24 && length >= 3 {
			v := uint16(raw[i+2]) | uint16(raw[i+3])<<8
			return &v
		}
		i += 1 + length
	}
	return nil
}

// ---------- raw AD reconstruction (macOS fallback) ----------

// baseUUIDSuffix is the tail of the Bluetooth Base UUID
// (0000xxxx-0000-1000-8000-00805F9B34FB). A UUID matching this pattern can be
// encoded on air as a compact 16-bit UUID.
const baseUUIDSuffix = "1000800000805f9b34fb"

// isShortUUID reports whether a 128-bit UUID is a Base-UUID (16-bit) form.
func isShortUUID(u bluetooth.UUID) (uint16, bool) {
	s := strings.ReplaceAll(strings.ToLower(u.String()), "-", "")
	if len(s) == 32 && strings.HasPrefix(s, "0000") && strings.HasSuffix(s, baseUUIDSuffix) {
		v, err := strconv.ParseUint(s[4:8], 16, 16)
		return uint16(v), err == nil
	}
	return 0, false
}

// uuidBytes returns the canonical big-endian 16-byte form of a UUID by
// parsing its standard hyphenated string representation.
func uuidBytes(u bluetooth.UUID) [16]byte {
	var b [16]byte
	s := strings.ReplaceAll(u.String(), "-", "")
	for i := 0; i < 16 && i*2+1 < len(s); i++ {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err == nil {
			b[i] = byte(v)
		}
	}
	return b
}

// appendAD writes one Type-Length-Value AD structure into buf.
func appendAD(buf []byte, adType byte, data []byte) []byte {
	buf = append(buf, byte(len(data)+1), adType)
	return append(buf, data...)
}

// appendUUIDs encodes a list of UUIDs into the given 16-bit or 128-bit AD type.
func appendUUIDs(buf []byte, uuids []bluetooth.UUID, shortType, longType byte) []byte {
	var s16, s128 []byte
	for _, u := range uuids {
		if v, ok := isShortUUID(u); ok {
			s16 = append(s16, byte(v), byte(v>>8)) // little-endian on air
		} else {
			b := uuidBytes(u)
			for i := 15; i >= 0; i-- { // reversed on air
				s128 = append(s128, b[i])
			}
		}
	}
	if len(s16) > 0 {
		buf = appendAD(buf, shortType, s16)
	}
	if len(s128) > 0 {
		buf = appendAD(buf, longType, s128)
	}
	return buf
}

// rebuildRawAD reconstructs a spec-conformant AD-structure stream from parsed
// advertisement fields. Used when the platform does not expose the original
// on-air bytes (CoreBluetooth on macOS returns nil from Bytes()). The result
// carries EVERY field the OS hands us — Flags, short/complete names, service
// UUID lists (incomplete + complete forms), Tx Power Level, Appearance and
// manufacturer/service data — re-encoded with Core Spec v5.x LE rules
// (little-endian company IDs / appearance, reversed 128-bit UUIDs). This is
// the full advertisement image shown as "Raw": the parser below reads exactly
// these bytes, so Raw and the parsed tables can never disagree.
func rebuildRawAD(name string, uuids []bluetooth.UUID, mfg []bluetooth.ManufacturerDataElement, svc []bluetooth.ServiceDataElement, flags *byte, shortName string, advTxPower *int8, appearance *uint16) []byte {
	buf := make([]byte, 0, 31)
	if flags != nil {
		buf = appendAD(buf, 0x01, []byte{*flags}) // Flags
	}
	if shortName != "" && shortName != name {
		buf = appendAD(buf, 0x08, []byte(shortName)) // Shortened Local Name
	}
	if name != "" {
		n := []byte(name)
		if len(n)+2 <= 31-len(buf) {
			buf = appendAD(buf, 0x09, n) // Complete Local Name
		} else if len(buf) < 30 {
			// No room for the full name: emit a truncated Shortened form.
			buf = appendAD(buf, 0x08, n[:30-len(buf)])
		}
	}
	if appearance != nil {
		buf = appendAD(buf, 0x24, []byte{byte(*appearance), byte(*appearance >> 8)}) // Appearance, little-endian
	}
	if advTxPower != nil {
		buf = appendAD(buf, 0x0A, []byte{byte(*advTxPower)}) // Tx Power Level
	}
	// On-air split of incomplete/complete lists is unknowable after parsing;
	// advertise everything in the "complete" forms (0x02/0x03) per spec §2.3.
	buf = appendUUIDs(buf, uuids, 0x02, 0x03) // 16-bit complete / 128-bit complete
	for _, m := range mfg {
		d := append([]byte{byte(m.CompanyID), byte(m.CompanyID >> 8)}, m.Data...)
		buf = appendAD(buf, 0xFF, d) // Manufacturer Specific Data
	}
	for _, sd := range svc {
		if v, ok := isShortUUID(sd.UUID); ok {
			d := append([]byte{byte(v), byte(v >> 8)}, sd.Data...)
			buf = appendAD(buf, 0x16, d) // Service Data - 16-bit UUID
		} else {
			b := uuidBytes(sd.UUID)
			d := make([]byte, 0, 16+len(sd.Data))
			for i := 15; i >= 0; i-- {
				d = append(d, b[i])
			}
			d = append(d, sd.Data...)
			buf = appendAD(buf, 0x21, d) // Service Data - 128-bit UUID
		}
	}
	return buf
}
