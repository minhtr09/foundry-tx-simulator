import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction, type UIEvent } from "react";
import type { ExpandMode, OutputView } from "../../app/form";
import type { AddressLabels } from "../../lib/labels";
import type { SimulateResponse } from "../../api/types";
import type { TraceNode } from "../../lib/traceTypes";
import BalanceAnalysisView from "../balances/BalanceAnalysisView";
import FundFlowGraph from "../fund-flow/FundFlowGraph";
import TraceTree from "../trace/TraceTree";

type OutputPanelProps = {
  addressLabels: AddressLabels;
  expandDepth: number;
  expandMode: ExpandMode;
  explorerBaseUrl: string;
  outputView: OutputView;
  response: SimulateResponse | null;
  runMeta: string;
  traceNodes: TraceNode[];
  onExpandDepthChange: Dispatch<SetStateAction<number>>;
  onExpandModeChange: Dispatch<SetStateAction<ExpandMode>>;
  onOutputViewChange: Dispatch<SetStateAction<OutputView>>;
};

export default function OutputPanel(props: OutputPanelProps) {
  const {
    addressLabels,
    expandDepth,
    expandMode,
    explorerBaseUrl,
    outputView,
    response,
    runMeta,
    onExpandDepthChange,
    onExpandModeChange,
    onOutputViewChange
  } = props;
  const scrollAreaRef = useRef<HTMLDivElement | null>(null);
  const scrollPositionsRef = useRef<Partial<Record<OutputView, number>>>({});
  const pendingScrollTopRef = useRef<number | null>(null);
  const [traceSearchQuery, setTraceSearchQuery] = useState("");
  const [traceMatchCount, setTraceMatchCount] = useState(0);
  const [traceMatchIndex, setTraceMatchIndex] = useState(0);

  const handleOutputViewChange = useCallback(
    (nextView: OutputView) => {
      const currentScrollTop = readScrollTop(scrollAreaRef.current);
      scrollPositionsRef.current[outputView] = currentScrollTop;
      pendingScrollTopRef.current = scrollPositionsRef.current[nextView] ?? 0;
      onOutputViewChange(nextView);
    },
    [onOutputViewChange, outputView]
  );

  const handleScroll = useCallback(
    (event: UIEvent<HTMLDivElement>) => {
      scrollPositionsRef.current[outputView] = event.currentTarget.scrollTop;
    },
    [outputView]
  );

  useLayoutEffect(() => {
    const pendingScrollTop = pendingScrollTopRef.current;
    if (pendingScrollTop === null) {
      return;
    }
    pendingScrollTopRef.current = null;
    restoreScrollTop(scrollAreaRef.current, pendingScrollTop);
  }, [outputView]);

  useEffect(() => {
    setTraceMatchIndex(0);
  }, [traceSearchQuery]);

  useEffect(() => {
    setTraceMatchIndex((current) => {
      if (traceMatchCount === 0) {
        return 0;
      }
      return Math.min(current, traceMatchCount - 1);
    });
  }, [traceMatchCount]);

  const hasTraceQuery = traceSearchQuery.trim().length > 0;
  const hasTraceMatches = traceMatchCount > 0;
  const traceMatchLabel = hasTraceQuery && hasTraceMatches ? `${traceMatchIndex + 1}/${traceMatchCount}` : "0/0";
  const jsonOutput = useMemo(() => (outputView === "json" ? formatResponseJSON(response) : ""), [outputView, response]);

  return (
    <section className="workspace" aria-label="Simulation output">
      <div className="workspace-toolbar">
        <div>
          <h2>Output</h2>
          <p>{runMeta}</p>
        </div>
        <div className="view-tabs" role="tablist" aria-label="Output views">
          <ViewButton label="Trace" value="trace" active={outputView} onClick={handleOutputViewChange} />
          <ViewButton label="Flow" value="flow" active={outputView} onClick={handleOutputViewChange} />
          <ViewButton label="Balances" value="balances" active={outputView} onClick={handleOutputViewChange} />
          <ViewButton label="JSON" value="json" active={outputView} onClick={handleOutputViewChange} />
        </div>
      </div>

      {outputView === "trace" && (
        <section className="output-view active">
          <div className="section-bar">
            <h3>Transaction Trace</h3>
            <div className="trace-actions">
              <label className="trace-search-control">
                Search
                <input
                  aria-label="Search trace"
                  type="search"
                  value={traceSearchQuery}
                  onChange={(event) => setTraceSearchQuery(event.currentTarget.value)}
                />
              </label>
              <span className="trace-search-count" aria-live="polite">
                {traceMatchLabel}
              </span>
              <div className="icon-actions trace-search-actions">
                <button
                  aria-label="Previous trace match"
                  disabled={!hasTraceMatches}
                  title="Previous trace match"
                  type="button"
                  onClick={() => setTraceMatchIndex((current) => (current + traceMatchCount - 1) % traceMatchCount)}
                >
                  {"<"}
                </button>
                <button
                  aria-label="Next trace match"
                  disabled={!hasTraceMatches}
                  title="Next trace match"
                  type="button"
                  onClick={() => setTraceMatchIndex((current) => (current + 1) % traceMatchCount)}
                >
                  {">"}
                </button>
                <button
                  aria-label="Clear trace search"
                  disabled={!hasTraceQuery}
                  title="Clear trace search"
                  type="button"
                  onClick={() => setTraceSearchQuery("")}
                >
                  x
                </button>
              </div>
              <label className="trace-depth-control">
                Depth
                <input
                  aria-label="Trace expand depth"
                  max={20}
                  min={0}
                  onChange={(event) => {
                    onExpandDepthChange(clampDepth(event.currentTarget.value));
                    onExpandModeChange("depth");
                  }}
                  step={1}
                  type="number"
                  value={expandDepth}
                />
              </label>
              <div className="icon-actions">
                <button type="button" title="Expand trace" onClick={() => onExpandModeChange("expand")}>
                  +
                </button>
                <button type="button" title="Collapse trace" onClick={() => onExpandModeChange("collapse")}>
                  -
                </button>
              </div>
            </div>
          </div>
          <div className="output-scroll" ref={scrollAreaRef} onScroll={handleScroll}>
            <TraceTree
              addressLabels={addressLabels}
              expandDepth={expandDepth}
              explorerBaseUrl={explorerBaseUrl}
              nodes={props.traceNodes}
              expandMode={expandMode}
              searchMatchIndex={traceMatchIndex}
              searchQuery={traceSearchQuery}
              onSearchMatchCountChange={setTraceMatchCount}
            />
          </div>
        </section>
      )}

      {outputView === "flow" && (
        <section className="output-view active">
          <div className="section-bar">
            <h3>Fund Flow</h3>
            <span className="muted">{response?.erc20Transfers?.length ?? 0} transfers</span>
          </div>
          <div className="output-scroll" ref={scrollAreaRef} onScroll={handleScroll}>
            <FundFlowGraph
              addressLabels={addressLabels}
              analysis={response?.balanceAnalysis}
              explorerBaseUrl={explorerBaseUrl}
              transfers={response?.erc20Transfers ?? []}
            />
          </div>
        </section>
      )}

      {outputView === "balances" && (
        <section className="output-view active">
          <div className="section-bar">
            <h3>Balance Analysis</h3>
            <span className="muted">{response?.balanceAnalysis?.changes?.length ?? 0} changes</span>
          </div>
          <div className="output-scroll" ref={scrollAreaRef} onScroll={handleScroll}>
            <BalanceAnalysisView addressLabels={addressLabels} analysis={response?.balanceAnalysis} explorerBaseUrl={explorerBaseUrl} />
          </div>
        </section>
      )}

      {outputView === "json" && (
        <section className="output-view active">
          <div className="section-bar">
            <h3>Raw Response</h3>
          </div>
          <div className="output-scroll" ref={scrollAreaRef} onScroll={handleScroll}>
            <pre className="json-output">{jsonOutput}</pre>
          </div>
        </section>
      )}
    </section>
  );
}

function ViewButton(props: { label: string; value: OutputView; active: OutputView; onClick: (value: OutputView) => void }) {
  return (
    <button type="button" className={`view-button ${props.active === props.value ? "active" : ""}`} onClick={() => props.onClick(props.value)}>
      {props.label}
    </button>
  );
}

function clampDepth(value: string): number {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    return 0;
  }
  return Math.min(20, Math.max(0, Math.trunc(parsed)));
}

function readScrollTop(element: HTMLElement | null): number {
  return element?.scrollTop ?? 0;
}

function restoreScrollTop(element: HTMLElement | null, scrollTop: number) {
  element?.scrollTo({ top: scrollTop });
}

function formatResponseJSON(response: SimulateResponse | null): string {
  if (!response) {
    return "{}";
  }
  return stringifyJSON({
    ...response,
    trace: parseTraceJSON(response.trace)
  });
}

function parseTraceJSON(trace: string): unknown {
  const trimmed = trace.trim();
  if (!trimmed) {
    return "";
  }
  try {
    return JSON.parse(trimmed);
  } catch {
    return trace;
  }
}

function stringifyJSON(value: unknown): string {
  const seen = new WeakSet<object>();
  try {
    return (
      JSON.stringify(
        value,
        (_key, next) => {
          if (typeof next === "bigint") {
            return next.toString();
          }
          if (next && typeof next === "object") {
            if (seen.has(next)) {
              return "[Circular]";
            }
            seen.add(next);
          }
          return next;
        },
        2
      ) ?? "{}"
    );
  } catch (err) {
    return JSON.stringify(
      {
        error: `Unable to render response JSON: ${err instanceof Error ? err.message : String(err)}`
      },
      null,
      2
    );
  }
}
