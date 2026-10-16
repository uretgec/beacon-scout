// Hand-written placeholder so the frontend compiles before `wails3 generate bindings`
// is run on the developer's machine. The real file is regenerated automatically by
// Wails into this directory (git-ignored via `/bindings`).
// Field set mirrors FilterOptions in beaconstore.go — keep in sync!
export interface FilterOptions {
  minRssi: number;
  maxRssi: number;
  nameContains: string;
  nameExact: string;
  macContains: string;
  uuidContains: string;
  companyId: string;
  beaconType: string;
  hasName: boolean;
  activeOnly: boolean;
  activeWindow: number;
  adTypes?: number[];
  majorExact?: number;
  minorExact?: number;
  eddystoneUrlContains?: string;
  namespaceHex?: string;
  instanceHex?: string;
  connectable?: string;
  addressRandom?: string;
  rssiTrend?: string;
  deviceType?: string;
  rawContains?: string;
  serviceUuidExact?: string;
  namePrefix?: string;
  nameSuffix?: string;
  minTxPower?: number;
  maxTxPower?: number;
  minHits?: number;
  [key: string]: unknown; // tolerate fields added on the Go side after regeneration
}

async function call(_method: string, ..._args: unknown[]): Promise<unknown> {
  throw new Error("Bindings not generated yet. Run: wails3 generate bindings");
}

export const ScanService = {
  Status: (): Promise<boolean> => call("Status") as Promise<boolean>,
  StartScan: (): Promise<boolean> => call("StartScan") as Promise<boolean>,
  StopScan: (): Promise<void> => call("StopScan") as Promise<void>,
  SetFilter: (f: FilterOptions): Promise<number> => call("SetFilter", f) as Promise<number>,
  SetSort: (sortBy: string, dir: string): Promise<void> => call("SetSort", sortBy, dir) as Promise<void>,
  GetResults: (): Promise<Record<string, unknown>[]> => call("GetResults") as Promise<Record<string, unknown>[]>,
  ClearResults: (): Promise<void> => call("ClearResults") as Promise<void>,
  SetRefreshIntervalMs: (ms: number): Promise<void> => call("SetRefreshIntervalMs", ms) as Promise<void>,
  GetRefreshIntervalMs: (): Promise<number> => call("GetRefreshIntervalMs") as Promise<number>,
  EnableAdapter: (): Promise<void> => call("EnableAdapter") as Promise<void>,
  EnablingAdapter: (): Promise<boolean> => call("EnablingAdapter") as Promise<boolean>,
  AdTypes: (): Promise<Record<string, unknown>[]> => call("AdTypes") as Promise<Record<string, unknown>[]>,
};
