import { FormEvent, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchChainConfig, fetchHealth, fetchProjects, fetchSimulationRecord, runRequest } from "../api/client";
import OutputPanel from "../features/output/OutputPanel";
import RequestForm from "../features/request/RequestForm";
import { explorerForChain } from "../lib/explorer";
import {
  buildRunRequest,
  formFromRecord,
  type BuiltRunRequest,
  type ExpandMode,
  type FormState,
  type HealthStatus,
  type OutputView,
  type RequestTab,
  type ThemeMode
} from "./form";
import { buildAddressLabels } from "../lib/labels";
import { loadPersistedUIState, savePersistedUIState } from "../lib/persistence";
import { traceDataFromResponse } from "../lib/forgeTrace";
import { parseSimulationExportText, simulationExportFilename } from "../lib/simulationExchange";
import type { SimulateRequest, SimulateResponse, SimulationRecord } from "../api/types";

const requestLookupTimeoutMillis = 10_000;

export default function App() {
  const queryClient = useQueryClient();
  const initialUIState = useMemo(() => loadPersistedUIState(), []);
  const [form, setForm] = useState<FormState>(initialUIState.form);
  const [optimisticProjects, setOptimisticProjects] = useState<string[]>([]);
  const [requestTab, setRequestTab] = useState<RequestTab>(initialUIState.requestTab);
  const [outputView, setOutputView] = useState<OutputView>(initialUIState.outputView);
  const [response, setResponse] = useState<SimulateResponse | null>(initialUIState.response);
  const [simulationRecord, setSimulationRecord] = useState<SimulationRecord | null>(initialUIState.simulationRecord);
  const [requestLookupId, setRequestLookupId] = useState(initialUIState.response?.id ?? "");
  const [theme, setTheme] = useState<ThemeMode>(initialUIState.theme);
  const [error, setError] = useState("");
  const [isOpeningRequest, setIsOpeningRequest] = useState(false);
  const [expandMode, setExpandMode] = useState<ExpandMode>("depth");
  const [traceExpandDepth, setTraceExpandDepth] = useState(initialUIState.traceExpandDepth);
  const simulationAbortRef = useRef<AbortController | null>(null);
  const requestLookupAbortRef = useRef<AbortController | null>(null);
  const loadedInitialRequestRef = useRef(false);

  useEffect(() => {
    savePersistedUIState({ form, outputView, requestTab, response, simulationRecord, theme, traceExpandDepth });
  }, [form, outputView, requestTab, response, simulationRecord, theme, traceExpandDepth]);

  useLayoutEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  const healthQuery = useQuery({
    queryKey: ["health", form.apiUrl],
    queryFn: () => fetchHealth(form.apiUrl),
    refetchInterval: 10_000
  });
  const chainQuery = useQuery({
    queryKey: ["chains", form.apiUrl],
    queryFn: () => fetchChainConfig(form.apiUrl)
  });
  const projectQuery = useQuery({
    queryKey: ["projects", form.apiUrl],
    queryFn: () => fetchProjects(form.apiUrl)
  });

  const chains = chainQuery.data?.chains.length ? chainQuery.data.chains : ["mainnet"];
  const explorerUrls = useMemo(() => chainQuery.data?.explorerUrls ?? {}, [chainQuery.data?.explorerUrls]);
  const projectSuggestions = useMemo(
    () => mergeProjects(optimisticProjects, projectQuery.data?.projects ?? []),
    [optimisticProjects, projectQuery.data?.projects]
  );
  const status: HealthStatus = healthQuery.isSuccess ? (healthQuery.data ? "online" : "error") : healthQuery.isError ? "error" : "offline";

  useEffect(() => {
    if (chainQuery.data?.chains.length && !chainQuery.data.chains.includes(form.chain)) {
      setForm((current) => ({ ...current, chain: chainQuery.data.chains[0] }));
    }
  }, [chainQuery.data, form.chain]);

  const runMeta = useMemo(() => {
    if (!response) {
      return "No run yet";
    }
    const state = response.success ? "success" : "failed";
    return `${state} | ${response.durationMillis}ms | exit ${response.exitCode} | ${response.id}`;
  }, [response]);
  const traceData = useMemo(() => traceDataFromResponse(response), [response]);
  const addressLabels = useMemo(
    () => buildAddressLabels(form.requestKind === "simulation" ? form.labelOverrides : [], form.requestKind === "simulation" ? form.sender : "", response, traceData.labels),
    [form.labelOverrides, form.requestKind, form.sender, response, traceData.labels]
  );
  const explorerBaseUrl = useMemo(() => explorerForChain(explorerUrls, form.chain), [explorerUrls, form.chain]);

  const update = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }));
  };

  const simulation = useMutation({
    mutationFn: ({ apiUrl, run, signal }: { apiUrl: string; run: BuiltRunRequest; signal: AbortSignal }) =>
      runRequest(apiUrl, run, signal),
    onSuccess: (result, variables) => {
      setResponse(result.response);
      setSimulationRecord(result.record);
      setRequestLookupId(result.requestId);
      setOutputView("trace");
      setExpandMode("depth");
      setOptimisticProjects([]);
      syncRequestIdToURL(result.requestId);
      if (variables.run.kind === "simulation") {
        void queryClient.invalidateQueries({ queryKey: ["projects", variables.apiUrl] });
      }
    },
    onError: (err) => {
      if (isAbortError(err)) {
        setError("Run aborted");
        return;
      }
      setError(err instanceof Error ? err.message : String(err));
    },
    onSettled: (_result, _err, variables) => {
      if (simulationAbortRef.current?.signal === variables?.signal) {
        simulationAbortRef.current = null;
      }
    }
  });

  const openStoredRequestById = useCallback(
    (requestId: string) => {
      const trimmedRequestId = requestId.trim();
      if (!trimmedRequestId || isOpeningRequest) {
        return;
      }

      const apiUrl = form.apiUrl;
      const controller = new AbortController();
      let didTimeout = false;
      const timeoutId = window.setTimeout(() => {
        didTimeout = true;
        controller.abort();
      }, requestLookupTimeoutMillis);

      requestLookupAbortRef.current?.abort();
      requestLookupAbortRef.current = controller;
      setIsOpeningRequest(true);
      setError("");

      fetchSimulationRecord(apiUrl, trimmedRequestId, controller.signal)
        .then((record) => {
          setForm(formFromRecord(record, apiUrl));
          setResponse(record.response);
          setSimulationRecord(record);
          setRequestLookupId(record.id);
          setOutputView("trace");
          setExpandMode("depth");
          setError("");
          syncRequestIdToURL(record.id);
          const projectPath = record.kind === "simulation" ? (record.request as SimulateRequest).projectPath : "";
          if (projectPath) {
            setOptimisticProjects((current) => mergeProjects([projectPath], current).slice(0, 20));
          }
        })
        .catch((err) => {
          if (controller.signal.aborted && !didTimeout) {
            return;
          }
          setError(didTimeout ? "Request lookup timed out" : err instanceof Error ? err.message : String(err));
        })
        .finally(() => {
          window.clearTimeout(timeoutId);
          if (requestLookupAbortRef.current === controller) {
            requestLookupAbortRef.current = null;
            setIsOpeningRequest(false);
          }
        });
    },
    [form.apiUrl, isOpeningRequest]
  );

  useEffect(() => {
    if (loadedInitialRequestRef.current || typeof window === "undefined") {
      return;
    }
    loadedInitialRequestRef.current = true;
    const requestId = new URLSearchParams(window.location.search).get("requestId")?.trim();
    if (!requestId) {
      return;
    }
    setRequestLookupId(requestId);
    openStoredRequestById(requestId);
  }, [openStoredRequestById]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (simulation.isPending) {
      return;
    }
    setError("");
    let run: BuiltRunRequest;
    try {
      run = buildRunRequest(form);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      return;
    }

    const controller = new AbortController();
    simulationAbortRef.current = controller;
    simulation.mutate({ apiUrl: form.apiUrl, run, signal: controller.signal });
  };

  const abortSimulation = () => {
    simulationAbortRef.current?.abort();
  };

  const reloadApp = () => {
    window.location.reload();
  };

  const openStoredRequest = () => {
    openStoredRequestById(requestLookupId);
  };

  const buildSimulationExportData = () => {
    if (!simulationRecord) {
      throw new Error("Run or import a simulation before exporting");
    }

    return {
      filename: simulationExportFilename(simulationRecord.id),
      text: `${JSON.stringify(simulationRecord, null, 2)}\n`
    };
  };

  const exportSimulationDataToFile = () => {
    try {
      const exported = buildSimulationExportData();
      downloadTextAsFile(exported.text, exported.filename);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const copySimulationData = async () => {
    try {
      if (!navigator.clipboard?.writeText) {
        throw new Error("clipboard write is not available");
      }
      await navigator.clipboard.writeText(buildSimulationExportData().text);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const downloadTextAsFile = (text: string, filename: string) => {
    const blob = new Blob([text], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 0);
  };

  const applySimulationImport = (text: string) => {
    const parsed = parseSimulationExportText(text);
    setForm(formFromRecord(parsed, form.apiUrl));
    setResponse(parsed.response);
    setSimulationRecord(parsed);
    setRequestLookupId(parsed.id);
    setOutputView("trace");
    setExpandMode("depth");
    const projectPath = parsed.kind === "simulation" ? (parsed.request as SimulateRequest).projectPath : "";
    if (projectPath) {
      setOptimisticProjects((current) => mergeProjects([projectPath], current).slice(0, 20));
    }
    setError("");
  };

  const importSimulationData = async (file: File) => {
    try {
      applySimulationImport(await file.text());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const pasteSimulationData = (text: string) => {
    try {
      applySimulationImport(text);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const changeRequestLookupId = (value: string) => {
    if (isOpeningRequest) {
      requestLookupAbortRef.current?.abort();
      requestLookupAbortRef.current = null;
      setIsOpeningRequest(false);
    }
    setRequestLookupId(value);
  };

  return (
    <main className="app-shell">
      <RequestForm
        chains={chains}
        error={error}
        form={form}
        canExport={Boolean(simulationRecord)}
        isRunning={simulation.isPending}
        isOpeningRequest={isOpeningRequest}
        projectSuggestions={projectSuggestions}
        requestLookupId={requestLookupId}
        requestTab={requestTab}
        status={status}
        theme={theme}
        onAbort={abortSimulation}
        onProjectBrowsed={(path) => {
          setOptimisticProjects((current) => mergeProjects([path], current).slice(0, 20));
          void queryClient.invalidateQueries({ queryKey: ["projects", form.apiUrl] });
        }}
        onExportCopy={() => void copySimulationData()}
        onExportFile={exportSimulationDataToFile}
        onImportFile={(file) => void importSimulationData(file)}
        onImportPaste={pasteSimulationData}
        onReload={reloadApp}
        onRequestTabChange={setRequestTab}
        onRequestLookupIdChange={changeRequestLookupId}
        onOpenRequest={openStoredRequest}
        onSubmit={submit}
        onThemeChange={setTheme}
        onUpdate={update}
      />
      <OutputPanel
        addressLabels={addressLabels}
        expandMode={expandMode}
        expandDepth={traceExpandDepth}
        explorerBaseUrl={explorerBaseUrl}
        outputView={outputView}
        response={response}
        runMeta={runMeta}
        traceNodes={traceData.nodes}
        onExpandDepthChange={setTraceExpandDepth}
        onExpandModeChange={setExpandMode}
        onOutputViewChange={setOutputView}
      />
    </main>
  );
}

function isAbortError(err: unknown): boolean {
  return err instanceof DOMException && err.name === "AbortError";
}

function mergeProjects(primary: string[], secondary: string[]): string[] {
  const seen = new Set<string>();
  const merged: string[] = [];
  for (const path of [...primary, ...secondary]) {
    if (!path || seen.has(path)) {
      continue;
    }
    seen.add(path);
    merged.push(path);
  }
  return merged;
}

function syncRequestIdToURL(requestId: string) {
  if (typeof window === "undefined" || !requestId) {
    return;
  }
  const url = new URL(window.location.href);
  url.searchParams.set("requestId", requestId);
  window.history.replaceState(null, "", `${url.pathname}${url.search}${url.hash}`);
}
