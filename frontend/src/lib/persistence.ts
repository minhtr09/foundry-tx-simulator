import { defaults, type FormState, type OutputView, type RequestTab, type ThemeMode } from "../app/form";
import { simulateResponseSchema, simulationRecordSchema } from "../api/schemas";
import type { SimulateResponse, SimulationRecord } from "../api/types";

const storageKey = "txsim.ui.v1";
const legacyDefaultApiUrl = "http://127.0.0.1:8080";

export type PersistedUIState = {
  form: FormState;
  outputView: OutputView;
  requestTab: RequestTab;
  response: SimulateResponse | null;
  simulationRecord: SimulationRecord | null;
  theme: ThemeMode;
  traceExpandDepth: number;
  defaultApiUrl?: string;
};

export function loadPersistedUIState(): PersistedUIState {
  if (typeof window === "undefined") {
    return defaultUIState();
  }

  const raw = window.localStorage.getItem(storageKey);
  if (!raw) {
    return defaultUIState();
  }

  try {
    const parsed = JSON.parse(raw) as Partial<PersistedUIState>;
    const simulationRecord = sanitizeSimulationRecord(parsed.simulationRecord);
    const response = simulationRecord?.response ?? sanitizeResponse(parsed.response);
    const outputView = validOutputView(parsed.outputView) && response ? parsed.outputView : "trace";
    return {
      form: sanitizeForm(parsed.form, parsed.defaultApiUrl),
      outputView,
      requestTab: validRequestTab(parsed.requestTab) ? parsed.requestTab : "overrides",
      response,
      simulationRecord,
      theme: validThemeMode(parsed.theme) ? parsed.theme : "light",
      traceExpandDepth: clampDepth(parsed.traceExpandDepth)
    };
  } catch {
    return defaultUIState();
  }
}

export function savePersistedUIState(state: PersistedUIState) {
  if (typeof window === "undefined") {
    return;
  }
  try {
    window.localStorage.setItem(storageKey, JSON.stringify({ ...state, defaultApiUrl: defaults.apiUrl }));
  } catch {
    try {
      window.localStorage.setItem(
        storageKey,
        JSON.stringify({ ...state, defaultApiUrl: defaults.apiUrl, response: null, simulationRecord: null })
      );
    } catch {
      // Persisting is only a debugging convenience; never block the UI on quota issues.
    }
  }
}

function sanitizeForm(value: unknown, persistedDefaultApiUrl: unknown): FormState {
  if (!value || typeof value !== "object") {
    return defaults;
  }

  const input = value as Partial<FormState>;
  const form = { ...defaults };
  for (const key of Object.keys(defaults) as Array<keyof FormState>) {
    if (key === "apiUrl") {
      continue;
    }

    const next = input[key];
    if (next !== undefined) {
      form[key] = next as never;
    }
  }

  if (typeof input.apiUrl === "string") {
    form.apiUrl = apiUrlForRuntime(input.apiUrl, persistedDefaultApiUrl);
  }

  return form;
}

function apiUrlForRuntime(savedApiUrl: string, persistedDefaultApiUrl: unknown): string {
  const savedDefaultApiUrl = typeof persistedDefaultApiUrl === "string" ? persistedDefaultApiUrl : legacyDefaultApiUrl;
  if (savedApiUrl === savedDefaultApiUrl) {
    return defaults.apiUrl;
  }
  return savedApiUrl;
}

function defaultUIState(): PersistedUIState {
  return {
    form: defaults,
    outputView: "trace",
    requestTab: "overrides",
    response: null,
    simulationRecord: null,
    theme: "light",
    traceExpandDepth: 3
  };
}

function validRequestTab(value: unknown): value is RequestTab {
  return value === "overrides" || value === "state" || value === "run";
}

function validOutputView(value: unknown): value is OutputView {
  return value === "trace" || value === "flow" || value === "balances" || value === "json";
}

function validThemeMode(value: unknown): value is ThemeMode {
  return value === "light" || value === "dark";
}

function sanitizeResponse(value: unknown): SimulateResponse | null {
  const parsed = simulateResponseSchema.safeParse(value);
  return parsed.success ? parsed.data : null;
}

function sanitizeSimulationRecord(value: unknown): SimulationRecord | null {
  const parsed = simulationRecordSchema.safeParse(value);
  return parsed.success ? parsed.data : null;
}

function clampDepth(value: unknown): number {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    return 3;
  }
  return Math.min(20, Math.max(0, Math.trunc(parsed)));
}
