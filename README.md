# 📡 Beacon Scout

A desktop BLE **beacon scanner** for macOS and Windows, built with [Wails v3](https://wails.io) (Go backend + web frontend) and [`tinygo-org/bluetooth`](https://github.com/tinygo-org/bluetooth).

It **passively listens** for Bluetooth Low Energy advertisement packets — it never connects, pairs, or touches the devices. Every captured packet is decoded according to the Bluetooth Core Specification and the SIG Assigned Numbers database, then shown in a live, sortable, filterable table.

🔗 Repository: <https://github.com/uretgec/beacon-scout>

## What it can do

- **Passive scanning** — captures ADV_IND / SCAN_RSP advertisements from any nearby BLE device.
- **Beacon decoding** — recognizes **iBeacon**, **Eddystone** (UID / URL / TLM), and **AltBeacon**, plus raw AD structures (Flags, Names, Service UUIDs, Service Data, Manufacturer Data, Tx Power, Appearance…).
- **Full detail view** — click any row: sectioned tables show identity, RSSI & timing, parsed beacon data, and the complete raw advertisement as a hex dump *and* a field-by-field AD-structure breakdown. Copy the parsed table or the full JSON record with one click.
- **Live filtering** — a collapsible sidebar with signal range (min/max RSSI), name/MAC/UUID matching, manufacturer picker (searchable, backed by the official SIG company list), GAP appearance picker (e.g. "phone", "watch"), advertised service UUIDs, beacon type, AD-type presence, connectability, address type, major/minor, Eddystone URL/Namespace and more. Filters re-apply automatically while you type.
- **Sorting** — click column headers (RSSI, name, last seen, hit count…) to sort ascending/descending.
- **Auto-refresh control** — choose how often the list updates (Off / 0.5 s / 1 s / 2 s / 5 s …). Start Scan turns on a 2 s refresh automatically; Stop Scan pauses it again.
- **Local only** — no network access, no telemetry; everything runs on your machine. The SIG numbers database is embedded in the binary.

## Quick start

```bash
# prerequisites: Go 1.24+, Node 18+, Wails v3 CLI  (see INSTALL.md)
go mod download
cd frontend && npm install && cd ..
wails3 generate bindings   # once per clone / after Go API changes
wails3 dev                 # development mode with hot reload
```

Production build: `wails3 build` (output under `build/bin/`).

### First run notes

- **macOS**: grant the Bluetooth permission when prompted (System Settings → Privacy & Security → Bluetooth).
- **Windows**: uses the native WinRT BLE stack; no extra drivers needed.
- Debugging: open DevTools with `Cmd+Opt+I` (macOS) / `F12` (Windows); every scan step logs `[scan] …` lines to the terminal.

## Project layout

| Path | Purpose |
|---|---|
| `main.go` | Wails app bootstrap, window, service registration |
| `scanservice.go` | BLE adapter lifecycle, scan loop, events, filter/sort APIs |
| `beaconstore.go` | de-duplicated device store, filtering, sorting |
| `adparser.go` | AD-structure (TLV) + beacon-format decoder |
| `numbersdb.go` + `data/numbers/` | embedded Bluetooth SIG Assigned Numbers lookup |
| `frontend/` | TypeScript UI (vanilla TS + Vite) |

Detailed setup, packaging and testing instructions: **[INSTALL.md](INSTALL.md)** · Change history: **[CHANGELOG.md](CHANGELOG.md)**

## Credits

- [Wails v3](https://wails.io) — desktop framework
- [tinygo-org/bluetooth](https://github.com/tinygo-org/bluetooth) — cross-platform BLE
- [NordicSemiconductor/bluetooth-numbers-database](https://github.com/NordicSemiconductor/bluetooth-numbers-database) — Bluetooth SIG assigned numbers (Apache-2.0), embedded via `//go:embed`

## License

MIT (application code). Bundled SIG data files remain Apache-2.0 — see `data/numbers/LICENSE`.
