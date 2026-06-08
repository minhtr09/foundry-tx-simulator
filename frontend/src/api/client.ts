import type { ChainConfig, ProjectsResponse, SimulateRequest, SimulateResponse, SimulationRecord, TxRequest } from "./types";
import type { ZodType } from "zod";
import {
  browseProjectResponseSchema,
  chainConfigSchema,
  errorResponseSchema,
  projectsResponseSchema,
  simulationRecordSchema,
  simulateResponseSchema
} from "./schemas";

export type SimulationRunResult = {
  requestId: string;
  response: SimulateResponse;
  record: SimulationRecord;
};

export type RunRequestInput = { kind: "simulation"; request: SimulateRequest } | { kind: "tx"; request: TxRequest };

export async function fetchChainConfig(apiUrl: string): Promise<ChainConfig> {
  const response = await fetch(`${trimSlash(apiUrl)}/chains`);
  const payload = await readJSON(response);
  if (!response.ok) {
    throw new Error(`chains request failed: ${response.status}`);
  }
  return parsePayload(chainConfigSchema, payload, "chains");
}

export async function fetchHealth(apiUrl: string): Promise<boolean> {
  const response = await fetch(`${trimSlash(apiUrl)}/health`);
  return response.ok;
}

export async function fetchProjects(apiUrl: string): Promise<ProjectsResponse> {
  const response = await fetch(`${trimSlash(apiUrl)}/projects`);
  const payload = await readJSON(response);
  if (!response.ok) {
    throw new Error(`projects request failed: ${response.status}`);
  }
  return parsePayload(projectsResponseSchema, payload, "projects");
}

export async function browseProject(apiUrl: string): Promise<string> {
  const response = await fetch(`${trimSlash(apiUrl)}/browse/project`);
  const payload = await readJSON(response);
  if (!response.ok) {
    throw new Error(errorMessage(payload, `browse project request failed: ${response.status}`));
  }
  return parsePayload(browseProjectResponseSchema, payload, "browse project").path;
}

export async function fetchSimulationRecord(apiUrl: string, requestId: string, signal?: AbortSignal): Promise<SimulationRecord> {
  const response = await fetch(`${trimSlash(apiUrl)}/requests/${encodeURIComponent(requestId.trim())}`, { signal });
  const payload = await readJSON(response);
  if (!response.ok) {
    throw new Error(errorMessage(payload, `request lookup failed: ${response.status}`));
  }
  return parsePayload(simulationRecordSchema, payload, "request lookup");
}

export async function simulate(apiUrl: string, request: SimulateRequest, signal?: AbortSignal): Promise<SimulateResponse> {
  const response = await fetch(`${trimSlash(apiUrl)}/simulation`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json"
    },
    signal,
    body: JSON.stringify(request)
  });
  const payload = await readJSON(response);
  if (!response.ok) {
    throw new Error(errorMessage(payload, `simulation request failed: ${response.status}`));
  }
  return parsePayload(simulateResponseSchema, payload, "simulation");
}

export async function replayTx(apiUrl: string, request: TxRequest, signal?: AbortSignal): Promise<SimulateResponse> {
  const response = await fetch(`${trimSlash(apiUrl)}/tx`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json"
    },
    signal,
    body: JSON.stringify(request)
  });
  const payload = await readJSON(response);
  if (!response.ok) {
    throw new Error(errorMessage(payload, `tx request failed: ${response.status}`));
  }
  return parsePayload(simulateResponseSchema, payload, "tx");
}

export async function runRequest(
  apiUrl: string,
  input: RunRequestInput,
  signal?: AbortSignal
): Promise<SimulationRunResult> {
  const response = input.kind === "tx" ? await replayTx(apiUrl, input.request, signal) : await simulate(apiUrl, input.request, signal);
  const fallbackRecord: SimulationRecord = {
    id: response.id,
    kind: input.kind,
    request: input.request,
    response
  };
  try {
    const record = await fetchSimulationRecord(apiUrl, response.id, signal);
    return { requestId: record.id, response: record.response, record };
  } catch {
    return { requestId: response.id, response, record: fallbackRecord };
  }
}

function trimSlash(value: string): string {
  return value.replace(/\/+$/, "");
}

async function readJSON(response: Response): Promise<unknown> {
  try {
    return await response.json();
  } catch (err) {
    throw new Error(`invalid JSON response: ${err instanceof Error ? err.message : String(err)}`, { cause: err });
  }
}

function parsePayload<T>(schema: ZodType<T>, payload: unknown, label: string): T {
  const result = schema.safeParse(payload);
  if (result.success) {
    return result.data;
  }
  throw new Error(`${label} response schema mismatch: ${formatSchemaError(result.error)}`);
}

function errorMessage(payload: unknown, fallback: string): string {
  const result = errorResponseSchema.safeParse(payload);
  return result.success ? result.data.error : fallback;
}

function formatSchemaError(error: unknown): string {
  if (error && typeof error === "object" && "issues" in error) {
    const issues = (error as { issues?: Array<{ path?: PropertyKey[]; message?: string }> }).issues ?? [];
    return issues
      .slice(0, 3)
      .map((issue) => `${formatPath(issue.path, "payload")}: ${issue.message || "invalid"}`)
      .join("; ");
  }
  return String(error);
}

function formatPath(path: PropertyKey[] | undefined, fallback: string): string {
  return path?.length ? path.map(String).join(".") : fallback;
}
