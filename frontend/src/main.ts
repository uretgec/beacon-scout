import { Events } from "@wailsio/runtime";
// Binding path mirrors the Go module name: github.com/example/beacon-scout.
// (Wails writes generated bindings to frontend/bindings/<module-path>/.)
import { ScanService } from "../bindings/github.com/example/beacon-scout";
import type { FilterOptions } from "../bindings/github.com/example/beacon-scout";

/* ---------- Types (mirror of the Go structs) ---------- */
interface ADStructure { type: number; typeName: string; hex: string; ascii?: string; decoded: string; }
interface ParsedAdv { structures: ADStructure[]; summary: string[]; }
interface BeaconRecord {
  address: string; isRandom: boolean; name: string;
  rssi: number; maxRssi: number; minRssi: number; avgRssi: number;
  txPower?: number; deviceType?: string; firstSeen: string; lastSeen: string; count: number;
  serviceUuids: string[]; companyIds: number[]; advTypes: string[];
  manufacturerData: Record<string, string>; serviceData: Record<string, string>;
  raw: string; rawAscii?: string; rawRebuilt?: boolean; beaconType: string; rssiTrend?: string;
  parsed?: ParsedAdv;
  connectable?: boolean; connectableKnown?: boolean;
  advSid?: number; periodicAdvInterval?: number; directRssi?: number;
  ibeaconUuid?: string; ibeaconMajor?: number; ibeaconMinor?: number;
  eddystoneNamespace?: string; eddystoneInstance?: string;
  eddystoneUrl?: string; googleBeaconId?: string;
}
interface ListPayload {
  scanning: boolean; items: BeaconRecord[]; phase?: string;
  // Backend fields: total = records currently held in the store,
  // matches = records passing the current filter,
  // seen = unique devices captured since launch (or last Clear).
  total?: number; matches?: number; seen?: number;
}

/* ---------- DOM helpers ---------- */
const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T;
const $input = (id: string) => $(id) as unknown as HTMLInputElement;
const $select = (id: string) => $(id) as unknown as HTMLSelectElement;
const tbody = $("rows");
const errorToast = $("errorToast");
let toastTimer: ReturnType<typeof setTimeout>;

function showError(msg: string) {
  errorToast.textContent = msg;
  errorToast.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => errorToast.classList.remove("show"), 6000);
}

function esc(s: string): string {
  return s.replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c] as string));
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString(undefined, { hour12: false });
}

function agoText(iso: string): string {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 1) return "now";
  if (s < 60) return `${s.toFixed(0)}s ago`;
  if (s < 3600) return `${(s / 60).toFixed(0)}m ago`;
  return `${(s / 3600).toFixed(1)}h ago`;
}

function rssiBars(rssi: number): string {
  // -30 dBm or better = full, -95 dBm or worse = empty
  const q = Math.min(4, Math.max(0, Math.round((rssi + 95) / 16)));
  return "▁".repeat(q) || "·";
}

function trendArrow(t?: string): string {
  if (t === "up") return ' <span style="color:#22c55e">↑</span>';
  if (t === "down") return ' <span style="color:#ef4444">↓</span>';
  if (t === "flat") return ' <span class="muted">→</span>';
  return "";
}

function badgeClass(t: string): string {
  if (t.startsWith("iBeacon")) return "badge ibeacon";
  if (t.startsWith("Eddystone")) return "badge eddystone";
  if (t.startsWith("AltBeacon")) return "badge altbeacon";
  return "badge";
}

/* ---------- Rendering ---------- */
let lastItems: BeaconRecord[] = [];
let expanded: string | null = null;

// Full Bluetooth Core Spec AD-structure breakdown rendered as a table:
// every Type-Length-Value record with its spec name, hex bytes and decoded value.
function adStructuresTable(parsed?: ParsedAdv): string {
  if (!parsed?.structures?.length) return "";
  const rows = parsed.structures.map(s => `<tr>
      <td><span class="hex">0x${s.type.toString(16).padStart(2, "0").toUpperCase()}</span></td>
      <td>${esc(s.typeName)}</td>
      <td><span class="hex">${esc(s.hex)}</span></td>
      <td>${esc(s.decoded || "—")}</td>
    </tr>`).join("");
  const summary = parsed.summary?.length
    ? `<tr><td colspan="4" class="grp-hdr">Detected formats: ${parsed.summary.map(esc).join(" · ")}</td></tr>` : "";
  return `<table class="ad-table"><thead><tr><th>Type</th><th>AD structure (Core Spec)</th><th>Bytes (hex)</th><th>Decoded</th></tr></thead><tbody>${summary}${rows}</tbody></table>`;
}

// One section of the device-detail table: a colored <thead> band followed by
// key/value rows in tbody — used for Identity / RSSI & Timing / Beacon data /
// Raw payload so the whole detail panel is one consistent table structure.
function detailSection(title: string, cls: string, pairs: [string, string][]): string {
  if (!pairs.length) return "";
  const body = pairs.map(([k, v]) =>
    `<tr><td class="dkey">${esc(k)}</td><td>${v}</td></tr>`).join("");
  return `<thead class="${cls}"><tr><th colspan="2">${title}</th></tr></thead><tbody>${body}</tbody>`;
}

// Classic hex-dump of the complete raw payload: offset | 16 hex bytes | ASCII.
function hexDump(rawHex: string): string {
  const clean = (rawHex || "").replace(/[^0-9a-fA-F]/g, "");
  if (!clean) return "";
  const bytes: number[] = [];
  for (let i = 0; i + 2 <= clean.length; i += 2) bytes.push(parseInt(clean.slice(i, i + 2), 16));
  const lines: string[] = [];
  for (let off = 0; off < bytes.length; off += 16) {
    const chunk = bytes.slice(off, off + 16);
    const hexPart = chunk.map(b => b.toString(16).toUpperCase().padStart(2, "0")).join(" ").padEnd(47, " ");
    const asciiPart = chunk.map(b => (b >= 0x20 && b <= 0x7e) ? String.fromCharCode(b) : ".").join("");
    lines.push(`${off.toString(16).padStart(4, "0")}  ${hexPart}  |${asciiPart}|`);
  }
  return `<pre class="hexdump">${esc(lines.join("\n"))}</pre>`;
}

// Compact Bluetooth SIG assigned-company table (mirrors adparser.go).
const COMPANY_NAMES: Record<number, string> = {
  0x0000: "Ericsson", 0x0001: "Nokia", 0x0002: "Intel", 0x0003: "Broadcom",
  0x0006: "Microsoft", 0x000D: "Texas Instruments", 0x000E: "AT&T",
  0x004C: "Apple", 0x0059: "Nordic Semiconductor", 0x0060: "STMicroelectronics",
  0x00E0: "Google", 0x0104: "Philips Lighting (Signify)",
  0x0118: "AltBeacon Consortium", 0x0159: "NRFI (Estimote)", 0x01A7: "Samsung",
  0x025D: "Huawei", 0x02D0: "Xiaomi", 0x038F: "Meta Platforms",
  0x05A7: "Kontakt.io", 0x0688: "HP", 0x0765: "Amazon",
};
function companyName(cid: number): string {
  if (!Number.isFinite(cid)) return "Unknown";
  return COMPANY_NAMES[cid] ?? `Unknown company`;
}

// Decode the Flags AD structure (type 0x01) straight from the raw hex payload.
// Core Spec Vol 3 Part C §2.3.1 — little scan over the TLV list.
function decodeFlagsHex(rawHex: string): string {
  const clean = (rawHex || "").replace(/[^0-9a-fA-F]/g, "");
  const bytes: number[] = [];
  for (let i = 0; i + 2 <= clean.length; i += 2) bytes.push(parseInt(clean.slice(i, i + 2), 16));
  for (let off = 0; off + 1 < bytes.length; ) {
    const len = bytes[off];
    if (len === 0 || off + 1 + len > bytes.length) break;
    const type = bytes[off + 1];
    if (type === 0x01 && len >= 2) {
      const f = bytes[off + 2];
      const parts: string[] = [];
      if (f & 0x01) parts.push("LE Restricted (no BR/EDR in this device)");
      else if (f & 0x02) parts.push("LE General Discoverable");
      else if (f & 0x04) parts.push("BR/EDR Not Supported");
      if (f & 0x08) parts.push("BT+LE Controller");
      if (f & 0x10) parts.push("Reserved for future use");
      return parts.length ? `${parts.join(" · ")} (0x${f.toString(16).toUpperCase().padStart(2, "0")})` : `0x${f.toString(16).toUpperCase().padStart(2, "0")} (no flags set)`;
    }
    off += 1 + len;
  }
  return "";
}

function detailCells(b: BeaconRecord): string {
  // --- Identity ----------------------------------------------------------
  const identity: [string, string][] = [
    ["Address", `<span class="hex">${esc(b.address)}</span> (${b.isRandom ? "Random" : "Public"})`],
    ["Name", b.name ? esc(b.name) : '<span class="muted">(unnamed)</span>'],
    ["Beacon type", `<span class="${badgeClass(b.beaconType)}">${esc(b.beaconType)}</span>`],
  ];
  if (b.deviceType) identity.push(["Device Type", esc(b.deviceType)]);
  // Advertisement Type: BLE AD type name from the parsed structures, plus
  // whether this was an ADV_IND / scan response style transmission.
  const advTypeName = b.advTypes?.[0] ?? "";
  identity.push(["Advertisement Type", advTypeName ? esc(advTypeName) : '<span class="muted">—</span>']);
  // Flags decoded from AD type 0x01 (Core Spec Vol 3, Part C, §2.3.1).
  const flags = decodeFlagsHex(b.raw);
  identity.push(["Flags", flags ? esc(flags) : '<span class="muted">No Flags AD (0x01) in payload</span>']);
  // Local Name (full or shortened, per AD types 0x09 / 0x08).
  identity.push(["Local Name", b.name ? esc(b.name) : '<span class="muted">(unnamed)</span>']);
  if (b.serviceUuids?.length) identity.push(["Service UUIDs", `<span class="hex">${esc(b.serviceUuids.join(", "))}</span>`]);
  if (b.companyIds?.length) {
    identity.push(["Company", b.companyIds.map(c => `0x${c.toString(16).padStart(4, "0").toUpperCase()} — ${esc(companyName(c))}`).join(", ")]);
  }
  if (b.advTypes?.length > 1) identity.push(["All AD types present", esc(b.advTypes.join(", "))]);
  if (b.txPower !== undefined && b.txPower !== null) identity.push(["Tx Power", `${b.txPower} dBm`]);
  if (b.connectableKnown) identity.push(["Connectability", b.connectable ? "Connectable (scannable)" : "Non-connectable"]);
  if (b.advSid !== undefined && b.advSid >= 0) identity.push(["Advertising SID", String(b.advSid)]);
  if (b.periodicAdvInterval) identity.push(["Periodic interval", `${(b.periodicAdvInterval * 1.25).toFixed(2)} ms`]);
  if (b.directRssi) identity.push(["Direct RSSI", `${b.directRssi} dBm`]);

  // --- RSSI & Timing -----------------------------------------------------
  const timing: [string, string][] = [
    ["RSSI (last)", `${b.rssi} dBm ${rssiBars(b.rssi)}`],
    ["RSSI min / max", `${b.minRssi} / ${b.maxRssi} dBm`],
    ["RSSI avg", b.avgRssi.toFixed(1) + " dBm"],
    ["First seen", fmtTime(b.firstSeen)],
    ["Last seen", `${fmtTime(b.lastSeen)} (${agoText(b.lastSeen)})`],
    ["Advert count", String(b.count)],
  ];

  // --- Beacon data (decoded specifics) ------------------------------------
  const beacon: [string, string][] = [];
  for (const [cid, data] of Object.entries(b.manufacturerData ?? {})) {
    beacon.push([`Manufacturer Data (${companyName(parseInt(cid, 16))})`, `<span class="hex">${esc(data)}</span>`]);
  }
  for (const [uuid, data] of Object.entries(b.serviceData ?? {})) {
    beacon.push([`Svc data ${uuid}`, `<span class="hex">${esc(data)}</span>`]);
  }
  if (b.ibeaconUuid) {
    beacon.push(["iBeacon UUID", `<span class="hex">${esc(b.ibeaconUuid)}</span>`]);
    beacon.push(["iBeacon Major/Minor", `${b.ibeaconMajor ?? 0} / ${b.ibeaconMinor ?? 0}`]);
  }
  if (b.eddystoneNamespace) beacon.push(["Eddystone Namespace", `<span class="hex">${esc(b.eddystoneNamespace)}</span>`]);
  if (b.eddystoneInstance) beacon.push(["Eddystone Instance", `<span class="hex">${esc(b.eddystoneInstance)}</span>`]);
  if (b.eddystoneUrl) beacon.push(["Eddystone URL", esc(b.eddystoneUrl)]);
  if (b.googleBeaconId) beacon.push(["Google Beacon ID", `<span class="hex">${esc(b.googleBeaconId)}</span>`]);

  // --- Raw payload ---------------------------------------------------------
  // On macOS CoreBluetooth does not expose the on-air bytes; the app then
  // reconstructs the FULL advertisement image (ADV_IND + Scan Response merged)
  // from every field the OS reports, so this hex is a complete AD-structure
  // stream — exactly what the parsed tables above were decoded from.
  const raw: [string, string][] = [
    ["Full Advertisement (hex)", `<span class="hex">${esc(b.raw)}</span>`],
    ["Source", b.rawRebuilt
      ? "reconstructed from all CB fields (Flags · Name · UUIDs · Tx Power · Appearance · Mfr · Service Data)"
      : "on-air bytes (captured verbatim)"],
  ];

  return `<table class="ad-table detail-table">`
       + `<thead class="sec-actions"><tr><th colspan="2">`
       +   `<button type="button" class="btn btn-sm copy-btn" data-copy="table">Copy Table</button> `
       +   `<button type="button" class="btn btn-sm copy-btn" data-copy="json">Copy JSON</button>`
       + `</th></tr></thead>`
       + detailSection("Identity", "sec-id", identity)
       + detailSection("RSSI &amp; Timing", "sec-rssi", timing)
       + (beacon.length ? detailSection("Beacon data", "sec-beacon", beacon) : "")
       + detailSection("Raw payload", "sec-raw", raw)
       + `</table>`
       + hexDump(b.raw)
       + adStructuresTable(b.parsed);
}

function render(items: BeaconRecord[], scanning: boolean, total: number) {
  lastItems = items;
  // "shown"   = rows currently visible after filters
  // "seen"    = unique devices captured by the scanner since launch/Clear
  // "matching"= devices passing the active filter (backend-computed)
  lastMatchTotal = total;
  const matching = total === items.length ? "" : ` · ${total} matching`;
  $("countInfo").textContent = `${items.length} shown${matching} / ${parseSeen()} seen`;
  // Keep the local scan state in sync with the backend's authoritative flag.
  if (scanning !== scanningNow) setScanUI(scanning);

  if (!items.length) {
    tbody.innerHTML = `<tr><td colspan="8" class="empty-msg">${scanning ? "Scanning… no beacons match yet." : "No beacons yet. Press “Start Scan”."}</td></tr>`;
    return;
  }
  const now = Date.now();
  tbody.innerHTML = items.map(b => {
    const stale = (now - new Date(b.lastSeen).getTime()) > 10000;
    const main = `<tr class="row${stale ? " stale" : ""}" data-addr="${esc(b.address)}">
      <td><span class="rssi-bar"><span class="rssi-val">${b.rssi}</span>${trendArrow(b.rssiTrend)}<span class="muted">${rssiBars(b.rssi)}</span></span></td>
      <td>${b.avgRssi.toFixed(1)}</td>
      <td>${b.name ? esc(b.name) : '<span class="muted">(unnamed)</span>'}</td>
      <td><span class="hex">${esc(b.address)}</span>${b.isRandom ? ' <span class="muted">rnd</span>' : ""}</td>
      <td><span class="${badgeClass(b.beaconType)}">${esc(b.beaconType)}</span></td>
      <td title="${fmtTime(b.lastSeen)}">${agoText(b.lastSeen)}</td>
      <td>${fmtTime(b.firstSeen)}</td>
      <td>${b.count}</td>
    </tr>`;
    const det = expanded === b.address
      ? `<tr class="detail" data-json="${encodeURIComponent(JSON.stringify(b))}"><td colspan="8">${detailCells(b)}</td></tr>` : "";
    return main + det;
  }).join("");
}

/* ---------- Events from Go ---------- */
Events.On("beacons:list", (ev: any) => {
  const d: ListPayload | undefined = ev?.data;
  if (!d) return;
  // Self-heal a stuck "Enabling adapter…" button: every list push carries the
  // backend's authoritative phase. If the backend says we are past the enable
  // phase, clear the progress UI immediately — even if the StartScan promise
  // has not resolved yet.
  if (d.phase && d.phase !== "enabling" && enablingSince !== null) setEnablingUI(false);
  if (enablingSince === null && !!d.scanning !== scanningNow) setScanUI(!!d.scanning);
  if (typeof d.seen === "number") lastSeenTotal = d.seen;
  render(d.items ?? [], !!d.scanning, d.matches ?? d.total ?? 0);
});

// Backend pushes human-readable errors here (adapter failures, timeouts…)
Events.On("scan:error", (ev: any) => {
  const msg = typeof ev?.data === "string" ? ev.data : JSON.stringify(ev?.data);
  console.error("[scan:error]", msg);
  showError(msg);
});

// Safety net: any uncaught JS error shows up in the UI too.
window.addEventListener("error", (e) => console.error("[window.error]", e.message));
window.addEventListener("unhandledrejection", (e) => console.error("[unhandled rejection]", e.reason));

/* ---------- Sorting (click table headers) ---------- */
document.querySelectorAll<HTMLElement>("thead th[data-sort]").forEach(th => {
  th.style.cursor = "pointer";
  th.title = "Click to sort";
  th.addEventListener("click", async () => {
    const field = th.dataset.sort!;
    let dir: string;
    if (th.classList.contains("sorted")) {
      dir = th.dataset.dir === "desc" ? "asc" : "desc";
    } else {
      dir = ["name", "address"].includes(field) ? "asc" : "desc";
    }
    document.querySelectorAll<HTMLElement>("thead th[data-sort]").forEach(h => { delete h.dataset.dir; h.classList.remove("sorted"); });
    th.classList.add("sorted"); th.dataset.dir = dir;
    console.log(`[ui] sort clicked: field=${field} dir=${dir}`);
    try {
      await ScanService.SetSort(field, dir);
      const num = !["name", "address"].includes(field);
      const sorted = [...lastItems].sort((a: any, b: any) => {
        let cmp: number;
        if (num) cmp = (a[field] ?? 0) - (b[field] ?? 0);
        else cmp = String(a[field] ?? "").localeCompare(String(b[field] ?? ""));
        return dir === "asc" ? cmp : -cmp;
      });
      render(sorted, scanningNow, parseTotal());
    } catch (e) { showError(`Sort error: ${e}`); }
  });
});

/* ---------- Row expand/collapse ---------- */
tbody.addEventListener("click", (ev) => {
  const target = ev.target as HTMLElement;

  // Copy buttons inside an expanded detail row — handle before row toggle.
  const copyBtn = target.closest(".copy-btn") as HTMLButtonElement | null;
  if (copyBtn) {
    ev.stopPropagation();
    void handleCopy(copyBtn);
    return;
  }

  const tr = target.closest("tr.row");
  if (!tr) return;
  const addr = tr.getAttribute("data-addr");
  expanded = expanded === addr ? null : addr;
  render(lastItems, $("statusDot").classList.contains("on"), parseTotal());
});

// Copy the parsed detail table (label: value lines) or the full raw record JSON
// of the expanded device to the clipboard.
async function handleCopy(btn: HTMLButtonElement) {
  const detailRow = btn.closest("tr.detail");
  if (!detailRow) return;
  let text = "";
  try {
    if (btn.dataset.copy === "json") {
      const rec = JSON.parse(decodeURIComponent(detailRow.getAttribute("data-json") ?? "{}"));
      text = JSON.stringify(rec, null, 2);
    } else {
      const lines: string[] = [];
      detailRow.querySelectorAll("table.detail-table > thead").forEach((th) => {
        const title = th.querySelector("th")?.textContent?.trim();
        if (title && !th.classList.contains("sec-actions")) lines.push(`## ${title}`);
      });
      detailRow.querySelectorAll("table.detail-table > tbody > tr").forEach((tr) => {
        const k = tr.querySelector("td.dkey")?.textContent?.trim();
        const v = tr.children[1]?.textContent?.trim();
        if (k && v !== undefined) lines.push(`${k}:\t${v}`);
      });
      text = lines.join("\n");
    }
    await navigator.clipboard.writeText(text);
    const orig = btn.textContent ?? "";
    btn.textContent = "Copied ✓";
    btn.classList.add("copied");
    setTimeout(() => { btn.textContent = orig; btn.classList.remove("copied"); }, 1500);
  } catch (e) {
    console.error("[copy] failed:", e);
    showError(`Clipboard error: ${e}`);
  }
}
let lastSeenTotal = 0;      // authoritative "unique devices seen" from the backend payload
let lastMatchTotal = 0;     // backend-reported match count (kept for local re-renders)
function parseSeen(): number { return lastSeenTotal; }
function parseTotal(): number { return lastMatchTotal; }

/* ---------- Filters sidebar ---------- */
// Sidebar is open by default (see index.html: class="sidebar open").
// The Filters button doubles as the status indicator: green = open, grey = closed.
$("toggleFilters").addEventListener("click", () => {
  const open = $("filterPanel").classList.toggle("open");
  // Toggle-button state class drives the CSS colors (green when open).
  $("toggleFilters").classList.toggle("open", open);
});

// Company dropdown ↔ custom-hex input toggle
const companySelect = $select("fCompanySelect");
const companyInput = $input("fCompany");
companySelect?.addEventListener("change", () => {
  const manual = companySelect.value === "manual";
  companyInput.style.display = manual ? "" : "none";
  if (!manual) companyInput.value = "";
});

// Service-UUID dropdown ↔ custom input toggle
const svcSelect = $select("fServiceUuidSelect");
const svcInput = $input("fServiceUuid");
svcSelect?.addEventListener("change", () => {
  const manual = svcSelect.value === "manual";
  svcInput.style.display = manual ? "" : "none";
  if (!manual) svcInput.value = "";
});

// Fill the multi-select of AD types from the curated Bluetooth SIG catalog.
(async () => {
  try {
    const types = (await (ScanService as any).AdTypes()) as { code: number; name: string }[];
    if (Array.isArray(types) && types.length) {
      const sel = $select("fAdTypes");
      sel.innerHTML = types
        .map(t => `<option value="${t.code}">0x${t.code.toString(16).padStart(2, "0").toUpperCase()} — ${esc(t.name)}</option>`)
        .join("");
      sel.size = Math.min(10, types.length);
    }
  } catch (e) {
    console.warn("[ui] AdTypes() unavailable (bindings not regenerated?):", e);
  }
})();

// Reads every filter input (basic + advanced) and builds a complete FilterOptions
// object. The explicit `satisfies FilterOptions` annotation guarantees at compile
// time that the shape matches the generated bindings.
function currentFilter(): FilterOptions {
  const num = (id: string, dflt: number): number => {
    const v = parseInt($input(id).value, 10);
    return Number.isNaN(v) ? dflt : v;
  };
  const str = (id: string): string => ($input(id).value ?? "").trim();

  const adSel = document.getElementById("fAdTypes") as unknown as HTMLSelectElement | null;
  const adTypes: number[] = [];
  if (adSel && typeof adSel.selectedOptions === "object") {
    for (const opt of Array.from(adSel.selectedOptions)) {
      const code = parseInt(opt.value, 10);
      if (!Number.isNaN(code)) adTypes.push(code);
    }
  }

  // Company: dropdown wins unless "manual" with custom hex typed in.
  const companyId = companySelect?.value === "manual" ? str("fCompany") : (companySelect?.value ?? "");

  // Service UUID: dropdown wins unless "manual".
  const svcManual = svcSelect?.value === "manual";
  const serviceUuidExact = svcManual ? str("fServiceUuid") : (svcSelect?.value ?? "");

  return {
    minRssi: num("fMinRssi", -200),
    maxRssi: num("fMaxRssi", 0),
    nameContains: str("fName"),
    nameExact: str("fNameExact"),
    namePrefix: str("fNamePrefix"),
    nameSuffix: str("fNameSuffix"),
    macContains: str("fMac"),
    uuidContains: str("fUuid").replace(/-/g, ""),
    companyId,
    serviceUuidExact,
    rawContains: str("fRawContains").replace(/[^0-9a-fA-F]/g, ""),
    beaconType: ($select("fType").value || "any"),
    hasName: $input("fHasName").checked,
    activeOnly: $input("fActiveOnly").checked,
    activeWindow: num("fActiveWindow", 10),
    adTypes,
    majorExact: num("fMajor", 0),
    minorExact: num("fMinor", 0),
    eddystoneUrlContains: str("fEddUrl"),
    namespaceHex: str("fNamespace").replace(/[^0-9a-fA-F]/g, ""),
    instanceHex: str("fInstance").replace(/[^0-9a-fA-F]/g, ""),
    connectable: ($select("fConnectable")?.value ?? ""),
    addressRandom: ($select("fAddressRandom")?.value ?? ""),
    rssiTrend: ($select("fRssiTrend")?.value ?? ""),
    minTxPower: num("fMinTx", 0),
    maxTxPower: num("fMaxTx", 0),
    minHits: num("fMinHits", 0),
  } satisfies FilterOptions;
}

async function applyFilters() {
  const btn = $("applyFilters") as HTMLButtonElement;
  btn.disabled = true;
  const original = btn.textContent;
  btn.textContent = "Applying…";
  try {
    const matched = await ScanService.SetFilter(currentFilter());
    console.log("[ui] filter applied — backend reports %d matching records", matched);
    const items = (await ScanService.GetResults()) as unknown as BeaconRecord[];
    render(items, scanningNow, parseTotal());
    flashPanelFeedback(`${matched} match${matched === 1 ? "" : "es"}`);
  } catch (e) {
    console.error("[ui] SetFilter failed:", e);
    showError(`Filter error: ${e}`);
  } finally {
    btn.disabled = false;
    btn.textContent = original;
  }
}

// Brief visible confirmation inside the filter panel that Apply actually ran.
function flashPanelFeedback(text: string) {
  let el = $("filterFeedback");
  if (!el) {
    el = document.createElement("span");
    el.id = "filterFeedback";
    el.className = "filter-feedback";
    $("applyFilters").parentElement?.appendChild(el);
  }
  el.textContent = text;
  el.classList.add("show");
  setTimeout(() => el!.classList.remove("show"), 2500);
}
$("applyFilters").addEventListener("click", applyFilters);

// Filters sidebar — live apply: any change in a filter control re-runs the
// filter after 300 ms, so the table follows typing without pressing Apply.
{
  let t: ReturnType<typeof setTimeout> | null = null;
  const panel = $("filterPanel");
  panel.addEventListener("input", () => {
    if (t) clearTimeout(t);
    t = setTimeout(() => { void applyFilters(); }, 300);
  });
  panel.addEventListener("change", () => { void applyFilters(); });
}

$("resetFilters").addEventListener("click", () => {
  $input("fMinRssi").value = "-200";
  $input("fMaxRssi").value = "0";
  ["fName", "fMac", "fUuid", "fCompany", "fNameExact", "fNamePrefix", "fNameSuffix",
   "fRawContains", "fServiceUuid", "fEddUrl", "fNamespace", "fInstance"].forEach(id => $input(id).value = "");
  ["fCompanySelect", "fServiceUuidSelect", "fType", "fConnectable", "fAddressRandom", "fRssiTrend"]
    .forEach(id => $select(id).selectedIndex = 0);
  ["fMajor", "fMinTx", "fMaxTx", "fMinHits"].forEach(id => $input(id).value = "0");
  $select("fAdTypes").selectedIndex = -1;
  companyInput.style.display = "none";
  svcInput.style.display = "none";
  $input("fHasName").checked = false;
  $input("fActiveOnly").checked = true;
  applyFilters();
});

/* ---------- Auto-refresh (header control) ---------- */
// The list re-pulls results with the CURRENT filters every N ms. 0 = paused
// ("no refresh") — the scan keeps collecting in the background; Apply still
// forces an immediate refresh. Changing the interval takes effect instantly.
let refreshMs = 0; // OFF by default — user opts in via the header input
let refreshTimer: ReturnType<typeof setInterval> | null = null;

function updateRefreshHint() {
  $("refreshHint").textContent = refreshMs > 0 ? `every ${refreshMs >= 2000 ? (refreshMs / 1000) + "s" : refreshMs + "ms"}` : "paused";
}

async function refreshOnce() {
  try {
    const items = (await ScanService.GetResults()) as unknown as BeaconRecord[];
    // "seen" total comes from the live beacons:list pushes; keep last known.
    render(items, scanningNow, items.length);
  } catch (e) {
    console.error("[ui] auto-refresh failed:", e);
  }
}

function startRefreshLoop() {
  if (refreshTimer) clearInterval(refreshTimer);
  if (refreshMs > 0) {
    refreshTimer = setInterval(refreshOnce, refreshMs);
    refreshOnce(); // immediate feedback when the interval changes
  }
  updateRefreshHint();
}

const refreshInput = $input("refreshMs");
const refreshPreset = $select("refreshPreset");

function setRefreshInterval(ms: number) {
  refreshMs = Math.max(0, Math.floor(ms));
  refreshInput.value = String(refreshMs);
  // Sync preset dropdown when the value matches one of the presets.
  const match = Array.from(refreshPreset.options).find(o => Number(o.value) === refreshMs);
  if (match) refreshPreset.value = match.value;
  startRefreshLoop();
  // Mirror to the backend so its event throttle uses the same cadence.
  ScanService.SetRefreshIntervalMs(refreshMs).catch(e => console.warn("[ui] SetRefreshIntervalMs failed:", e));
}

refreshInput.addEventListener("change", () => setRefreshInterval(parseInt(refreshInput.value, 10) || 0));
refreshPreset.addEventListener("change", () => setRefreshInterval(parseInt(refreshPreset.value, 10)));

/* ---------- Scan control ---------- */
let scanningNow = false;
let enablingSince: number | null = null; // ms timestamp when adapter enable began
let enableTickTimer: ReturnType<typeof setInterval> | null = null;

// setEnablingUI is re-assigned further below to add a polling self-heal on top
// of the base implementation, so it must be a mutable binding (not const/function).
let setEnablingUI = function (on: boolean) {
  const toggle = $("toggleScan") as HTMLButtonElement;
  const status = $("statusText");
  if (on) {
    if (enablingSince === null) enablingSince = Date.now();
    toggle.disabled = true;
    toggle.textContent = "Enabling adapter…";
    toggle.className = "btn warning";
    status.textContent = "Enabling adapter…";
    if (enableTickTimer === null) {
      enableTickTimer = setInterval(() => {
        if (enablingSince === null) return;
        const secs = Math.floor((Date.now() - enablingSince) / 1000);
        toggle.textContent = `Enabling adapter… ${secs}s`;
        status.textContent =
          secs > 8
            ? "Waiting for Bluetooth permission… (check System Settings → Privacy & Security → Bluetooth)"
            : "Enabling adapter…";
      }, 1000);
    }
  } else {
    enablingSince = null;
    if (enableTickTimer !== null) { clearInterval(enableTickTimer); enableTickTimer = null; }
  }
};

function setScanUI(scanning: boolean) {
  scanningNow = scanning;
  const toggle = $("toggleScan") as HTMLButtonElement;
  // Backend confirmed we left the enabling phase — clear any stuck progress UI.
  if (scanning || enablingSince !== null) setEnablingUI(false);
  toggle.textContent = scanning ? "Stop Scan" : "Start Scan";
  toggle.className = "btn " + (scanning ? "danger" : "primary");
  toggle.disabled = false;
  $("statusDot").className = "status-dot" + (scanning ? " on" : "");
  $("statusText").textContent = scanning ? "Scanning…" : "Idle";
}

// Authoritative backend state pushes — this is what keeps the button in sync
// even when StartScan() takes 25 s to resolve (permission dialog etc.).
Events.On("scan:state", (e: any) => {
  const d = e?.data;
  const phase = typeof d === "string" ? d : d?.phase;
  console.log("[ui] scan:state ->", phase);
  if (phase === "enabling") setEnablingUI(true);
  else if (phase === "scanning") { setEnablingUI(false); setScanUI(true); }
  else if (phase === "idle") { setEnablingUI(false); setScanUI(false); }
});

// Belt-and-braces self-heal for a stuck "Enabling adapter…" label. The whole
// enabling flow can also fail silently on the frontend side: if the
// `scan:state`/`beacons:list` events are missed (bindings not regenerated,
// event delivered before listener attached, webview IPC hiccup), the counting
// timer here would keep ticking forever even though the backend long ago
// entered scanning mode. So while the enabling UI is up, poll the backend's
// authoritative flags directly every second and flip the button as soon as
// either says we are past the enable phase. Cheap (two local method calls)
// and stops automatically once the state resolves.
let enableHealTimer: ReturnType<typeof setInterval> | null = null;
const origSetEnablingUI = setEnablingUI;
setEnablingUI = function (on: boolean) {
  origSetEnablingUI(on);
  if (on && enableHealTimer === null) {
    enableHealTimer = setInterval(async () => {
      if (enablingSince === null) {
        if (enableHealTimer !== null) { clearInterval(enableHealTimer); enableHealTimer = null; }
        return;
      }
      try {
        const [scanning, enabling] = await Promise.all([
          ScanService.Status(),
          ScanService.EnablingAdapter(),
        ]);
        if (scanning || !enabling) {
          console.log(`[ui] enabling self-heal: Status=${scanning} EnablingAdapter=${enabling} → flipping button`);
          setEnablingUI(false);
          setScanUI(!!scanning);
        }
      } catch { /* backend busy / bindings missing — retry next tick */ }
    }, 1000);
  } else if (!on && enableHealTimer !== null) {
    clearInterval(enableHealTimer);
    enableHealTimer = null;
  }
};

$("toggleScan").addEventListener("click", async () => {
  const btn = $("toggleScan") as HTMLButtonElement;
  const wasScanning = scanningNow;
  console.log(`[ui] ${wasScanning ? "StopScan" : "StartScan"} clicked (local state: scanning=${wasScanning})`);
  try {
    if (wasScanning) {
      btn.disabled = true;
      btn.textContent = "Stopping…";
      $("statusText").textContent = "Stopping…";
      await ScanService.StopScan();
      setScanUI(false);
      // Scanning stopped — there is nothing new to poll, so force the
      // auto-refresh interval back to OFF (0) on both sides.
      if (refreshMs !== 0) setRefreshInterval(0);
    } else {
      // Optimistically enter the enabling state; the backend also pushes
      // "scan:state" events so the label stays live even if this promise
      // blocks for up to ~25 s waiting for the macOS permission dialog.
      setEnablingUI(true);
      const nowScanning = await ScanService.StartScan();
      setEnablingUI(false);
      setScanUI(!!nowScanning);
      // Scanning started — turn the auto-refresh ON at the default 2000 ms
      // (Stop Scan switches it back to OFF). If the user manually picked a
      // different interval before starting, keep their choice.
      if (nowScanning && refreshMs === 0) setRefreshInterval(2000);
    }
  } catch (e) {
    console.error(`[ui] ${wasScanning ? "StopScan" : "StartScan"} failed:`, e);
    showError(`Bluetooth error: ${e}`);
    setEnablingUI(false);
    try { setScanUI(await ScanService.Status()); } catch { setScanUI(false); }
  } finally {
    btn.disabled = false;
  }
});

$("clear").addEventListener("click", async () => {
  const btn = $("clear") as HTMLButtonElement;
  console.log("[ui] Clear clicked");
  btn.disabled = true;
  try {
    await ScanService.ClearResults();
    expanded = null;
    lastItems = [];
    lastSeenTotal = 0; lastMatchTotal = 0;
    render([], scanningNow, 0);
    $("countInfo").textContent = "0 shown / 0 seen";
    flashButtonFeedback(btn, "Cleared");
  } catch (e) {
    console.error("[ui] ClearResults failed:", e);
    showError(`Clear error: ${e}`);
  } finally {
    btn.disabled = false;
  }
});

// Short label swap on a button as visible confirmation it ran.
function flashButtonFeedback(btn: HTMLButtonElement, text: string) {
  const original = btn.textContent;
  btn.textContent = text;
  setTimeout(() => { btn.textContent = original; }, 1200);
}

/* Initial state sync — adopt the backend's real state instead of assuming Idle. */
(async () => {
  try {
    const scanning = await ScanService.Status();
    console.log("[ui] backend reachable, Status() =", scanning);
    setScanUI(!!scanning);
  } catch (e) {
    console.warn("[ui] backend not reachable yet:", e);
    showError("Backend bindings not ready. If you cloned fresh, run: wails3 generate bindings");
  }
  // Refresh is always OFF (0) on startup, per spec — do NOT adopt a stale
  // backend value; force both sides to 0 and sync the preset dropdown.
  setRefreshInterval(0);
})();
