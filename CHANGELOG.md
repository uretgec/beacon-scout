# Changelog

All notable changes to Beacon Scout. Short SHA = commit id in this repository (`git show <sha>`).

## Latest (2026-10-27)

### Fixed

- **CI (release.yml):** gofmt gate failed on `build/ios/*.go` files. Root cause:
  the `.gitignore` rule `build/` was silently excluding ALL Wails build source
  assets from every clone — platform Taskfiles, Info.plist files, icons,
  `main.m`/`main_ios.go`, Xcode templates. Fresh clones therefore had no
  darwin/windows/ios Taskfiles at all, which explains the cascading errors
  (`ios/arm64 requires external (cgo) linking`, missing embed dirs, etc.).
  Now `build/` is tracked; only output paths are ignored (`build/bin/`,
  `build/windows/*.exe`, NSIS bootstrap, `Assets.car`). The three iOS Go files
  were `gofmt -w`-formatted and 75 previously-untracked source files committed
  (verified: zero exe/dll/o/a/dylib among them). Commit `2c08825`.
- **CI (release.yml):** gofmt step now checks tracked Go files only
  (`git ls-files '*.go' | xargs gofmt -l`), so generated bindings output can
  never break the lint gate again. Commit `25b334e`.

## Previous (2026-10-26)

### Fixed#2

- **CI (release.yml):** Frontend build failed on fresh checkouts with
  `[plugin wails-typed-events] Event bindings module not found …` — Vite's
  `wailsTypedEventsPlugin` requires generated event bindings before any build.
  The `lint` and `test` jobs now install the pinned wails3 CLI
  (`v3.0.0-beta.30`, matching go.mod) and run `wails3 generate bindings -ts`
  before `npm run build`. Also bumped runner toolchains for deprecation
  warnings: `GO_VERSION 1.25 → 1.26`, `NODE_VERSION 20 → 24`,
  `actions/setup-node@v4 → v5` (all jobs). Commit `f26ef54`.
- **CI (release.yml):** `go vet`/`go test` failed on macOS & Windows runners with
  `pattern all:frontend/dist: no matching files found`. Static analysis now runs in a
  dedicated `lint` job that builds the frontend first; the per-OS matrix became a build
  smoke test (`npm ci && npm run build` + `go build .`) gated behind lint. Commit `8b4dbe9`.
- Fresh clones always have `frontend/dist/` present (committed `.gitkeep`; ignore rule is
  now `frontend/dist/*` + exception), so `//go:embed all:frontend/dist` never breaks
  before `npm run build`. Commit `8b4dbe9`.

## 2026-10-18

- **CI/CD: GitHub Actions unsigned release pipeline** - new `.github/workflows/release.yml`:
  - `test` job matrix (ubuntu / macos / windows): `gofmt` check, `go vet ./...`, `go test ./...`, frontend `npm ci && npm run build`. Builds are gated on all tests passing.
  - `darwin` job (macos-latest): installs wails3 CLI + task, generates bindings, `task darwin:package:universal` + `package:dmg` -> universal (arm64+amd64) unsigned `.dmg`, ad-hoc codesign (`codesign -s -`).
  - `windows` job (windows-latest): installs wails3 (+ GOPATH bin fix) and NSIS via choco, `task windows:build` + `task windows:package FORMAT=nsis INSTALL_SCOPE=user` -> per-user NSIS installer `.exe` (unsigned, no UAC).
  - `release` job: sha256 checksums for every asset + GitHub Release with built-in instructions for running unsigned builds (Gatekeeper "Open Anyway" / `xattr -cr` on macOS; SmartScreen "More info -> Run anyway" / Properties->Unblock on Windows).
  - Triggers: push of any `v*` tag, or manual `workflow_dispatch` (optional version + prerelease flag).
- docs(README): new **"Downloading pre-built binaries"** section mirroring the release-notes steps for end users (macOS Gatekeeper + Windows SmartScreen guidance, checksum verification).

## 2026-10-17 (later)

- `8e3569f` fix: unused-function warnings in `numbersdb.go` resolved by wiring the helpers into real call sites instead of deleting them: new bound method `ServiceUuidNames(uuids)` (detail table's "Service UUIDs" row now hydrates to SIG names, e.g. `0x180F Battery Service`, via `.sig-uuids` cells in `main.ts`), new bound method `CharacteristicNames()` (full GATT characteristic catalogue exposed to the frontend), and `hexToUint16`/`serviceNamesFor` covered by new `numbersdb_test.go`. Truly dead single-UUID lookups `serviceName()`/`characteristicName()` removed (superseded by the batch methods).
- `8271a59` chore: remove stray `beacon-scout.exe` again; pre-commit binary guard hook installed.
- ``708345a``/``047f04a`` — fix(ui): GAP Appearance picker populated from the embedded SIG database (`ScanService.Appearances()`); `appearanceSelect` ReferenceError resolved.
- SIG Assigned Numbers integration (vendored, `//go:embed`): `numbersdb.go` + `data/numbers/` (company_ids, gap_appearance, service_uuids, characteristic_uuids + Apache-2.0 LICENSE from NordicSemiconductor/bluetooth-numbers-database). New service methods `Companies()`, `Appearances()`, `CompanyName(code)`, `ServiceNames()`; searchable Manufacturer & Appearance pickers in the filters sidebar; company names in detail view now resolve against the full SIG table (~1700 companies).
- ``b5084c2`` — fix: header "total" counter live-fetch via `GetTotalCount()` on every render.
- Housekeeping: **deleted `conversation.md`** (planning notes no longer needed); **.gitignore rewritten** — deduplicated the three accumulated `*.exe` rules and leftover scratch entries into one clean sectioned file (binaries, frontend artifacts, generated bindings w/ committed uretgec stubs, taskfile overrides, mobile per-machine files, OS/editor noise); **README.md rewritten** as a concise user-facing description of Beacon Scout (features, quick start, project layout, credits).

## 2026-10-17

- ``e1d81d5`` — chore: repository identity -> **github.com/uretgec/beacon-scout** (go.mod, binding dir + imports, bundle ids, gitignore, README). Verified on-disk at HEAD `e1d81d5`: zero `example/beacon-scout` references remain; exe count = 0.
- docs: added `conversation.md` — tiny-go/bluetooth capability audit + bluetooth-numbers-database (SIG assigned numbers) analysis and `//go:embed` plan.

- ``55150af`` — chore: repository identity updated to **github.com/uretgec/beacon-scout** (was the `github.com/example` placeholder). Module path in `go.mod`, frontend binding imports + committed placeholder bindings directory (`frontend/bindings/github.com/uretgec/beacon-scout/`), `.gitignore` allow-rules, macOS bundle id `com.uretgec.beacon-scout` (`build/darwin/Info*.plist`, TCC reset hint in `scanservice.go` error text and `INSTALL.md`), iOS bundle ids, Windows manifest / MSIX identity, `build/config.yml` product info (Uretgec / Beacon Scout), and README repo link all aligned. Note: after pulling this change run `wails3 generate bindings` once so generated bindings land in the new directory.

## 2026-10-16

- `b5084c2` — fix: header "total" counter was stuck at 0 because it only updated when `beacons:list` events flowed (i.e. while scanning with refresh pushing). Added backend `ScanService.GetTotalCount()` (= store.Count()) as the single live source; frontend now fetches it on **every render** (`updateTotalCounter()`), so total is correct even with refresh OFF, scan stopped, or after Clear. Removed stale `parseTotal`/fallback paths.
- `e37698e` — fix: remove redundant "seen" counter entirely. Backend payload now carries a single authoritative `total` (= store.Count(), unique devices since launch/Clear) plus `matches`; frontend header shows `N shown · M matching / T total`. Fixes both the always-zero and duplicate-counter confusion.
- `1ad962c` — fix: emit store count in beacons:list payload (header showed "0 seen" because the field was missing)
- `2d518fc` — revert: restore working tree to `d233bb8` (darwinbridge + adfields files back, per user request)
- `e494f66` — fix: rebuild_test.go used non-existent `bluetooth.NewUUID16` → correct v0.16.0 API is `New16BitUUID`; gofmt test files; stray `beacon-scout.exe` removed from disk again (git-tracked exe count = 0)
- `0dfeeaa` — feat(raw): "Full Advertisement" hex is now a complete Core Spec AD-structure stream. On macOS (CoreBluetooth hides on-air bytes) the store reconstructs ADV_IND+Scan Response merged TLV including **Flags (0x01), Shortened (0x08)/Complete (0x09) Local Name, Appearance (0x19), Tx Power Level (0x0A)**, UUID lists, Manufacturer & Service Data; new `rawRebuilt` flag + "Source" row in detail table tells captured-verbatim vs reconstructed. New round-trip test `TestRebuildRawADFullAdvertisement`. ASCII column stays removed.
- `41e5fd1` — fix: placeholder FilterOptions completed (deviceType, rssiTrend, rawContains, serviceUuidExact, namePrefix/Suffix); delete stray beacon-scout.exe
- `2a69391` — docs: CHANGELOG with short SHA per change
- `4b1552b` — feat: detail table shows Device Type, Advertisement Type, Flags (decoded from AD 0x01), Local Name, Company (with SIG name lookup); Copy Table + Copy JSON clipboard buttons
- `160f282` — task snapshot 8ac51285-0bc7-47e4-8d43-69d909eb217b
- `656f0cd` — refactor: device detail panel as unified table with sectioned thead bands (Identity / RSSI & Timing / Beacon data / Raw payload); drop ASCII column from AD-structure table
- `a9955c5` — task snapshot b14e2317-97af-4f46-ba5e-744b9aa10dc0
- `7a217d6` — task snapshot e10c4822-86b3-4917-82e1-588ce883d589
- `0681481` — fix: re-apply live-filter block (was lost by a later snapshot revert); verified tsc+vite build clean
- `eb297e4` — feat: Start Scan auto-enables 2000ms refresh (Stop restores OFF); fix duplicate total in ListPayload
- `3dbd1f2` — UI: Filters sidebar open by default; toggle button green=open / grey=closed
- `8881fa4` — Stop Scan resets refresh to OFF; startup forces refresh 0; remove stray .exe; harden gitignore for **/*.exe
- `8a410d1` — fix: guarantee button flips off 'Enabling adapter' via 4 independent paths
- `41b9064` — fix: unstick 'Enabling adapter' button at first advertisement (scan callback fires before Enable() returns on macOS); refresh preset defaults to Off
- `d624847` — fix: enabling button stuck (in-flight enable guard, phase in list payload, watchdog); refresh default OFF; advanced filters collapsed by default

## 2026-10-14

- `3e78c64` — chore: commit ScanService placeholder bindings so fresh clones compile; ignore all other generated bindings
- `651ddb6` — fix: live 'Enabling adapter' progress via scan:state events; remove stray beacon-scout.exe; harden .gitignore for binaries
- `56bbdfc` — Fix FilterOptions TS error: typed placeholder bindings, full filter payload, Max RSSI default 200, advanced filter UI
- `11d19ed` — Fix sorting field mismatch, clear button feedback, inspector docs
- `7e235e4` — Fix scan button desync, stuck scanning state, and silent Apply Filters
- `4304279` — Fix payload.Bytes method-vs-field misuse flagged by go vet
- `d85c15b` — Fix compile errors: remove nonexistent adapter.Powered(), handle Event.Emit bool return, move inspector flag to WebviewWindowOptions (OpenInspectorOnStartup)
