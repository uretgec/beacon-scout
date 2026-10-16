# Changelog

All notable changes to Beacon Scout. Short SHA = commit id in this repository (`git show <sha>`).

## 2026-10-17

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
