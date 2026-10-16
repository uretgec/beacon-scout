package main

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"tinygo.org/x/bluetooth"
)

// ScanService drives the BLE advertisement scanner (tinygo.org/x/bluetooth)
// and exposes control methods to the frontend through Wails bindings.
// It never connects to devices; it only passively captures advertisements.
type ScanService struct {
	app   *application.App
	store *BeaconStore

	mu         sync.Mutex
	scanning   bool
	enabling   int32        // 1 while adapter.Enable() is in flight — drives the "scan:state" progress events
	enableDone atomic.Int32 // 1 once Enable() has returned (success or error) — stops the enabling watchdog
	adapterOn  bool
	stopScan   func() error
	lastEmit   time.Time
	filter     FilterOptions
	sortBy     string
	sortDir    string
	interval   time.Duration // refresh period for the auto-push ticker (default 1s)
	ticker     *time.Ticker
	pushStop   chan struct{} // stops the current live-push loop (guarded by mu)
	dataSeq    atomic.Uint64 // bumped on every ingested advertisement
	emittedSeq uint64        // dataSeq value at last emit (guarded by mu)
}

func NewScanService(store *BeaconStore) *ScanService {
	return &ScanService{
		store:    store,
		sortBy:   "rssi",
		sortDir:  "desc",
		interval: 0, // auto-refresh OFF by default (user opts in via the header input)
		// Default filter mirrors the UI defaults in index.html. Without this,
		// every advertisement push between launch and the first Apply ran with a
		// zero-value filter (ActiveWindow=0), whose "active only" rule drops ALL
		// records — the table stayed near-empty until Apply was pressed.
		filter: FilterOptions{
			MinRSSI:      -200,
			MaxRSSI:      200,
			BeaconType:   "any",
			ActiveOnly:   true,
			ActiveWindow: 10,
		},
	}
}

// emitState pushes the authoritative UI state ("scan:state" event) so the
// frontend never has to guess whether a scan is starting, running or idle.
// phase: "idle" | "enabling" | "scanning".
func (s *ScanService) emitState(phase string) {
	if s.app == nil {
		return
	}
	if ok := s.app.Event.Emit("scan:state", map[string]any{
		"phase":    phase,
		"scanning": phase == "scanning",
	}); !ok {
		log.Printf("[scan] emit scan:state(%s) returned false (no listeners?)", phase)
	}
}

// refreshTicker pushes the current result list to the frontend at a fixed
// cadence while scanning. It pauses when no scan is running or when new
// advertisements are not arriving (nothing to refresh), satisfying the rule
// "no advertising -> no refresh". Interval 0 = paused (ticker stopped).
func (s *ScanService) refreshTicker() {
	s.mu.Lock()
	if s.ticker != nil {
		s.mu.Unlock()
		return
	}
	interval := s.interval
	if interval <= 0 {
		interval = time.Second // will be re-tuned by SetRefreshIntervalMs
	}
	s.ticker = time.NewTicker(interval)
	t := s.ticker
	s.mu.Unlock()

	for range t.C {
		s.mu.Lock()
		scanning, interval := s.scanning, s.interval
		enabling := atomic.LoadInt32(&s.enabling) == 1
		s.mu.Unlock()
		if (!scanning && !enabling) || interval <= 0 {
			continue // fully idle or paused: no pushes
		}
		// Only push when advertisement data actually changed since last emit.
		s.emitIfChanged()

		// Safety net against a stuck "enabling" UI: if the frontend is still
		// showing the enabling phase (adapter enable in flight) but results are
		// already flowing — i.e. scanning demonstrably works — force-publish the
		// authoritative "scanning" state so the button can never hang on
		// "Enabling adapter…". Harmless once the normal path has cleared it.
		if enabling && s.store.Count() > 0 {
			log.Printf("[scan] watchdog: results arriving while 'enabling' flag set — publishing scanning state")
			s.emitState("scanning")
		}
	}
}

// SetRefreshIntervalMs changes the push-throttle period (ms) for live result
// updates. New semantics: advertisements ALWAYS reach the UI while scanning —
// this value only controls how often a snapshot is pushed.
//
//	0     → real-time mode (pushes coalesced at ~150 ms, lowest latency)
//	N>0   → at most one push per N ms (reduces IPC load on busy environments)
//
// The scan never stops producing updates regardless of this setting.
func (s *ScanService) SetRefreshIntervalMs(ms int) {
	s.mu.Lock()
	if ms <= 0 {
		s.interval = 0 // real-time mode (see emitLive): pushes coalesce at ~150 ms
		log.Printf("[scan] refresh set to real-time (unthrottled)")
	} else {
		d := time.Duration(ms) * time.Millisecond
		// Floor of 100 ms protects the IPC channel from overflows.
		if d < 100*time.Millisecond {
			d = 100 * time.Millisecond
		}
		s.interval = d
		if s.ticker != nil {
			s.ticker.Reset(d)
		}
		log.Printf("[scan] refresh interval set to %s", d)
	}
	tickerExists := s.ticker != nil
	scanning := s.scanning
	interval := s.interval
	s.mu.Unlock()

	// Refresh turned ON while a scan is already running: the ticker was never
	// created (StartScan skips it when refresh defaults to OFF), so create it
	// now. refreshTicker is idempotent and lock-guarded, safe to call here.
	if !tickerExists && scanning && interval > 0 {
		s.refreshTicker()
	}
}

// GetRefreshIntervalMs returns the current refresh period (0 = paused).
func (s *ScanService) GetRefreshIntervalMs() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int(s.interval / time.Millisecond)
}

// Init is called by Wails when the service is registered.
func (s *ScanService) Init(app *application.App) {
	s.app = app
	log.Printf("[scan] ScanService initialised")
}

// Status returns whether a scan is currently running.
func (s *ScanService) Status() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanning
}

// EnableAdapter powers on the Bluetooth adapter ahead of time so the macOS
// permission dialog (if any) appears immediately, instead of timing out later.
// Safe to call multiple times; returns a friendly, actionable error if it fails.
func (s *ScanService) EnableAdapter() error {
	s.mu.Lock()
	if s.adapterOn {
		s.mu.Unlock()
		return nil // already enabled — skip (avoids a second permission stall)
	}
	// If adapter.Enable() is ALREADY in flight (e.g. StartScan triggered it),
	// return immediately instead of stacking a second concurrent enable. The
	// in-flight call will publish the authoritative "scanning"/error state via
	// events; callers must not sit and wait here.
	if atomic.LoadInt32(&s.enabling) == 1 {
		s.mu.Unlock()
		log.Printf("[scan] EnableAdapter: enable already in flight — returning without waiting")
		return nil
	}
	// If a scan is already running, the adapter is necessarily powered on.
	if s.scanning {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	adapter := bluetooth.DefaultAdapter

	// NOTE: tinygo.org/x/bluetooth has no Powered()/power-state API; the only
	// way to learn the adapter state is adapter.Enable() itself.

	// On macOS, adapter.Enable() blocks until CoreBluetooth reports the
	// CentralManager state. If Bluetooth permission has NOT been granted,
	// that callback never fires and tinygo's internal ~10s timeout triggers
	// "timeout enabling CentralManager". We run Enable in its own goroutine
	// with our own deadline and translate the failure into actionable steps.
	log.Printf("[scan] enabling Bluetooth adapter (waiting for CoreBluetooth power-on / permission grant)...")
	start := time.Now()

	// Tell the UI we are in the "enabling" phase. A watchdog keeps re-publishing
	// it once per second ONLY while Enable is genuinely still pending — if the
	// scan callback starts delivering advertisements first (CoreBluetooth may
	// invoke the Scan handler before adapter.Enable()'s completion callback
	// fires, which previously left the button stuck on "Enabling adapter…"),
	// the watchdog publishes "scanning" immediately and clears the enabling flag.
	atomic.StoreInt32(&s.enabling, 1)
	s.enableDone.Store(0)
	s.emitState("enabling")
	watchdogStop := make(chan struct{})
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-watchdogStop:
				return
			case <-tick.C:
				if atomic.LoadInt32(&s.enabling) == 0 || s.enableDone.Load() == 1 {
					return
				}
				if s.store.Count() > 0 {
					// Advertisements are already flowing → the adapter is effectively
					// usable even though Enable() has not returned yet. Unstick the UI.
					log.Printf("[scan] watchdog: advertisements flowing while Enable() pending — publishing scanning state")
					atomic.StoreInt32(&s.enabling, 0)
					s.mu.Lock()
					s.adapterOn = true
					s.mu.Unlock()
					s.emitState("scanning")
					s.emitNow()
					return
				}
				s.emitState("enabling")
			}
		}
	}()
	defer func() {
		s.enableDone.Store(1)
		atomic.StoreInt32(&s.enabling, 0)
		close(watchdogStop)
	}()

	// Darwin: install the CoreBluetooth bridge. It captures TxPowerLevel /
	// Connectable from cbgo.AdvFields — fields that tiny-go/bluetooth v0.16.0
	// parses but silently drops when building the ScanResult, which is why
	// Flags and Tx Power never reached the UI on macOS before. The library's
	// Enable() replaces the CentralManager delegate with its own, so we
	// re-install ours immediately after every successful enable (see below).
	installDarwinBridge(adapter)

	errCh := make(chan error, 1)
	go func() { errCh <- adapter.Enable() }()
	select {
	case err := <-errCh:
		log.Printf("[scan] adapter.Enable() returned after %s, err=%v", time.Since(start).Round(time.Millisecond), err)
		if err != nil {
			ferr := friendlyEnableError(err)
			s.emitError(ferr)
			return ferr
		}
		// Enable() replaced the CentralManager delegate with the library's
		// own; put our wrapper back on top so Tx Power / connectability
		// capture actually works (no-op on Linux/Windows).
		reinstallDarwinBridge(adapter)
		s.mu.Lock()
		s.adapterOn = true
		s.mu.Unlock()
		return nil
	case <-time.After(25 * time.Second):
		err := errors.New("Timed out (25 s) waiting for CoreBluetooth to power on.\n" +
			"On macOS this almost always means Bluetooth permission was DENIED or is pending:\n" +
			"  1. System Settings → Privacy & Security → Bluetooth → enable the switch for this app.\n" +
			"  2. In dev mode (`wails3 dev`) the permission belongs to your TERMINAL app\n" +
			"     (Terminal / iTerm / VS Code), not the beacon-scout binary.\n" +
			"  3. Reset stale grants, then fully quit and relaunch:\n" +
			"       tccutil reset Bluetooth com.uretgec.beacon-scout\n" +
			"          If that reports \"No such bundle identifier\", find the real id with:  tccutil list Bluetooth")
		log.Printf("[scan] adapter.Enable() TIMED OUT after %s", time.Since(start).Round(time.Second))
		s.emitError(err)
		return err
	}
}

func friendlyEnableError(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "timeout enabling CentralManager") {
		msg += "  → classic macOS symptom: Bluetooth permission is DENIED (or never granted) for this app / terminal."
	}
	hint := "\nPlease check:\n" +
		"• Bluetooth is turned on (Control Center / System Settings).\n" +
		"• macOS: System Settings → Privacy & Security → Bluetooth → allow this app (or your terminal), then relaunch.\n" +
		"• macOS reset command uses the REAL bundle id — find it with: tccutil list Bluetooth\n" +
		"• Windows: Settings → Bluetooth & devices → Bluetooth is On, and the adapter driver is up to date."
	return fmt.Errorf("could not enable Bluetooth adapter: %s%s", msg, hint)
}

// StartScan begins passive BLE advertisement scanning.
// Returns the resulting scanning state (true) so the frontend can update the
// UI even if events are missed. Errors are returned as a message string.
func (s *ScanService) StartScan() (bool, error) {
	s.mu.Lock()
	if s.scanning {
		s.mu.Unlock()
		log.Printf("[scan] StartScan called but already scanning — resyncing UI state")
		// Not silently ignored: push current state so the button flips to "Stop".
		s.emitState("scanning")
		s.emitNow()
		return true, nil
	}
	s.mu.Unlock()

	log.Printf("[scan] StartScan requested")
	s.emitState("enabling")
	adapter := bluetooth.DefaultAdapter

	if err := s.EnableAdapter(); err != nil {
		return false, err
	}

	errCh := make(chan error, 1)
	firstPacket := sync.OnceFunc(func() {
		log.Printf("[scan] first advertisement received ✔")
		// Unstick the UI RIGHT NOW, synchronously, from inside the scan callback.
		// CoreBluetooth delivers scan results before adapter.Enable()'s completion
		// callback fires, so this is often the FIRST moment we know scanning works.
		// The previous implementation only emitted a lightweight "scan:state" event
		// here — but the frontend's self-heal path lives in the "beacons:list"
		// handler, which was never triggered at this point (emitThrottled is a no-op
		// while refresh is paused, and the watchdog ticker needs both a live ticker
		// AND interval > 0). Result: the button hung on "Enabling adapter…" even
		// though advertisements were flowing. Fix: clear the flags, publish BOTH the
		// state event and a full list payload (which carries the authoritative
		// phase), so any of the three frontend recovery paths flips the button.
		if atomic.CompareAndSwapInt32(&s.enabling, 1, 0) && s.enableDone.Load() == 0 {
			log.Printf("[scan] first packet while Enable() pending — publishing scanning state early")
			s.mu.Lock()
			s.adapterOn = true
			s.scanning = true
			s.mu.Unlock()
			s.emitState("scanning")
			s.emitNow() // list payload with phase:"scanning" → frontend self-heals
		}
	})
	go func() {
		err := adapter.Scan(func(a *bluetooth.Adapter, res bluetooth.ScanResult) {
			firstPacket()
			// Darwin: CoreBluetooth parsed Tx Power / Connectable into cbgo.AdvFields
			// but the library drops them before ScanResult — hand the captured values
			// to the store via the one-shot bridge (no-op on other platforms).
			attachDarwinExtras(res)
			s.store.ingest(res.Address, res.RSSI, res.AdvertisementPayload)
			s.dataSeq.Add(1)
			// Live push: new advertisements reach the UI immediately (deduplicated
			// per device inside the store). The header "Refresh" control no longer
			// gates visibility — it only throttles these pushes: 0 = real-time,
			// N>0 = at most one push per N ms. So the list always updates while
			// scanning without pressing Apply.
			s.emitLive()
		})
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err != nil {
			wrapped := fmt.Errorf("scan failed to start: %w", err)
			log.Printf("[scan] %v", wrapped)
			s.emitError(wrapped)
			s.emitState("idle")
			return false, wrapped
		}
		log.Printf("[scan] adapter.Scan() returned immediately without error")
	case <-time.After(2 * time.Second):
		// No immediate error: scanning is up and running.
	}

	s.mu.Lock()
	s.scanning = true
	s.adapterOn = true
	s.stopScan = adapter.StopScan
	s.mu.Unlock()
	// Enable() has effectively completed for UI purposes: stop every "enabling"
	// mechanism (watchdog re-emits, stuck-flag self-heals) and broadcast the
	// authoritative state + a fresh list snapshot. This guarantees the button
	// flips to "Stop Scan" even if the permission/enable race resolved silently.
	s.enableDone.Store(1)
	atomic.StoreInt32(&s.enabling, 0)
	s.emitState("scanning")
	s.emitNow()

	// Auto-refresh ticker is only meaningful when an interval is set; while
	// refresh is paused (default OFF) we must NOT create a busy 1s ticker —
	// state pushes above already cover the UI needs.
	if s.GetRefreshIntervalMs() > 0 {
		s.refreshTicker()
	}

	log.Printf("[scan] scanning active — waiting for advertisements")
	s.emitState("scanning")
	s.emitNow()
	return true, nil
}

// StopScan stops an in-progress scan and returns the new scanning state.
func (s *ScanService) StopScan() error {
	s.mu.Lock()
	if s.scanning && s.stopScan != nil {
		if err := s.stopScan(); err != nil {
			log.Printf("[scan] StopScan error: %v", err)
		}
		s.scanning = false
		s.stopScan = nil
		log.Printf("[scan] stopped (store holds %d devices)", s.store.Count())
	} else {
		s.scanning = false // force idle flag so UI can never get stuck "scanning"
		log.Printf("[scan] StopScan called while not scanning — forcing idle state")
	}
	if s.pushStop != nil {
		close(s.pushStop)
		s.pushStop = nil
	}
	s.mu.Unlock()
	s.emitState("idle")
	s.emitNow()
	return nil
}

// SetFilter stores the advanced filter options and returns how many records
// match, so the frontend can give visible feedback when Apply is clicked.
func (s *ScanService) SetFilter(f FilterOptions) int {
	s.mu.Lock()
	s.filter = f
	s.mu.Unlock()
	matched := len(s.GetResults())
	log.Printf("[scan] filter applied — %d records match", matched)
	s.emitNow()
	return matched
}

// SetSort configures sorting (field + direction) applied before pushing lists.
// Supported fields: rssi | avgRssi | name | lastSeen | firstSeen | count | address.
func (s *ScanService) SetSort(sortBy, dir string) {
	s.mu.Lock()
	// Normalise to the lowercase keys understood by sortRecords.
	// ("lastSeen" -> "lastseen", "rssi" stays "rssi", etc.)
	s.sortBy = strings.ToLower(sortBy)
	if dir == "asc" {
		s.sortDir = "asc"
	} else {
		s.sortDir = "desc"
	}
	s.mu.Unlock()
	log.Printf("[scan] sort set: field=%q dir=%q", s.sortBy, s.sortDir)
	s.emitNow()
}

// GetResults returns the current filtered+sorted list on demand.
func (s *ScanService) GetResults() []BeaconRecord {
	s.mu.Lock()
	f, sortBy, dir := s.filter, s.sortBy, s.sortDir
	s.mu.Unlock()
	return s.store.snapshotFilteredSorted(f, sortBy, dir)
}

// GetTotalCount is the single authoritative source for the header's "total"
// counter: unique devices currently held in the store (since launch/Clear).
// The frontend polls it on every render, so it stays correct even when no
// beacons:list events are flowing (refresh OFF, scan stopped…).
func (s *ScanService) GetTotalCount() int {
	return s.store.Count()
}

// ClearResults empties the stored device list and immediately pushes an empty
// snapshot so the table clears even before the next 1-second throttled update.
func (s *ScanService) ClearResults() {
	s.store.Clear()
	log.Printf("[scan] results cleared")
	s.emitNow()
}

// emitLive is called from the scan callback for every received advertisement.
// It ALWAYS delivers updates to the UI while scanning — the header "Refresh"
// input merely throttles the push cadence:
//
//	0 ms  → real-time (one push per new-device batch, min 150 ms apart)
//	N > 0 → at most one push per N ms
//
// Previously this path was a silent no-op when refresh was paused, which made
// the list look frozen until Apply was pressed. That behaviour is gone.
func (s *ScanService) emitLive() {
	s.mu.Lock()
	interval := s.interval
	if interval <= 0 {
		interval = 150 * time.Millisecond // minimum spacing between live pushes
	}
	due := time.Since(s.lastEmit) >= interval
	s.mu.Unlock()
	if due {
		s.emitNow()
	} else {
		s.scheduleFlush(interval - time.Since(s.lastEmit))
	}
}

// scheduleFlush guarantees the pending data change reaches the UI shortly
// after the throttle window closes (trailing-edge flush), so nothing gets
// lost between pushes. Idempotent while a flush is already pending.
func (s *ScanService) scheduleFlush(d time.Duration) {
	s.mu.Lock()
	if s.pushStop != nil {
		s.mu.Unlock()
		return // flush already scheduled
	}
	stop := make(chan struct{})
	s.pushStop = stop
	s.mu.Unlock()

	time.AfterFunc(d, func() {
		s.mu.Lock()
		active := s.pushStop == stop
		s.pushStop = nil
		s.mu.Unlock()
		if !active {
			return // superseded/cancelled
		}
		s.emitIfChanged()
	})
}

// emitIfChanged pushes a snapshot only when new advertisements arrived since
// the last emit — "no advertising, no refresh".
func (s *ScanService) emitIfChanged() {
	s.mu.Lock()
	changed := s.dataSeq.Load() != s.emittedSeq
	s.mu.Unlock()
	if changed {
		s.emitNow()
	}
}

func (s *ScanService) emitNow() {
	s.mu.Lock()
	f, sortBy, dir := s.filter, s.sortBy, s.sortDir
	s.lastEmit = time.Now()
	scanning := s.scanning
	s.mu.Unlock()

	if s.app == nil {
		return
	}
	list := s.store.snapshotFilteredSorted(f, sortBy, dir)

	s.mu.Lock()
	s.emittedSeq = s.dataSeq.Load()
	s.mu.Unlock()

	// EventManager.Emit returns bool (true = delivered), not an error.
	if ok := s.app.Event.Emit("beacons:list", map[string]any{
		"scanning": scanning,
		// phase lets the frontend self-heal a stuck "Enabling adapter…" button:
		// any list push from the backend carries the authoritative scan state.
		"phase": scanPhase(scanning, atomic.LoadInt32(&s.enabling) == 1),
		// total = unique devices currently held in the store (since launch or
		// last Clear) — the single authoritative counter shown in the header.
		"total":   s.store.Count(),
		"matches": len(list), // devices passing the ACTIVE filter right now
		"items":   list,
	}); !ok {
		log.Printf("[scan] emit beacons:list returned false (no listeners?)")
	}
}

// scanPhase derives the UI phase string from the two authoritative flags.
func scanPhase(scanning, enabling bool) string {
	switch {
	case scanning:
		return "scanning"
	case enabling:
		return "enabling"
	default:
		return "idle"
	}
}

// AdTypes returns the curated Bluetooth SIG AD-type catalog so the filter UI
// can offer named selections instead of requiring users to know hex codes.
func (s *ScanService) AdTypes() []map[string]any {
	return AdTypeCatalog()
}

// EnablingAdapter reports whether adapter.Enable() is currently in flight
// (e.g. waiting for the macOS Bluetooth permission dialog).
func (s *ScanService) EnablingAdapter() bool {
	return atomic.LoadInt32(&s.enabling) == 1
}

// emitError pushes a human-readable error to the frontend ("scan:error" event)
// so failures are visible as a toast in the UI even if the console is closed.
func (s *ScanService) emitError(err error) {
	if s.app == nil || err == nil {
		return
	}
	if ok := s.app.Event.Emit("scan:error", err.Error()); !ok {
		log.Printf("[scan] failed to emit scan:error (no listeners?)")
	}
}
