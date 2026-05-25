import { simulateRequestSchema } from "../api/schemas";
import { isAddress } from "../lib/labels";
import type {
  CompilerConfig,
  ERC20ApprovalOverride,
  ERC20BalanceOverride,
  ERC721ApprovalOverride,
  LabelOverride,
  SimulateRequest,
  StateOverride
} from "../api/types";

export type RequestTab = "overrides" | "state" | "compiler";
export type OutputView = "trace" | "flow" | "balances" | "json";
export type ExpandMode = "depth" | "expand" | "collapse";
export type HealthStatus = "offline" | "online" | "error";
export type ThemeMode = "light" | "dark";

export type FormState = {
  apiUrl: string;
  chain: string;
  blockNumber: string;
  projectPath: string;
  sender: string;
  target: string;
  data: string;
  labelOverrides: LabelOverride[];
  erc20BalanceOverrides: ERC20BalanceOverride[];
  erc20ApprovalOverrides: ERC20ApprovalOverride[];
  erc721ApprovalOverrides: ERC721ApprovalOverride[];
  stateContractName: string;
  stateSource: string;
  compilerUse: string;
  optimizerRuns: string;
  evmVersion: string;
  revertStrings: string;
  viaIR: boolean;
  optimize: boolean;
  offline: boolean;
  noMetadata: boolean;
  decodeInternal: boolean;
};

const defaultApiUrl = window.__TXSIM_CONFIG__?.apiUrl ?? "http://127.0.0.1:8080";

export const defaults: FormState = {
  apiUrl: defaultApiUrl,
  chain: "mainnet",
  blockNumber: "",
  projectPath: "",
  sender: "",
  target: "",
  data: "",
  labelOverrides: [],
  erc20BalanceOverrides: [],
  erc20ApprovalOverrides: [],
  erc721ApprovalOverrides: [],
  stateContractName: "",
  stateSource: "",
  compilerUse: "",
  optimizerRuns: "",
  evmVersion: "",
  revertStrings: "",
  viaIR: true,
  optimize: true,
  offline: false,
  noMetadata: false,
  decodeInternal: false
};

export type UpdateForm = <K extends keyof FormState>(key: K, value: FormState[K]) => void;

export function formFromRequest(request: SimulateRequest, apiUrl: string): FormState {
  const compiler = request.compiler ?? {};
  const stateSource = request.stateOverride?.source ?? "";
  const stateContractName = request.stateOverride?.contractName ?? "";
  return {
    ...defaults,
    apiUrl,
    chain: request.chain,
    blockNumber: request.blockNumber,
    projectPath: request.projectPath ?? "",
    sender: request.sender,
    target: request.target,
    data: request.data,
    labelOverrides: request.labelOverrides ?? [],
    erc20BalanceOverrides: request.erc20BalanceOverrides ?? [],
    erc20ApprovalOverrides: request.erc20ApprovalOverrides ?? [],
    erc721ApprovalOverrides: request.erc721ApprovalOverrides ?? [],
    stateContractName,
    stateSource,
    compilerUse: compiler.use ?? "",
    optimizerRuns: compiler.optimizerRuns === undefined ? "" : String(compiler.optimizerRuns),
    evmVersion: compiler.evmVersion ?? "",
    revertStrings: compiler.revertStrings ?? "",
    viaIR: compiler.viaIR ?? defaults.viaIR,
    optimize: compiler.optimize ?? defaults.optimize,
    offline: compiler.offline ?? defaults.offline,
    noMetadata: compiler.noMetadata ?? defaults.noMetadata,
    decodeInternal: request.decodeInternal ?? defaults.decodeInternal
  };
}

export function buildRequest(form: FormState): SimulateRequest {
  if (!form.blockNumber.trim()) {
    throw new Error("blockNumber is required");
  }
  if (!form.sender.trim()) {
    throw new Error("sender is required");
  }
  if (!form.target.trim()) {
    throw new Error("target is required");
  }

  const compiler: CompilerConfig = {
    viaIR: form.viaIR,
    optimize: form.optimize
  };
  optionalString(compiler, "use", form.compilerUse);
  optionalString(compiler, "evmVersion", form.evmVersion);
  optionalString(compiler, "revertStrings", form.revertStrings);
  if (form.offline) {
    compiler.offline = true;
  }
  if (form.noMetadata) {
    compiler.noMetadata = true;
  }
  if (form.optimizerRuns.trim()) {
    const runs = Number(form.optimizerRuns);
    if (!Number.isInteger(runs) || runs < 0) {
      throw new Error("optimizerRuns must be a non-negative integer");
    }
    compiler.optimizerRuns = runs;
  }

  const request: SimulateRequest = {
    chain: form.chain,
    blockNumber: form.blockNumber.trim(),
    sender: form.sender.trim(),
    target: form.target.trim(),
    data: form.data.trim() || "0x",
    labelOverrides: withSenderLabel(form.sender, compactRows(form.labelOverrides, ["account", "label"], "Label overrides")),
    erc20BalanceOverrides: compactRows(form.erc20BalanceOverrides, ["token", "account", "balance"], "ERC20 balance overrides"),
    erc20ApprovalOverrides: compactRows(form.erc20ApprovalOverrides, ["token", "owner", "spender", "amount"], "ERC20 approval overrides"),
    erc721ApprovalOverrides: compactRows(form.erc721ApprovalOverrides, ["token", "owner", "spender", "tokenId"], "ERC721 approval overrides"),
    decodeInternal: form.decodeInternal,
    compiler
  };

  optionalString(request, "projectPath", form.projectPath);
  if (form.stateSource.trim()) {
    const stateOverride: StateOverride = { source: form.stateSource };
    optionalString(stateOverride, "contractName", form.stateContractName);
    request.stateOverride = stateOverride;
  }

  const parsed = simulateRequestSchema.safeParse(request);
  if (!parsed.success) {
    throw new Error(`request validation failed: ${formatSchemaError(parsed.error)}`);
  }
  return parsed.data;
}

function withSenderLabel(sender: string, labels: LabelOverride[]): LabelOverride[] {
  const account = sender.trim();
  if (!isAddress(account) || labels.some((label) => label.account.toLowerCase() === account.toLowerCase())) {
    return labels;
  }
  return [{ account, label: "Sender" }, ...labels];
}

function optionalString<T, K extends keyof T>(target: T, key: K, value: string) {
  const trimmed = value.trim();
  if (trimmed) {
    target[key] = trimmed as T[K];
  }
}

function compactRows<T, K extends keyof T>(rows: T[], fields: K[], label: string): T[] {
  return rows.flatMap((row, index) => {
    const normalized = { ...row };
    const missing: string[] = [];
    let hasAnyValue = false;

    for (const field of fields) {
      const value = String(row[field] ?? "").trim();
      normalized[field] = value as T[K];
      if (value) {
        hasAnyValue = true;
      } else {
        missing.push(String(field));
      }
    }

    if (!hasAnyValue) {
      return [];
    }
    if (missing.length > 0) {
      throw new Error(`${label} row ${index + 1} missing ${missing.join(", ")}`);
    }
    return [normalized];
  });
}

function formatSchemaError(error: { issues: Array<{ path: PropertyKey[]; message: string }> }): string {
  return error.issues
    .slice(0, 3)
    .map((issue) => `${issue.path.length ? issue.path.map(String).join(".") : "request"}: ${issue.message}`)
    .join("; ");
}
