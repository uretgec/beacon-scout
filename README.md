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

## Downloading pre-built binaries

GitHub Actions builds **unsigned** packages for macOS and Windows on every release
(workflow: `.github/workflows/release.yml` — runs the test suite first, then
publishes a `.dmg` and an NSIS installer to the GitHub Release page).
Grab them from [Releases](https://github.com/uretgec/beacon-scout/releases).

Because the builds are **not code-signed**, your OS will warn you on first launch.
This is normal for open-source community builds:

### macOS (`.dmg`)

Gatekeeper may say *"can't be opened because its integrity cannot be verified"* or
*"is damaged"*. Pick ONE of these:

1. **Right-click (Control-click) → Open** in Finder, then click **Open**. Once is enough.
2. If it says "damaged": run `xattr -cr "/Applications/Beacon Scout.app"` in Terminal, then launch again.
3. Or **System Settings → Privacy & Security → "Open Anyway"** at the bottom.

First scan also asks for Bluetooth permission — click **Allow**.

### Windows (`*-installer.exe` or `*-portable.zip`)

SmartScreen will say *"Windows protected your PC"*:

1. Click **"More info"** → **"Run anyway"**.
2. If the browser blocked the download: keep the file, then right-click →
   Properties → check **"Unblock"** → OK before running.

Two flavours are published:

- **Installer** — per-user NSIS (no admin/UAC needed). WebView2 runtime is required
  (preinstalled on Windows 11; bootstrapped on Windows 10 when online).
- **Portable ZIP** — no install at all: extract anywhere and double-click
  `beacon-scout.exe` (same SmartScreen step applies once). Uninstall = delete folder.

Each release asset ships with a `.sha256` checksum you can verify with
`shasum -a 256 <file>` (macOS/Linux) or `Get-FileHash <file>` (PowerShell).

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
