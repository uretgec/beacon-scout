# Beacon Scout — Installation & Testing Guide

This document explains how to install, build, run, and test **Beacon Scout**, a desktop app that passively scans for Bluetooth Low Energy (BLE) beacon advertisements on **macOS** and **Windows**.

---

## Generating Wails Bindings (required on a fresh clone)

The TypeScript bindings under `bindings/` are generated per machine and are **not** committed.
A small placeholder stub is committed under `frontend/bindings/` so the project builds out of the box,
but the stub throws until real bindings are generated:

```sh
wails3 generate bindings
```

Run this once after cloning (and again whenever you change Go service methods).
`wails3 dev` does this automatically.


## 1. Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| Go | 1.25 or newer | https://go.dev/dl/ |
| Node.js + npm | 18 or newer | https://nodejs.org/ |
| Wails CLI v3 | `v3.0.0-beta.29` or newer | Installed with Go (see below) |
| Task (optional) | any recent version | https://taskfile.dev — used by the project's `Taskfile.yml` |
| Hardware | BLE 4.0+ adapter | Built-in laptop Bluetooth is fine; VMs usually do **not** have Bluetooth access |

### Install the toolchain

**Go**

```bash
# macOS (Homebrew)
brew install go

# Windows (winget)
winget install GoLang.Go
```

**Wails v3 CLI**

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

Make sure `$GOPATH/bin` (macOS/Linux) or `%GOPATH%\bin` (Windows) is on your `PATH`. Verify:

```bash
wails3 doctor
```

**Node.js**

```bash
# macOS
brew install node

# Windows
winget install OpenJS.NodeJS.LTS
```

---

## 2. Install Project Dependencies

From the project root (the folder containing `go.mod`):

```bash
# Go backend dependencies (Wails v3, tinygo.org/x/bluetooth, ...)
go mod download

# Frontend dependencies (Vite, TypeScript, Wails runtime)
cd frontend
npm install
cd ..
```

---

## 3. Run in Development Mode

Development mode gives you hot-reload for both the Go backend and the frontend:

```bash
wails3 dev
```

or, if you have Task installed:

```bash
task dev
```

The application window opens automatically. Click **Start Scan** to begin detecting beacons.

---

## 4. Build a Production Binary

```bash
wails3 build
```

or:

```bash
task build
```

The binary is placed in the `build/bin/` directory:

- **macOS**: `build/bin/beacon-scout.app` (drag it to *Applications* if you like)
- **Windows**: `build/bin/beacon-scout.exe`

To create an installer/package instead of a plain binary:

```bash
task package          # NSIS installer on Windows, .app/DMG-style bundle on macOS
```

---

## 5. Platform-Specific Notes

### macOS

1. **Bluetooth permission** — the app declares `NSBluetoothAlwaysUsageDescription` in its `Info.plist`, so macOS shows a prompt: *"Beacon Scout would like to use Bluetooth"*. Click **Allow**. If you see an error like **"timeout enabling CentralManager"**, the permission was never granted (the prompt may have been auto-denied). Fix it with:
   ```bash
   tccutil reset All com.example.beaconscout
   ```
   then relaunch the app and accept the prompt. You can also toggle it under *System Settings → Privacy & Security → Bluetooth*.
   > **Note for `wails3 dev`:** when running from a terminal, macOS attributes the Bluetooth permission to the **terminal app** (Terminal/iTerm). Grant permission to your terminal, or run the built `.app` bundle instead.
2. **Gatekeeper** — if you downloaded a built `.app` from another machine and macOS blocks it, right-click the app → **Open**, or run:
   ```bash
   xattr -dr com.apple.quarantine beacon-scout.app
   ```
3. No admin/root privileges are required for scanning.

### Windows

1. **Bluetooth stack** — Windows 10/11 with the standard Microsoft Bluetooth stack works out of the box. The app uses the WinRT Bluetooth APIs (`BluetoothLEAdvertisementWatcher`); no extra drivers are needed.
2. **SmartScreen** — unsigned builds may trigger a SmartScreen warning. Choose **More info → Run anyway** during testing.
3. Make sure Bluetooth is **on** and the machine is not in airplane mode (*Settings → Bluetooth & devices*).

---

## 6. Testing the Application

### 6.1 Automated checks

```bash
# Backend compiles cleanly
go vet ./...
go build ./...

# Frontend builds cleanly
cd frontend && npm run build && cd ..
```

### 6.2 Manual smoke test

1. Start the app (`wails3 dev` or run the built binary).
2. Click **Start Scan**.
3. Within a few seconds you should see rows appear in the table: MAC address, name, RSSI, beacon type, etc.
4. Click **Stop Scan** — new rows should stop appearing, existing data remains.
5. Click **Clear** — the list becomes empty.

### 6.3 Verifying beacon parsing

You need at least one advertising device. Options:

- A real BLE beacon / smart band / wireless earbuds in pairing mode.
- A smartphone running a beacon simulator app:
  - **Android**: "Beacon Simulator"
  - **iOS/macOS**: "LightBlue" or any iBeacon/Eddystone broadcaster app
- Another computer with a USB BLE dongle (e.g., using `bluetenizer` or a small Go/Python broadcaster script).

Broadcast these formats and confirm the app identifies them correctly:

| Broadcast | Expected "Type" column | Expected detail fields |
|---|---|---|
| iBeacon (UUID + major/minor) | `iBeacon` | Parsed UUID, major, minor, measured power |
| Eddystone-URL | `Eddystone-URL` | Decoded URL (e.g. `https://example.com`) |
| Eddystone-UID | `Eddystone-UID` | Namespace + instance ID |
| AltBeacon | `AltBeacon` | Id1/Id2/Id3 |
| Plain named device | `` (empty) | Name + service UUIDs only |

### 6.4 Testing filters

While scanning, verify each filter narrows the list immediately (filters are applied on the Go side and results refresh every second):

- **RSSI range** (e.g. min `-70`) → only strong signals remain.
- **Name contains** (e.g. `pixel`) → only matching names.
- **MAC contains** (e.g. `AA:BB`) → partial address match.
- **Company ID** (`0x004c`) → Apple manufacturers only.
- **Beacon type** dropdown → e.g. show only `iBeacon`.
- **Active only** → only devices seen in the last N seconds.

### 6.5 Testing sorting

Click column headers to sort ascending/descending by **RSSI**, **Name**, **Last Seen**, **Count**, and **Address**. Sorting applies to the filtered set.

### 6.6 Edge cases worth checking

- Scanning with **no** beacons nearby → empty list, no crash.
- Rapidly toggling **Start/Stop Scan** → no duplicate goroutines/errors in the console.
- A device broadcasting very fast (many packets/sec) → single row per MAC, `Count` increases, RSSI min/max/avg update.
- Restarting the app → store starts empty (by design; nothing is persisted).

---

## 7. Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| `wails3: command not found` | `%GOPATH%/bin` not on PATH — add it, or re-run `go install github.com/wailsapp/wails/v3/cmd/wails3@latest` |
| Blank window in dev mode | Frontend not built — run `cd frontend && npm install && npm run build:dev` |
| Scan starts but list stays empty | Bluetooth off, permission denied (macOS), VM without Bluetooth passthrough, or no beacons in range. Test with a phone beacon simulator. |
| "Bluetooth adapter not found" | Dongle unplugged / driver missing. Check *Device Manager* (Windows) or *System Information → Bluetooth* (macOS). |
| Very few devices visible | Increase transmit distance awareness: many beacons advertise at −90 dBm; lower the RSSI filter minimum to `-200` to disable filtering. |
| macOS prompt never appears | Reset privacy: *System Settings → Privacy & Security → Bluetooth*, remove the app entry, restart the app. |

---

## 8. Uninstall

- **Dev usage**: delete the project folder; optionally `go clean -modcache` for the Go modules.
- **macOS**: move `beacon-scout.app` to Trash, then optionally remove its preferences/cache under `~/Library/Application Support/`.
- **Windows**: remove the executable, or uninstall via *Settings → Apps* if you installed the NSIS package.
