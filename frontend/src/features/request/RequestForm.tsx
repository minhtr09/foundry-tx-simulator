import { useRef, useState, type ChangeEventHandler, type FormEventHandler } from "react";
import { browseProject } from "../../api/client";
import type { FormState, HealthStatus, RequestTab, ThemeMode, UpdateForm } from "../../app/form";
import ProjectHistoryDropdown from "./ProjectHistoryDropdown";
import ScriptOverridesTab from "./ScriptOverridesTab";

type RequestFormProps = {
  chains: string[];
  error: string;
  form: FormState;
  canExport: boolean;
  isOpeningRequest: boolean;
  isRunning: boolean;
  projectSuggestions: string[];
  requestLookupId: string;
  requestTab: RequestTab;
  status: HealthStatus;
  theme: ThemeMode;
  onAbort: () => void;
  onExportCopy: () => void;
  onExportFile: () => void;
  onImportFile: (file: File) => void;
  onImportPaste: (text: string) => void;
  onOpenRequest: () => void;
  onProjectBrowsed: (path: string) => void;
  onReload: () => void;
  onRequestTabChange: (value: RequestTab) => void;
  onRequestLookupIdChange: (value: string) => void;
  onSubmit: FormEventHandler<HTMLFormElement>;
  onThemeChange: (value: ThemeMode) => void;
  onUpdate: UpdateForm;
};

export default function RequestForm(props: RequestFormProps) {
  const {
    chains,
    error,
    form,
    canExport,
    isOpeningRequest,
    isRunning,
    projectSuggestions,
    requestLookupId,
    requestTab,
    status,
    theme,
    onAbort,
    onExportCopy,
    onExportFile,
    onImportFile,
    onImportPaste,
    onOpenRequest,
    onProjectBrowsed,
    onReload,
    onRequestTabChange,
    onRequestLookupIdChange,
    onSubmit,
    onThemeChange,
    onUpdate
  } = props;
  const [browseError, setBrowseError] = useState("");
  const [isBrowsingProject, setIsBrowsingProject] = useState(false);
  const [isExportPanelOpen, setIsExportPanelOpen] = useState(false);
  const [isImportPanelOpen, setIsImportPanelOpen] = useState(false);
  const [isPasteImportOpen, setIsPasteImportOpen] = useState(false);
  const [pasteImportText, setPasteImportText] = useState("");
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const isRequestLookupDisabled = isRunning || isOpeningRequest || !requestLookupId.trim();
  const isImportExportDisabled = isRunning || isOpeningRequest;
  const isTxRequest = form.requestKind === "tx";

  const handleImportChange: ChangeEventHandler<HTMLInputElement> = (event) => {
    const file = event.currentTarget.files?.[0];
    event.currentTarget.value = "";
    if (file) {
      onImportFile(file);
      setIsImportPanelOpen(false);
      setIsPasteImportOpen(false);
    }
  };

  const handleBrowseProject = async () => {
    setBrowseError("");
    setIsBrowsingProject(true);
    try {
      const path = await browseProject(form.apiUrl);
      onUpdate("projectPath", path);
      onProjectBrowsed(path);
    } catch (err) {
      setBrowseError(err instanceof Error ? err.message : String(err));
    } finally {
      setIsBrowsingProject(false);
    }
  };

  return (
    <section className="control-panel" aria-label="Simulation request">
      <div className="panel-header">
        <h1>
          <button aria-label="Reload Foundry Tx Simulator" className="title-reload-button" title="Reload" type="button" onClick={onReload}>
            Foundry Tx Simulator
          </button>
        </h1>
        <div className="panel-header-actions">
          <button
            aria-label={theme === "dark" ? "Use light theme" : "Use dark theme"}
            className="theme-toggle"
            type="button"
            onClick={() => onThemeChange(theme === "dark" ? "light" : "dark")}
          >
            {theme === "dark" ? "Light" : "Dark"}
          </button>
          <span className={`status-pill ${status}`}>{status}</span>
        </div>
      </div>

      <form className="request-form" onSubmit={onSubmit}>
        <label>
          API URL
          <input value={form.apiUrl} onChange={(event) => onUpdate("apiUrl", event.target.value)} />
        </label>

        <div className="mode-toggle" role="group" aria-label="Request type">
          <ModeButton label="Simulation" value="simulation" active={form.requestKind} disabled={isRunning} onClick={(value) => onUpdate("requestKind", value)} />
          <ModeButton label="Tx" value="tx" active={form.requestKind} disabled={isRunning} onClick={(value) => onUpdate("requestKind", value)} />
        </div>

        <div className="field-block">
          <label htmlFor="request-id">Request ID</label>
          <span className="request-id-field">
            <input
              id="request-id"
              value={requestLookupId}
              placeholder="20260511T120000.000000000-deadbeef"
              onChange={(event) => onRequestLookupIdChange(event.target.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  if (isRequestLookupDisabled) {
                    return;
                  }
                  onOpenRequest();
                }
              }}
            />
          </span>
          <span className="request-file-actions">
            <input
              ref={importInputRef}
              aria-label="Import simulation data file"
              type="file"
              accept="application/json,.json"
              hidden
              onChange={handleImportChange}
            />
            <button
              className="lookup-button"
              type="button"
              disabled={isRequestLookupDisabled}
              onClick={onOpenRequest}
            >
              {isOpeningRequest ? "Opening..." : "Open"}
            </button>
            <button
              className="secondary-button"
              type="button"
              disabled={isImportExportDisabled}
              onClick={() => {
                setIsExportPanelOpen(false);
                setIsImportPanelOpen((current) => !current);
              }}
            >
              Import
            </button>
            <button
              className="secondary-button"
              type="button"
              disabled={isImportExportDisabled || !canExport}
              onClick={() => {
                setIsImportPanelOpen(false);
                setIsPasteImportOpen(false);
                setIsExportPanelOpen((current) => !current);
              }}
            >
              Export
            </button>
          </span>
          {isImportPanelOpen && (
            <div className="import-panel" role="dialog" aria-label="Import simulation data">
              <span className="import-actions">
                <button
                  aria-label="Import simulation data file"
                  className="secondary-button"
                  type="button"
                  disabled={isImportExportDisabled}
                  onClick={() => importInputRef.current?.click()}
                >
                  File
                </button>
                <button
                  aria-label="Paste simulation data"
                  className="secondary-button"
                  type="button"
                  disabled={isImportExportDisabled}
                  onClick={() => setIsPasteImportOpen((current) => !current)}
                >
                  Paste
                </button>
              </span>
              {isPasteImportOpen && (
                <div className="paste-import-panel" aria-label="Paste simulation data">
                  <label>
                    Simulation Data JSON
                    <textarea
                      value={pasteImportText}
                      rows={7}
                      spellCheck={false}
                      onChange={(event) => setPasteImportText(event.target.value)}
                    />
                  </label>
                  <span className="paste-import-actions">
                    <button
                      className="secondary-button"
                      type="button"
                      disabled={isImportExportDisabled}
                      onClick={() => {
                        onImportPaste(pasteImportText);
                        setIsImportPanelOpen(false);
                        setIsPasteImportOpen(false);
                      }}
                    >
                      Import
                    </button>
                    <button
                      className="secondary-button"
                      type="button"
                      onClick={() => {
                        setIsPasteImportOpen(false);
                        setPasteImportText("");
                      }}
                    >
                      Cancel
                    </button>
                  </span>
                </div>
              )}
            </div>
          )}
          {isExportPanelOpen && (
            <div className="export-panel" role="dialog" aria-label="Export simulation data">
              <span className="export-actions">
                <button
                  aria-label="Copy simulation data to clipboard"
                  className="secondary-button"
                  type="button"
                  disabled={isImportExportDisabled || !canExport}
                  onClick={() => {
                    onExportCopy();
                    setIsExportPanelOpen(false);
                  }}
                >
                  Copy
                </button>
                <button
                  aria-label="Download simulation data file"
                  className="secondary-button"
                  type="button"
                  disabled={isImportExportDisabled || !canExport}
                  onClick={() => {
                    onExportFile();
                    setIsExportPanelOpen(false);
                  }}
                >
                  File
                </button>
              </span>
            </div>
          )}
        </div>

        <div className="two-col">
          <label>
            Chain
            <select value={form.chain} onChange={(event) => onUpdate("chain", event.target.value)}>
              {chains.map((chain) => (
                <option key={chain} value={chain}>
                  {chain}
                </option>
              ))}
            </select>
          </label>
          {isTxRequest ? (
            <label>
              Tx Hash
              <input value={form.txHash} placeholder="0x..." onChange={(event) => onUpdate("txHash", event.target.value)} />
            </label>
          ) : (
            <div className="field-block">
              <span className="field-label-row">
                <label htmlFor="simulation-block">Block</label>
                <label className="latest-block-toggle">
                  <input
                    type="checkbox"
                    checked={form.useLatestBlock}
                    onChange={(event) => onUpdate("useLatestBlock", event.target.checked)}
                  />
                  latest
                </label>
              </span>
              <input
                id="simulation-block"
                value={form.useLatestBlock ? "" : form.blockNumber}
                placeholder={form.useLatestBlock ? "latest" : "23000000"}
                disabled={form.useLatestBlock}
                onChange={(event) => onUpdate("blockNumber", event.target.value)}
              />
            </div>
          )}
        </div>

        {!isTxRequest && (
          <>
            <div className="field-block">
              <label htmlFor="foundry-project">Foundry Project</label>
              <span className="browse-field">
                <input
                  className="project-path-input"
                  id="foundry-project"
                  value={form.projectPath}
                  placeholder="~/foundry-project"
                  onChange={(event) => onUpdate("projectPath", event.target.value)}
                />
                <ProjectHistoryDropdown projects={projectSuggestions} onSelect={(path) => onUpdate("projectPath", path)} />
                <button className="browse-button" type="button" disabled={isBrowsingProject} onClick={handleBrowseProject}>
                  {isBrowsingProject ? "Choosing..." : "Browse"}
                </button>
              </span>
              {browseError && <span className="field-error">{browseError}</span>}
            </div>

            <label>
              Sender
              <input value={form.sender} placeholder="0x..." onChange={(event) => onUpdate("sender", event.target.value)} />
            </label>

            <label>
              Target
              <input value={form.target} placeholder="0x..." onChange={(event) => onUpdate("target", event.target.value)} />
            </label>

            <label>
              Value (wei)
              <input value={form.value} placeholder="0" onChange={(event) => onUpdate("value", event.target.value)} />
            </label>

            <label>
              Calldata
              <textarea
                value={form.data}
                rows={3}
                spellCheck={false}
                placeholder="0x"
                onChange={(event) => onUpdate("data", event.target.value)}
              />
            </label>

          </>
        )}

        <div className="tabs" role="tablist" aria-label="Request sections">
          {!isTxRequest && <TabButton label="Override Options" value="overrides" active={requestTab} onClick={onRequestTabChange} />}
          {!isTxRequest && <TabButton label="Override Contract" value="state" active={requestTab} onClick={onRequestTabChange} />}
          <TabButton label="Run Options" value="run" active={isTxRequest ? "run" : requestTab} onClick={onRequestTabChange} />
        </div>

        {!isTxRequest && requestTab === "overrides" && <ScriptOverridesTab form={form} onUpdate={onUpdate} />}
        {!isTxRequest && requestTab === "state" && <StateTab form={form} onUpdate={onUpdate} />}
        {(isTxRequest || requestTab === "run") && <RunOptionsTab form={form} isTxRequest={isTxRequest} onUpdate={onUpdate} />}

        {error && <div className="error-box">{error}</div>}
        <button
          className={`primary-action ${isRunning ? "abort-action" : ""}`}
          type={isRunning ? "button" : "submit"}
          onClick={isRunning ? onAbort : undefined}
        >
          {isRunning ? "Abort" : isTxRequest ? "Replay Tx" : "Run Simulation"}
        </button>
      </form>
    </section>
  );
}

function StateTab(props: { form: FormState; onUpdate: UpdateForm }) {
  const { form, onUpdate } = props;
  return (
    <section className="tab-panel active">
      <label>
        Override Contract Name
        <input
          value={form.stateContractName}
          placeholder="WETHStateOverride"
          onChange={(event) => onUpdate("stateContractName", event.target.value)}
        />
      </label>
      <label>
        Override Contract Source
        <textarea
          value={form.stateSource}
          rows={10}
          spellCheck={false}
          placeholder={"// SPDX-License-Identifier: UNLICENSED\npragma solidity ^0.8.0;"}
          onChange={(event) => onUpdate("stateSource", event.target.value)}
        />
      </label>
    </section>
  );
}

function RunOptionsTab(props: { form: FormState; isTxRequest: boolean; onUpdate: UpdateForm }) {
  const { form, isTxRequest, onUpdate } = props;
  return (
    <section className="tab-panel active">
      <div className="toggle-grid">
        <Checkbox label="decode internal" checked={form.decodeInternal} onChange={(value) => onUpdate("decodeInternal", value)} />
        {isTxRequest && <Checkbox label="quick" checked={form.quick} onChange={(value) => onUpdate("quick", value)} />}
        {!isTxRequest && (
          <>
            <Checkbox label="viaIR" checked={form.viaIR} onChange={(value) => onUpdate("viaIR", value)} />
            <Checkbox label="optimize" checked={form.optimize} onChange={(value) => onUpdate("optimize", value)} />
            <Checkbox label="offline" checked={form.offline} onChange={(value) => onUpdate("offline", value)} />
            <Checkbox label="no metadata" checked={form.noMetadata} onChange={(value) => onUpdate("noMetadata", value)} />
          </>
        )}
      </div>
      {!isTxRequest && (
        <>
          <div className="two-col">
            <label>
              Solc
              <input value={form.compilerUse} onChange={(event) => onUpdate("compilerUse", event.target.value)} />
            </label>
            <label>
              Optimizer Runs
              <input
                value={form.optimizerRuns}
                inputMode="numeric"
                placeholder="200"
                onChange={(event) => onUpdate("optimizerRuns", event.target.value)}
              />
            </label>
          </div>
          <div className="two-col">
            <label>
              EVM Version
              <input value={form.evmVersion} onChange={(event) => onUpdate("evmVersion", event.target.value)} />
            </label>
            <label>
              Revert Strings
              <select value={form.revertStrings} onChange={(event) => onUpdate("revertStrings", event.target.value)}>
                <option value=""></option>
                <option value="default">default</option>
                <option value="strip">strip</option>
                <option value="debug">debug</option>
                <option value="verboseDebug">verboseDebug</option>
              </select>
            </label>
          </div>
        </>
      )}
    </section>
  );
}

function TabButton(props: { label: string; value: RequestTab; active: RequestTab; onClick: (value: RequestTab) => void }) {
  return (
    <button type="button" className={`tab-button ${props.active === props.value ? "active" : ""}`} onClick={() => props.onClick(props.value)}>
      {props.label}
    </button>
  );
}

function ModeButton(props: {
  label: string;
  value: FormState["requestKind"];
  active: FormState["requestKind"];
  disabled: boolean;
  onClick: (value: FormState["requestKind"]) => void;
}) {
  return (
    <button type="button" className={`mode-button ${props.active === props.value ? "active" : ""}`} disabled={props.disabled} onClick={() => props.onClick(props.value)}>
      {props.label}
    </button>
  );
}

function Checkbox(props: { label: string; checked: boolean; onChange: (value: boolean) => void }) {
  return (
    <label className="checkbox-row">
      <input type="checkbox" checked={props.checked} onChange={(event) => props.onChange(event.target.checked)} />
      {props.label}
    </label>
  );
}
