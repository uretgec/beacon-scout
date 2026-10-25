package main

// Bluetooth SIG Assigned Numbers lookup, vendored from Nordic Semiconductor's
// bluetooth-numbers-database (https://github.com/NordicSemiconductor/bluetooth-numbers-database)
// and embedded into the binary with //go:embed — no runtime file dependency.
//
// Files kept (the four that matter for passive advertisement parsing):
//   data/numbers/company_ids.json        -> company code -> official name
//   data/numbers/gap_appearance.json     -> appearance code -> category/name/icon
//   data/numbers/service_uuids.json      -> 16-bit GATT service UUID -> name
//   data/numbers/characteristic_uuids.json -> 16-bit characteristic UUID -> name
// The *_schema.json files are CI validation only and descriptor_uuids.json is
// connection-oriented, so they are intentionally not shipped.
//
// License: Apache-2.0 (see data/numbers/LICENSE). Attribution preserved here.

import (
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed data/numbers/*.json
var numbersFS embed.FS

// Real v1 schemas (NordicSemiconductor/bluetooth-numbers-database):
//
//	company_ids.json:         [{ "code": 76, "name": "Apple, Inc." }]   (decimal codes)
//	service_uuids.json:       [{ "name": "...", "uuid": "180A" }]        (hex strings)
//	characteristic_uuids.json same shape as services.
//	gap_appearance.json:      [{ "category": 2, "name": "Computer", "subcategory":[{"value":3,"name":"Laptop"}]}]
//
// Appearance value = (category << 6) | subcategory-value, per Core Spec r5.x.
type ndbCompany struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}
type ndbUUIDEntry struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}
type ndbCategory struct {
	Category    int    `json:"category"`
	Name        string `json:"name"`
	Subcategory []struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	} `json:"subcategory"`
}
type ndbAppearance struct {
	Value    uint16
	Name     string
	Category string
}

type numbersDB struct {
	once           sync.Once
	companies      map[uint16]string
	services       map[uint16]string
	characteristic map[uint16]string
	appearances    map[uint16]ndbAppearance
	companyList    []map[string]any // sorted [{code,name}] for frontend pickers
	appearanceList []map[string]any
}

var ndb numbersDB

// load parses the embedded JSON once. Any failure degrades gracefully: the
// small hand-written fallback tables in adparser.go keep working.
func (db *numbersDB) load() {
	db.once.Do(func() {
		db.companies = map[uint16]string{}
		db.services = map[uint16]string{}
		db.characteristic = map[uint16]string{}
		db.appearances = map[uint16]ndbAppearance{}

		if b, err := numbersFS.ReadFile("data/numbers/company_ids.json"); err == nil {
			var list []ndbCompany
			if json.Unmarshal(b, &list) == nil {
				for _, c := range list {
					if c.Code >= 0 && c.Code <= 0xFFFF {
						db.companies[uint16(c.Code)] = c.Name
					}
				}
			}
		}
		loadUUIDs := func(path string, dst map[uint16]string) {
			b, err := numbersFS.ReadFile(path)
			if err != nil {
				return
			}
			var list []ndbUUIDEntry
			if json.Unmarshal(b, &list) != nil {
				return
			}
			for _, e := range list {
				u := strings.ReplaceAll(strings.ToLower(e.UUID), "0x", "")
				if len(u) == 4 { // 16-bit form only
					if v, err := strconv.ParseUint(u, 16, 16); err == nil {
						dst[uint16(v)] = e.Name
					}
				}
			}
		}
		loadUUIDs("data/numbers/service_uuids.json", db.services)
		loadUUIDs("data/numbers/characteristic_uuids.json", db.characteristic)

		if b, err := numbersFS.ReadFile("data/numbers/gap_appearance.json"); err == nil {
			var cats []ndbCategory
			if json.Unmarshal(b, &cats) == nil {
				for _, cat := range cats {
					base := uint16(cat.Category&0x03FF) << 6
					// The category itself is a valid appearance (sub-field 0).
					db.appearances[base] = ndbAppearance{Value: base, Name: cat.Name, Category: cat.Name}
					for _, sub := range cat.Subcategory {
						v := base | uint16(sub.Value&0x3F)
						db.appearances[v] = ndbAppearance{Value: v, Name: sub.Name, Category: cat.Name}
					}
				}
			}
		}

		// Pre-build sorted picker lists (only once; ~1700 companies).
		for code, name := range db.companies {
			db.companyList = append(db.companyList, map[string]any{
				"code": fmt.Sprintf("0x%04X", code), "name": name,
			})
		}
		sort.Slice(db.companyList, func(i, j int) bool {
			a, b := db.companyList[i], db.companyList[j]
			if a["name"] != b["name"] {
				return a["name"].(string) < b["name"].(string)
			}
			return a["code"].(string) < b["code"].(string)
		})
		for _, a := range db.appearances {
			db.appearanceList = append(db.appearanceList, map[string]any{
				"value": int(a.Value), "hex": fmt.Sprintf("0x%04X", a.Value),
				"name": a.Name, "category": a.Category,
			})
		}
		sort.Slice(db.appearanceList, func(i, j int) bool {
			return db.appearanceList[i]["value"].(int) < db.appearanceList[j]["value"].(int)
		})

		log.Printf("[numbersdb] loaded: %d companies, %d services, %d characteristics, %d appearances",
			len(db.companies), len(db.services), len(db.characteristic), len(db.appearances))
	})
}

// companyName resolves a SIG company ID; "" when unknown (caller falls back).
func companyName(cid uint16) string {
	ndb.load()
	return ndb.companies[cid]
}

// CharacteristicNames returns the SIG names of all 16-bit GATT characteristic
// UUIDs from the embedded database, sorted by name. Bound for the frontend so
// future panels (Service Data decoding etc.) can resolve characteristic codes.
func (s *ScanService) CharacteristicNames() []map[string]any {
	ndb.load()
	out := make([]map[string]any, 0, len(ndb.characteristic))
	for code, name := range ndb.characteristic {
		out = append(out, map[string]any{
			"code": fmt.Sprintf("0x%04X", code), "name": name,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
	return out
}

// decodeAppearance renders an appearance value like "Generic Phone (cat: Phone)".
// gap_appearance.json stores decimal values per Core Spec r5.x convention.
func decodeAppearance(v uint16) string {
	ndb.load()
	if a, ok := ndb.appearances[v]; ok {
		if a.Category != "" {
			return fmt.Sprintf("%s (%s)", a.Name, a.Category)
		}
		return a.Name
	}
	return fmt.Sprintf("unknown (0x%04X)", v)
}

// appearanceCategory returns just the SIG category name of an appearance value
// (e.g. 0x0080 -> "Phone"), "" when the code is unknown. Used by the
// AppearanceCategory filter.
func appearanceCategory(v uint16) string {
	ndb.load()
	return ndb.appearances[v].Category
}

// Companies returns the full SIG company table for the filter sidebar picker.
func (s *ScanService) Companies() []map[string]any {
	ndb.load()
	return ndb.companyList
}

// Appearances returns the GAP appearance catalogue for the filter sidebar.
func (s *ScanService) Appearances() []map[string]any {
	ndb.load()
	return ndb.appearanceList
}

// CompanyName resolves a single company id (e.g. 0x4C -> "Apple, Inc.") using
// the embedded SIG table. Returns "" when unknown so the frontend can fall back.
func (s *ScanService) CompanyName(code int) string {
	ndb.load()
	return ndb.companies[uint16(code)]
}

// ServiceNames returns the SIG names of all advertised 16-bit GATT service
// UUIDs, for populating the service-UUID picker in the filters sidebar.
func (s *ScanService) ServiceNames() []map[string]any {
	ndb.load()
	out := make([]map[string]any, 0, len(ndb.services))
	for code, name := range ndb.services {
		out = append(out, map[string]any{
			"code": fmt.Sprintf("0x%04X", code), "name": name,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["name"].(string) < out[j]["name"].(string) })
	return out
}

// ServiceUuidNames is a bound ScanService method: it maps a record's advertised
// 16-bit service UUIDs to their SIG names (128-bit UUIDs pass through as-is).
// The frontend detail table calls this with rec.serviceUuids to render the
// "Service UUIDs" row as e.g. "0x180F Battery Service, 0x180A Heart Rate".
func (s *ScanService) ServiceUuidNames(uuids []string) string {
	return serviceNamesFor(uuids)
}

// serviceNamesFor maps the record's advertised 16-bit service UUIDs to their
// SIG names (128-bit UUIDs are returned unresolved).
func serviceNamesFor(uuids []string) string {
	ndb.load()
	var out []string
	for _, u := range uuids {
		h := strings.ReplaceAll(strings.ToLower(u), "0x", "")
		if len(h) == 4 {
			v, err := strconv.ParseUint(h, 16, 16)
			if err == nil {
				if n := ndb.services[uint16(v)]; n != "" {
					out = append(out, fmt.Sprintf("0x%04X %s", v, n))
					continue
				}
			}
		}
		out = append(out, strings.ToUpper(h))
	}
	return strings.Join(out, ", ")
}

// hexToUint16 helper used by tests/tools (kept tiny on purpose).
func hexToUint16(h string) (uint16, error) {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != 2 {
		return 0, fmt.Errorf("bad hex")
	}
	return uint16(b[0])<<8 | uint16(b[1]), nil
}
