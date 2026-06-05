import { simulationRecordSchema } from "../api/schemas";
import type { RequestKind, RunRequest, SimulateResponse, SimulationRecord } from "../api/types";

export function buildSimulationExport(kind: RequestKind, request: RunRequest, response: SimulateResponse): SimulationRecord {
  return {
    id: response.id,
    kind,
    request,
    response
  };
}

export function parseSimulationExport(value: unknown): SimulationRecord {
  const parsed = simulationRecordSchema.safeParse(value);
  if (!parsed.success) {
    const details = parsed.error.issues
      .slice(0, 3)
      .map((issue) => `${issue.path.length ? issue.path.map(String).join(".") : "file"}: ${issue.message}`)
      .join("; ");
    throw new Error(`import validation failed: ${details}`);
  }
  return parsed.data;
}

export function parseSimulationExportText(text: string): SimulationRecord {
  try {
    return parseSimulationExport(JSON.parse(text));
  } catch (err) {
    if (err instanceof SyntaxError) {
      throw new Error(`import JSON is invalid: ${err.message}`, { cause: err });
    }
    throw err;
  }
}

export function simulationExportFilename(id: string): string {
  const safeId = id.replace(/[^a-zA-Z0-9._-]+/g, "-") || "simulation";
  return `foundry-tx-simulator-${safeId}.json`;
}
