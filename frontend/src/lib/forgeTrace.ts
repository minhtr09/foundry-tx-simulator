import type { SimulateResponse } from "../api/types";
import type { TraceAddressLabel, TraceNode } from "./traceTypes";

type ForgeTraceEntry = {
  arena: ForgeArenaNode[];
};

type ForgeArenaNode = {
  children: number[];
  idx: number;
  logs: unknown[];
  ordering: unknown[];
  parent: number | null;
  trace: ForgeCallTrace;
};

type ForgeCallTrace = {
  address: string;
  caller: string;
  data: string;
  decoded?: ForgeDecoded;
  depth: number;
  gasLimit?: number;
  gasUsed?: number;
  kind: string;
  output: string;
  status: string;
  success: boolean;
  value: string;
};

type ForgeDecoded = {
  label: string;
  returnData?: unknown;
  callData?: {
    args: unknown[];
    signature: string;
  };
};

type WorkNode = {
  children: WorkNode[];
  item: TraceNode;
};

type OrderedEntry = {
  callID?: number;
  logIndex?: number;
};

type TraceData = {
  labels: TraceAddressLabel[];
  nodes: TraceNode[];
};

export function traceDataFromResponse(response: SimulateResponse | null | undefined): TraceData {
  if (!response) {
    return { labels: [], nodes: [] };
  }
  return parseForgeTrace(response.trace);
}

export function parseForgeTrace(trace: string): TraceData {
  try {
    const payload = JSON.parse(trace);
    const record = readRecord(payload);
    const labels = new Map<string, string>();
    mergeLabels(labels, normalizeLabels(record?.labeled_addresses));
    const traces = readArray(record?.traces).flatMap(parseTraceEntry);
    for (const entry of traces) {
      collectArenaLabels(entry.arena, labels);
    }
    return {
      labels: mapToTraceLabels(labels),
      nodes: traces.flatMap((entry) => traceEntryToNodes(entry, labels))
    };
  } catch {
    return { labels: [], nodes: [] };
  }
}

function parseTraceEntry(value: unknown): ForgeTraceEntry[] {
  if (!Array.isArray(value) || value.length !== 2) {
    return [];
  }
  const body = readRecord(value[1]);
  const arena = readArray(body?.arena).map(parseArenaNode);
  if (arena.length === 0) {
    return [];
  }
  return [{ arena }];
}

function parseArenaNode(value: unknown, index: number): ForgeArenaNode {
  const record = readRecord(value);
  const trace = readRecord(record?.trace);
  const decoded = readRecord(trace?.decoded);
  const callData = readRecord(decoded?.call_data);
  return {
    children: readIntegerArray(record?.children),
    idx: readInteger(record?.idx) ?? index,
    logs: readArray(record?.logs),
    ordering: readArray(record?.ordering),
    parent: readParent(record?.parent),
    trace: {
      address: readString(trace?.address),
      caller: readString(trace?.caller),
      data: readString(trace?.data),
      decoded: decoded
        ? {
            label: readString(decoded.label),
            returnData: decoded.return_data,
            callData: callData
              ? {
                  args: readArray(callData.args),
                  signature: readString(callData.signature)
                }
              : undefined
          }
        : undefined,
      depth: readInteger(trace?.depth) ?? 0,
      gasLimit: readUint(trace?.gas_limit),
      gasUsed: readUint(trace?.gas_used),
      kind: readString(trace?.kind),
      output: readString(trace?.output),
      status: readString(trace?.status),
      success: readBool(trace?.success),
      value: readCallValue(trace?.value)
    }
  };
}

function traceEntryToNodes(entry: ForgeTraceEntry, labels: Map<string, string>): TraceNode[] {
  const nodesByID = new Map<number, WorkNode>();
  for (const item of entry.arena) {
    nodesByID.set(item.idx, { item: arenaNodeToTraceNode(item, labels), children: [] });
  }

  for (const item of entry.arena) {
    const node = nodesByID.get(item.idx);
    if (node) {
      node.children = orderedChildren(item, nodesByID);
    }
  }

  const roots = entry.arena.flatMap((item) => (item.parent === null ? nodesByID.get(item.idx) ?? [] : []));
  if (roots.length === 0 && entry.arena[0]) {
    const fallbackRoot = nodesByID.get(entry.arena[0].idx);
    if (fallbackRoot) {
      roots.push(fallbackRoot);
    }
  }
  return roots.map(workNodeToTraceNode);
}

function orderedChildren(item: ForgeArenaNode, nodesByID: Map<number, WorkNode>): WorkNode[] {
  if (item.children.length === 0 && item.logs.length === 0) {
    const result = resultNode(item.trace);
    return result ? [result] : [];
  }

  const out: WorkNode[] = [];
  const seenCalls = new Set<number>();
  const seenLogs = new Set<number>();
  for (const entry of orderedEntries(item)) {
    if (entry.callID !== undefined) {
      const child = nodesByID.get(entry.callID);
      if (child && !seenCalls.has(entry.callID)) {
        seenCalls.add(entry.callID);
        out.push(child);
      }
    }
    if (entry.logIndex !== undefined && entry.logIndex >= 0 && entry.logIndex < item.logs.length && !seenLogs.has(entry.logIndex)) {
      seenLogs.add(entry.logIndex);
      out.push(logNode(item.logs[entry.logIndex]));
    }
  }

  for (const childID of item.children) {
    const child = nodesByID.get(childID);
    if (child && !seenCalls.has(childID)) {
      seenCalls.add(childID);
      out.push(child);
    }
  }
  for (const [index, log] of item.logs.entries()) {
    if (!seenLogs.has(index)) {
      out.push(logNode(log));
    }
  }
  const result = resultNode(item.trace);
  if (result) {
    out.push(result);
  }
  return out;
}

function orderedEntries(item: ForgeArenaNode): OrderedEntry[] {
  return item.ordering.flatMap((raw) => {
    const record = readRecord(raw);
    if (!record) {
      return [];
    }
    const entries: OrderedEntry[] = [];
    const callIndex = readInteger(record.Call);
    if (callIndex !== undefined) {
      const callID = callIDFromOrdering(callIndex, item.children);
      if (callID !== undefined) {
        entries.push({ callID });
      }
    }
    const logIndex = readInteger(record.Log);
    if (logIndex !== undefined) {
      entries.push({ logIndex });
    }
    return entries;
  });
}

function callIDFromOrdering(value: number, children: number[]): number | undefined {
  if (value >= 0 && value < children.length) {
    return children[value];
  }
  return children.includes(value) ? value : undefined;
}

function arenaNodeToTraceNode(item: ForgeArenaNode, labels: Map<string, string>): TraceNode {
  const trace = item.trace;
  const target = traceTarget(trace);
  const targetLabel = traceTargetLabel(trace, labels);
  const decodedSignature = traceFunctionSignature(trace);
  const isFallback = isFallbackSignature(decodedSignature);
  const functionSignature = isFallback ? "" : decodedSignature;
  const fn = traceFunction(trace, decodedSignature);
  const args = traceArguments(trace);
  const callType = trace.kind.trim().toLowerCase();
  return {
    raw: traceRaw(target, targetLabel, fn, args, callType, trace),
    kind: "call",
    gas: trace.gasUsed,
    parent: item.parent,
    target,
    targetLabel,
    function: fn,
    functionSignature: functionSignature || undefined,
    selector: isFallback ? undefined : traceSelector(trace),
    arguments: args || undefined,
    callType: callType || undefined,
    callValue: traceCallValue(trace) || undefined,
    resultType: trace.status || undefined,
    value: traceValue(trace) || undefined
  };
}

function logNode(raw: unknown): WorkNode {
  const value = formatLogValue(raw);
  return {
    children: [],
    item: {
      raw: value,
      kind: "event",
      value,
      gas: undefined,
      resultType: undefined,
      target: undefined,
      function: undefined,
      arguments: undefined,
      callType: undefined
    }
  };
}

function resultNode(trace: ForgeCallTrace): WorkNode | undefined {
  const status = trace.status.trim();
  const value = traceValue(trace);
  if (trace.success || !status || !value || status === "Return" || status === "Stop") {
    return undefined;
  }
  return {
    children: [],
    item: {
      raw: `← [${status}] ${value.replace(new RegExp(`^${escapeRegExp(status)}\\s*`), "")}`.trim(),
      kind: status.toLowerCase(),
      resultType: status,
      value
    }
  };
}

function workNodeToTraceNode(node: WorkNode): TraceNode {
  const item: TraceNode = { ...node.item };
  if (node.children.length > 0) {
    item.children = node.children.map(workNodeToTraceNode);
  }
  return item;
}

function traceTarget(trace: ForgeCallTrace): string {
  const address = trace.address.trim();
  return address || trace.caller.trim();
}

function traceTargetLabel(trace: ForgeCallTrace, labels: Map<string, string>): string | undefined {
  const address = trace.address.trim();
  if (!isAddress(address)) {
    return undefined;
  }
  return trace.decoded?.label.trim() || labels.get(address.toLowerCase()) || undefined;
}

function traceFunctionSignature(trace: ForgeCallTrace): string {
  return trace.decoded?.callData?.signature.trim() ?? "";
}

function traceFunction(trace: ForgeCallTrace, signature = traceFunctionSignature(trace)): string {
  if (signature) {
    const paren = signature.indexOf("(");
    return paren > 0 ? signature.slice(0, paren) : signature;
  }
  return trace.kind.trim().toLowerCase() || "call";
}

function traceSelector(trace: ForgeCallTrace): string | undefined {
  const data = trace.data.trim();
  if (!/^0x[0-9a-fA-F]{8}/.test(data)) {
    return undefined;
  }
  return data.slice(0, 10).toLowerCase();
}

function isFallbackSignature(signature: string): boolean {
  return signature.trim().toLowerCase() === "fallback()";
}

function traceArguments(trace: ForgeCallTrace): string {
  const args = trace.decoded?.callData?.args ?? [];
  if (args.length > 0) {
    return args.map(formatJSONValue).join(", ");
  }
  const data = trace.data.trim();
  return data && data !== "0x" ? data : "";
}

function traceValue(trace: ForgeCallTrace): string {
  const values = [trace.status.trim()].filter(Boolean);
  if (trace.output && trace.output !== "0x") {
    values.push(trace.output);
  }
  const returnData = trace.decoded?.returnData;
  if (returnData !== undefined) {
    const value = formatJSONValue(returnData);
    if (value && value !== "null") {
      values.push(value);
    }
  }
  return values.join(" ");
}

function traceCallValue(trace: ForgeCallTrace): string {
  const value = uintString(trace.value);
  return value === "0" ? "" : `${value} wei`;
}

function traceRaw(target: string, targetLabel: string | undefined, fn: string, args: string, callType: string, trace: ForgeCallTrace): string {
  const gas = trace.gasUsed && trace.gasUsed > 0 ? `[${trace.gasUsed}] ` : "";
  const value = traceCallValue(trace);
  const callValue = value ? ` {value: ${value}}` : "";
  const kind = callType ? ` [${callType}]` : "";
  const status = trace.status ? ` => ${trace.status}` : "";
  return `${gas}${targetLabel || target || "unknown"}::${fn || "call"}(${args})${callValue}${kind}${status}`;
}

function formatJSONValue(value: unknown): string {
  if (typeof value === "string") {
    return value;
  }
  if (value === null || value === undefined) {
    return "";
  }
  return JSON.stringify(value) ?? String(value);
}

function formatLogValue(value: unknown): string {
  const record = readRecord(value);
  const decoded = readRecord(record?.decoded);
  const name = readString(decoded?.name).trim();
  const params = readArray(decoded?.params).flatMap(formatDecodedParam);
  if (name) {
    return `${name}(${params.join(", ")})`;
  }
  return formatJSONValue(value);
}

function formatDecodedParam(value: unknown): string[] {
  if (!Array.isArray(value) || value.length < 2) {
    return [];
  }
  const name = readString(value[0]).trim();
  const paramValue = formatJSONValue(value[1]);
  if (!name) {
    return [paramValue];
  }
  return [`${name}: ${paramValue}`];
}

function normalizeLabels(value: unknown): Map<string, string> {
  const labels = new Map<string, string>();
  const record = readRecord(value);
  if (!record) {
    return labels;
  }
  for (const [key, rawValue] of Object.entries(record)) {
    const labelOrAddress = readString(rawValue).trim();
    const trimmedKey = key.trim();
    if (isAddress(trimmedKey) && labelOrAddress) {
      labels.set(trimmedKey.toLowerCase(), labelOrAddress);
    }
    if (isAddress(labelOrAddress) && trimmedKey) {
      labels.set(labelOrAddress.toLowerCase(), trimmedKey);
    }
  }
  return labels;
}

function mergeLabels(target: Map<string, string>, source: Map<string, string>) {
  for (const [address, label] of source) {
    if (!target.has(address)) {
      target.set(address, label);
    }
  }
}

function collectArenaLabels(arena: ForgeArenaNode[], labels: Map<string, string>) {
  for (const item of arena) {
    const address = item.trace.address.trim();
    const label = item.trace.decoded?.label.trim();
    if (isAddress(address) && label && !labels.has(address.toLowerCase())) {
      labels.set(address.toLowerCase(), label);
    }
  }
}

function mapToTraceLabels(labels: Map<string, string>): TraceAddressLabel[] {
  return Array.from(labels.entries()).flatMap(([address, label]) => (label.trim() ? [{ address, label }] : []));
}

function readRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return undefined;
  }
  return value as Record<string, unknown>;
}

function readArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function readIntegerArray(value: unknown): number[] {
  return readArray(value).flatMap((item) => {
    const parsed = readInteger(item);
    return parsed === undefined ? [] : [parsed];
  });
}

function readString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function readCallValue(value: unknown): string {
  if (typeof value === "number" && Number.isSafeInteger(value) && value >= 0) {
    return String(value);
  }
  if (typeof value === "string") {
    return value;
  }
  return "0";
}

function readBool(value: unknown): boolean {
  return typeof value === "boolean" ? value : false;
}

function readParent(value: unknown): number | null {
  return value === null ? null : readInteger(value) ?? null;
}

function readInteger(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isInteger(value)) {
    return value;
  }
  if (typeof value !== "string") {
    return undefined;
  }
  const parsed = Number.parseInt(value, 10);
  return Number.isInteger(parsed) ? parsed : undefined;
}

function readUint(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value;
  }
  if (typeof value !== "string") {
    return undefined;
  }
  const trimmed = value.trim();
  const parsed = trimmed.startsWith("0x") || trimmed.startsWith("0X") ? Number.parseInt(trimmed.slice(2), 16) : Number.parseInt(trimmed, 10);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function isAddress(value: string): boolean {
  return /^0x[0-9a-fA-F]{40}$/.test(value.trim());
}

function uintString(value: string): string {
  const trimmed = value.trim();
  if (!trimmed) {
    return "0";
  }
  try {
    if (/^0x[0-9a-fA-F]+$/.test(trimmed)) {
      return BigInt(trimmed).toString(10);
    }
    if (/^[0-9]+$/.test(trimmed)) {
      return BigInt(trimmed).toString(10);
    }
  } catch {
    return "0";
  }
  return "0";
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
