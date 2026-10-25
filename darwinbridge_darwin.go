//go:build darwin

package main

import (
	"log"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"github.com/tinygo-org/cbgo"
	"tinygo.org/x/bluetooth"
)

// Darwin scan bridge.
//
// Why this exists: cbgolib (the CoreBluetooth binding used by
// tiny-go/bluetooth v0.16.0 on macOS) fully parses
// CBAdvertisementDataTxPowerLevelKey and CBAdvertisementDataConnectableKey
// into cbgo.AdvFields — but bluetooth.makeScanResult silently drops both
// fields when converting to the AdvertisementPayload interface. Result: on
// macOS an advertised Tx Power Level (AD type 0x0A) and the connectability
// flag never reach us, even though the device broadcasts them. This was the
// real reason "Flags" and "TX Power" stayed empty in the UI.
//
// Fix without touching library code: read the library's own CentralManager
// handle and its delegate instance out of bluetooth.Adapter's unexported
// fields (one-time reflect introspection), then install a thin wrapper
// delegate via cm.SetDelegate. The wrapper records the dropped AdvFields
// extras keyed by peripheral identifier and forwards every callback VERBATIM
// to the original delegate, so the library keeps working untouched — no
// recursion, no behavior change. The Scan callback (running synchronously
// inside DidDiscoverPeripheral) picks the extras up via attachDarwinExtras
// and hands them to store.ingest().
//
// IMPORTANT: the library's Enable() calls cm.SetDelegate(a.cmd) itself, which
// would shadow our wrapper. Therefore we re-install after every successful
// enable via reinstallDarwinBridge(); it always re-wraps whatever delegate is
// currently installed so callbacks are never lost or double-forwarded.
//
// If library internals ever change, introspection fails soft: install logs
// and skips, and the app behaves exactly as before (graceful degradation).

type darwinExtras struct {
	flags   *byte // synthesized from CBConnectableKey (connectability bit)
	txPower *int8 // CBAdvertisementDataTxPowerLevelKey -> AD type 0x0A
}

var (
	darwinMu      sync.Mutex
	darwinPending = map[string]darwinEntry{}
	darwinState   struct {
		wrapper *darwinDelegate
	}
)

type darwinEntry struct {
	e  darwinExtras
	at time.Time
}

const darwinPendingTTL = 2 * time.Second

func darwinStore(id string, e darwinExtras) {
	darwinMu.Lock()
	defer darwinMu.Unlock()
	now := time.Now()
	darwinPending[id] = darwinEntry{e: e, at: now}
	for k, v := range darwinPending {
		if now.Sub(v.at) > darwinPendingTTL {
			delete(darwinPending, k)
		}
	}
}

func darwinTake(id string) (darwinExtras, bool) {
	darwinMu.Lock()
	defer darwinMu.Unlock()
	v, ok := darwinPending[id]
	if ok {
		delete(darwinPending, id)
	}
	return v.e, ok
}

// attachDarwinExtras links the extras recorded for this ScanResult's
// peripheral to the one-shot handoff slot, for macosExtraFields to consume
// during the immediately following store.ingest() call (same goroutine).
func attachDarwinExtras(res bluetooth.ScanResult) {
	id := res.Address.UUID.String()
	e, ok := darwinTake(id)
	if !ok {
		return
	}
	setPendingExtras(e.flags, e.txPower)
}

// pendingExtras is the one-shot handoff between the scan callback and
// macosExtraFields (same goroutine, synchronous path).
var (
	pendingMu    sync.Mutex
	pendingFlags *byte
	pendingTx    *int8
	pendingHas   bool
)

func setPendingExtras(flags *byte, tx *int8) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	pendingFlags, pendingTx, pendingHas = flags, tx, true
}

func takeDarwinExtras() (flags *byte, shortName string, txPower *int8, appearance *uint16) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	if !pendingHas {
		return nil, "", nil, nil
	}
	pendingHas = false
	return pendingFlags, "", pendingTx, nil
}

// installDarwinBridge wraps the library's CentralManager delegate. Safe to
// call multiple times; failures are logged and ignored (Tx Power capture
// degrades to the previous behavior). Call right after EnableAdapter().
func installDarwinBridge(a *bluetooth.Adapter) {
	cm, current := cboIntrospect(a)
	if cm == nil || current == nil {
		log.Printf("[scan] darwin bridge: library internals not reachable — skipping (Tx Power capture unavailable)")
		return
	}

	darwinMu.Lock()
	w := darwinState.wrapper
	darwinMu.Unlock()

	if w == nil {
		w = &darwinDelegate{}
		darwinMu.Lock()
		darwinState.wrapper = w
		darwinMu.Unlock()
	}

	// Always forward to whatever delegate is currently installed (ours on
	// repeat calls, the library's fresh one after Enable()). Using the live
	// `current` as the forwarding target guarantees no callback is lost and
	// none is forwarded twice.
	darwinMu.Lock()
	w.original = current
	darwinMu.Unlock()

	cm.SetDelegate(w)
	log.Printf("[scan] darwin bridge installed/re-installed (Tx Power + connectability capture active)")
}

// reinstallDarwinBridge is called after every successful adapter.Enable():
// the library replaces the CentralManager delegate with its own during
// Enable(), so we must put our wrapper back on top. Identical semantics to
// installDarwinBridge but kept as a separate name for call-site clarity.
func reinstallDarwinBridge(a *bluetooth.Adapter) {
	installDarwinBridge(a)
}

// cboIntrospect reads bluetooth.Adapter's unexported darwin fields:
//   - cm  cbgo.CentralManager (the manager handle)
//   - the delegate currently installed on it (via reflection on cmd)
//
// Returns (nil, nil) on any structural mismatch.
func cboIntrospect(a *bluetooth.Adapter) (*cbgo.CentralManager, cbgo.CentralManagerDelegate) {
	v := reflect.ValueOf(a).Elem()

	fcm := v.FieldByName("cm")
	if !fcm.IsValid() || fcm.Type() != reflect.TypeOf(cbgo.CentralManager{}) {
		return nil, nil
	}
	cmVal := reflect.NewAt(fcm.Type(), unsafe.Pointer(fcm.UnsafeAddr())).Elem()
	cm := cmVal.Interface().(cbgo.CentralManager)

	fcmd := v.FieldByName("cmd")
	if !fcmd.IsValid() || fcmd.Kind() != reflect.Ptr || fcmd.IsNil() {
		return nil, nil
	}
	cmdVal := reflect.NewAt(fcmd.Type(), unsafe.Pointer(fcmd.UnsafeAddr())).Elem()
	orig, ok := cmdVal.Interface().(cbgo.CentralManagerDelegate)
	if !ok {
		return nil, nil
	}
	return &cm, orig
}

// darwinDelegate forwards every cbgo callback to the ORIGINAL library
// delegate while capturing the AdvFields extras the library would drop.
type darwinDelegate struct {
	mu       sync.Mutex
	original cbgo.CentralManagerDelegate
}

func (d *darwinDelegate) getOriginal() cbgo.CentralManagerDelegate {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.original
}

func (d *darwinDelegate) DidDiscoverPeripheral(cmgr cbgo.CentralManager, prph cbgo.Peripheral, advFields cbgo.AdvFields, rssi int) {
	var e darwinExtras
	if advFields.TxPowerLevel != nil {
		tp := int8(*advFields.TxPowerLevel)
		e.txPower = &tp
	}
	if advFields.Connectable != nil {
		f := byte(0x06) // LE general discoverable + BR/EDR not supported
		if !*advFields.Connectable {
			f = 0x04 // non-connectable: only "BR/EDR not supported"-ish semantics; ingest maps it to ADV_NONCONN_IND-style flags
		}
		e.flags = &f
	}
	darwinStore(prph.Identifier().String(), e)
	if o := d.getOriginal(); o != nil {
		o.DidDiscoverPeripheral(cmgr, prph, advFields, rssi)
	}
}

func (d *darwinDelegate) DidConnectPeripheral(cmgr cbgo.CentralManager, prph cbgo.Peripheral) {
	if o := d.getOriginal(); o != nil {
		o.DidConnectPeripheral(cmgr, prph)
	}
}

func (d *darwinDelegate) DidDisconnectPeripheral(cmgr cbgo.CentralManager, prph cbgo.Peripheral, err error) {
	if o := d.getOriginal(); o != nil {
		o.DidDisconnectPeripheral(cmgr, prph, err)
	}
}

func (d *darwinDelegate) DidFailToConnectPeripheral(cmgr cbgo.CentralManager, prph cbgo.Peripheral, err error) {
	if o := d.getOriginal(); o != nil {
		o.DidFailToConnectPeripheral(cmgr, prph, err)
	}
}

func (d *darwinDelegate) CentralManagerDidUpdateState(cmgr cbgo.CentralManager) {
	if o := d.getOriginal(); o != nil {
		o.CentralManagerDidUpdateState(cmgr)
	}
}

func (d *darwinDelegate) CentralManagerWillRestoreState(cmgr cbgo.CentralManager, opts cbgo.CentralManagerRestoreOpts) {
	if o := d.getOriginal(); o != nil {
		o.CentralManagerWillRestoreState(cmgr, opts)
	}
}
