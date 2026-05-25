package simulation

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"foundry-tx-simulator/backend/internal/config"
	"foundry-tx-simulator/backend/internal/forge"
	"foundry-tx-simulator/backend/internal/fundflow"
	"foundry-tx-simulator/backend/internal/model"
	"foundry-tx-simulator/backend/internal/pathutil"
	"foundry-tx-simulator/backend/internal/prices"
	"foundry-tx-simulator/backend/internal/runid"
	"foundry-tx-simulator/backend/internal/solidity"
	"foundry-tx-simulator/backend/internal/traceparser"
)

const (
	scriptContractName = "SimulateTxScript"
	localFoundryDir    = "contracts"
	localScriptRelPath = "src/SimulateTx.s.sol"
	localScriptTarget  = localFoundryDir + "/" + localScriptRelPath + ":" + scriptContractName
	senderLabel        = "Sender"
)

type forgeRunner interface {
	Run(ctx context.Context, args ...string) forge.Result
}

type priceProvider interface {
	Fetch(ctx context.Context, chain string, tokens []string) (map[string]fundflow.TokenPrice, error)
}

type anvilWorker interface {
	Fork(ctx context.Context, rpcURL string, blockNumber model.Uint256) (string, error)
	Stop()
}

type Service struct {
	cfg     config.Config
	forge   forgeRunner
	prices  priceProvider
	workers chan *simulationWorker
}

type foundryExecution struct {
	Root         string
	ScriptTarget string
	ScriptDir    string
	ScriptPath   string
	External     bool
	tempFiles    []string
}

type simulationWorker struct {
	id    int
	anvil anvilWorker
}

func NewService(cfg config.Config) *Service {
	return &Service{
		cfg: cfg,
		forge: forge.Runner{
			Bin:      cfg.ForgeBin,
			RepoRoot: cfg.RepoRoot,
		},
		prices:  prices.DefaultProvider(cfg.RPCURLs),
		workers: newSimulationWorkers(cfg),
	}
}

func newSimulationWorkers(cfg config.Config) chan *simulationWorker {
	count := cfg.MaxConcurrent
	if count <= 0 {
		count = 1
	}
	host := strings.TrimSpace(cfg.AnvilHost)
	if host == "" {
		host = defaultAnvilHost
	}
	bin := strings.TrimSpace(cfg.AnvilBin)
	if bin == "" {
		bin = defaultAnvilBin
	}
	portStart := cfg.AnvilPortStart
	if portStart <= 0 {
		portStart = defaultAnvilPortStart
	}

	workers := make(chan *simulationWorker, count)
	for i := 0; i < count; i++ {
		workers <- &simulationWorker{
			id:    i,
			anvil: newAnvilInstance(bin, host, portStart+i),
		}
	}
	return workers
}

func (s *Service) Close() {
	if s == nil || s.workers == nil {
		return
	}
	held := make([]*simulationWorker, 0, cap(s.workers))
	for {
		select {
		case worker := <-s.workers:
			held = append(held, worker)
			if worker != nil && worker.anvil != nil {
				worker.anvil.Stop()
			}
		default:
			for _, worker := range held {
				s.workers <- worker
			}
			return
		}
	}
}

func (e *foundryExecution) cleanup() {
	for i := len(e.tempFiles) - 1; i >= 0; i-- {
		_ = os.Remove(e.tempFiles[i])
	}
}

func (s *Service) Simulate(parent context.Context, req model.SimulateRequest) (model.SimulateResponse, int) {
	start := time.Now()
	runID := runid.New()
	resp := model.SimulateResponse{ID: runID}
	runStarted := false
	finish := func(status int) (model.SimulateResponse, int) {
		attrs := []any{
			"run_id", runID,
			"status", status,
			"success", resp.Success,
			"exit_code", resp.ExitCode,
			"duration_ms", time.Since(start).Milliseconds(),
			"chain", req.Chain,
		}
		if resp.Error != "" {
			attrs = append(attrs, "error", resp.Error)
		}
		if status >= http.StatusBadRequest {
			slog.Warn("simulation finished", attrs...)
		} else {
			slog.Info("simulation finished", attrs...)
		}
		if runStarted {
			if err := s.SaveRecord(req, resp); err != nil {
				slog.Warn("persist simulation record", "run_id", runID, "error", err)
			}
		}
		return resp, status
	}

	rpcURL, err := s.validateRequest(&req)
	if err != nil {
		resp.Error = err.Error()
		return finish(http.StatusBadRequest)
	}
	etherscanAPIKey := strings.TrimSpace(s.cfg.EtherscanAPIKey)
	source, contractName := req.StateOverrideSourceAndName()
	hasStateOverride := strings.TrimSpace(source) != ""

	slog.Info(
		"simulation started",
		"run_id", runID,
		"chain", req.Chain,
		"block_number", req.BlockNumber.String(),
		"sender", req.Sender,
		"target", req.Target,
		"data_bytes", normalizedHexBytes(req.Data),
		"external_project", req.ProjectPath != "",
		"project_path", req.ProjectPath,
		"label_overrides", len(req.LabelOverrides),
		"erc20_balance_overrides", len(req.ERC20BalanceOverrides),
		"erc20_approval_overrides", len(req.ERC20ApprovalOverrides),
		"erc721_approval_overrides", len(req.ERC721ApprovalOverrides),
		"has_state_override", hasStateOverride,
		"has_etherscan_key", etherscanAPIKey != "",
	)

	timeout := time.Duration(s.cfg.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	waitStart := time.Now()
	worker, release, err := s.acquireWorker(ctx)
	if err != nil {
		resp.Error = "rate limit: timed out waiting for an available simulation slot"
		return finish(http.StatusTooManyRequests)
	}
	slog.Info("simulation worker acquired", "run_id", runID, "worker_id", worker.id, "wait_ms", time.Since(waitStart).Milliseconds())
	runStarted = true
	defer func() {
		release()
		slog.Info("simulation worker released", "run_id", runID, "worker_id", worker.id)
	}()

	execution, err := s.prepareFoundryExecution(&req, runID)
	if err != nil {
		resp.Error = "prepare foundry project: " + err.Error()
		return finish(http.StatusInternalServerError)
	}
	defer execution.cleanup()
	resp.ScriptPath = execution.ScriptPath
	slog.Info(
		"foundry execution prepared",
		"run_id", runID,
		"root", execution.Root,
		"script_target", execution.ScriptTarget,
		"script_path", execution.ScriptPath,
		"external_project", execution.External,
	)

	if execution.External {
		slog.Info("forge build src started", "run_id", runID, "root", execution.Root)
		buildResult := s.buildProjectSrc(ctx, execution, req.Compiler)
		logForgeResult(runID, "build project src", buildResult)
		if buildResult.Err != nil {
			status := populateForgeFailure(&resp, start, buildResult, rpcURL, req.Chain, "build project src", buildResult.Err)
			return finish(status)
		}
	}

	stateBytecode := "0x"
	if strings.TrimSpace(source) != "" {
		if contractName == "" {
			contractName = solidity.DetectContractName(source)
			if contractName == "" {
				resp.Error = "stateOverride.contractName is required when the source contains no `contract Name` declaration"
				return finish(http.StatusBadRequest)
			}
		}

		statePath, err := s.writeStateOverrideSource(&execution, runID, source)
		if err != nil {
			resp.Error = "write state override source: " + err.Error()
			return finish(http.StatusInternalServerError)
		}
		slog.Info("state override source written", "run_id", runID, "contract", contractName, "path", statePath)

		slog.Info("state override compile started", "run_id", runID, "root", execution.Root, "contract", contractName)
		bytecode, compileResult, err := s.compileStateOverride(ctx, execution.Root, statePath, contractName, req.Compiler)
		logForgeResult(runID, "compile state override", compileResult)
		if err != nil {
			status := populateForgeFailure(&resp, start, compileResult, rpcURL, req.Chain, "compile state override", err)
			return finish(status)
		}
		stateBytecode = bytecode
		slog.Info("state override compile completed", "run_id", runID, "contract", contractName, "bytecode_bytes", normalizedHexBytes(stateBytecode))
	}

	slog.Info("anvil fork prepare started", "run_id", runID, "worker_id", worker.id, "chain", req.Chain, "block_number", req.BlockNumber.String())
	anvilRPCURL, err := worker.anvil.Fork(ctx, rpcURL, req.BlockNumber)
	if err != nil {
		resp.Error = "prepare anvil fork: " + err.Error()
		return finish(http.StatusBadGateway)
	}
	slog.Info("anvil fork ready", "run_id", runID, "worker_id", worker.id, "anvil_rpc", anvilRPCURL)

	forgeArgs := []string{
		"script",
		execution.ScriptTarget,
		"--sig",
		"run((address,string)[],(address,address,uint256)[],(address,address,address,uint256)[],(address,address,address,uint256)[],bytes,address,address,bytes)",
	}
	forgeArgs = append(forgeArgs, solidity.ForgeRunArgs(req, stateBytecode)...)
	forgeArgs = append(forgeArgs,
		"--root", execution.Root,
		"--rpc-url", anvilRPCURL,
		"-vvvvv",
		"--json",
		"--non-interactive",
	)
	compilerArgs := solidity.ForgeCompilerArgs(req.Compiler)
	forgeArgs = append(forgeArgs, compilerArgs...)
	if etherscanAPIKey != "" {
		forgeArgs = append(forgeArgs, "--etherscan-api-key", etherscanAPIKey)
	}

	slog.Info(
		"forge script started",
		"run_id", runID,
		"root", execution.Root,
		"script_target", execution.ScriptTarget,
		"anvil_rpc", anvilRPCURL,
		"compiler_args", len(compilerArgs),
	)
	result := s.forge.Run(ctx, forgeArgs...)
	logForgeResult(runID, "forge script", result)
	resp.DurationMillis = time.Since(start).Milliseconds()
	resp.Stdout = solidity.RedactRPC(solidity.StripANSI(result.Stdout), rpcURL, req.Chain)
	resp.Stdout = strings.ReplaceAll(resp.Stdout, anvilRPCURL, "<anvil-rpc-url>")
	resp.Stderr = solidity.RedactRPC(solidity.StripANSI(result.Stderr), rpcURL, req.Chain)
	resp.Stderr = strings.ReplaceAll(resp.Stderr, anvilRPCURL, "<anvil-rpc-url>")
	combined := strings.TrimSpace(resp.Stdout + "\n" + resp.Stderr)
	resp.ExitCode = result.ExitCode
	resp.Success = result.Err == nil
	parsed, err := traceparser.ParseOutput(resp.Stdout)
	if err != nil {
		resp.Success = false
		resp.Trace = combined
		resp.Error = "parse forge json trace: " + err.Error()
		return finish(http.StatusBadGateway)
	}
	resp.Trace = parsed.Trace
	slog.Info("forge output parsed", "run_id", runID, "trace_bytes", len(resp.Trace), "erc20_transfers", len(parsed.ERC20Transfers))
	if result.Err != nil {
		return finish(http.StatusOK)
	}
	resp.ERC20Transfers = parsed.ERC20Transfers
	slog.Info("fund flow extracted", "run_id", runID, "erc20_transfers", len(resp.ERC20Transfers))
	priceMap := s.fetchTokenPrices(ctx, runID, req.Chain, resp.ERC20Transfers)
	resp.ERC20Transfers = fundflow.EnrichERC20Transfers(resp.ERC20Transfers, priceMap)
	resp.BalanceAnalysis = fundflow.AnalyzeBalanceChanges(resp.ERC20Transfers, priceMap)
	balanceChanges := 0
	userTotals := 0
	if resp.BalanceAnalysis != nil {
		balanceChanges = len(resp.BalanceAnalysis.Changes)
		userTotals = len(resp.BalanceAnalysis.UserTotals)
	}
	slog.Info(
		"balance analysis completed",
		"run_id", runID,
		"price_metadata_tokens", len(priceMap),
		"usd_priced_tokens", fundflow.CountUSDPrices(priceMap),
		"balance_changes", balanceChanges,
		"user_totals", userTotals,
	)

	return finish(http.StatusOK)
}

func (s *Service) validateRequest(req *model.SimulateRequest) (string, error) {
	req.Chain = strings.TrimSpace(req.Chain)
	projectPath, err := s.normalizeProjectPath(req.ProjectPath)
	if err != nil {
		return "", err
	}
	req.ProjectPath = projectPath
	if err := validateSimulateRequest(req); err != nil {
		return "", err
	}

	rpcURL, ok := s.cfg.RPCURLs[req.Chain]
	if !ok {
		return "", fmt.Errorf("unknown chain %q", req.Chain)
	}
	if strings.TrimSpace(rpcURL) == "" {
		return "", fmt.Errorf("rpc url for chain %q is empty after environment expansion", req.Chain)
	}

	normalizedData, err := solidity.NormalizeBytes("data", req.Data)
	if err != nil {
		return "", err
	}
	req.Data = normalizedData
	ensureSenderLabel(req)

	if err := validateCompilerConfig(req.Compiler); err != nil {
		return "", err
	}

	return rpcURL, nil
}

func ensureSenderLabel(req *model.SimulateRequest) {
	for _, label := range req.LabelOverrides {
		if strings.EqualFold(strings.TrimSpace(label.Account), req.Sender) {
			return
		}
	}
	req.LabelOverrides = append([]model.LabelOverride{{Account: req.Sender, Label: senderLabel}}, req.LabelOverrides...)
}

func (s *Service) normalizeProjectPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if resolved, ok := existingDirectoryPath(s.cfg.RepoRoot, value); ok {
		return resolved, nil
	}
	if resolved, ok := s.resolveProjectRootPath(value); ok {
		return resolved, nil
	}
	absPath, err := pathutil.AbsFrom(s.cfg.RepoRoot, value)
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("projectPath %q does not exist or is not mounted in the backend environment", absPath)
}

func (s *Service) resolveProjectRootPath(value string) (string, bool) {
	suffixes := pathSuffixes(value)
	for _, root := range s.cfg.ProjectRoots {
		for _, suffix := range suffixes {
			if resolved, ok := existingDirectoryPath(root, suffix); ok {
				return resolved, true
			}
		}
	}
	return "", false
}

func existingDirectoryPath(baseDir string, value string) (string, bool) {
	absPath, err := pathutil.AbsFrom(baseDir, value)
	if err != nil {
		return "", false
	}
	stat, err := os.Stat(absPath)
	if err != nil || !stat.IsDir() {
		return "", false
	}
	return absPath, true
}

func pathSuffixes(value string) []string {
	cleaned := filepath.Clean(strings.TrimSpace(value))
	if cleaned == "." || cleaned == string(filepath.Separator) {
		return nil
	}
	parts := strings.Split(strings.Trim(cleaned, string(filepath.Separator)), string(filepath.Separator))
	suffixes := make([]string, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		suffix := filepath.Join(parts[i:]...)
		if suffix != "." && suffix != "" {
			suffixes = append(suffixes, suffix)
		}
	}
	return suffixes
}

func validateCompilerConfig(config *model.CompilerConfig) error {
	if config == nil {
		return nil
	}

	config.Use = strings.TrimSpace(config.Use)
	config.EVMVersion = strings.TrimSpace(config.EVMVersion)
	config.RevertStrings = strings.TrimSpace(config.RevertStrings)
	switch config.RevertStrings {
	case "", "default", "strip", "debug", "verboseDebug":
	default:
		return fmt.Errorf("compiler.revertStrings must be one of default, strip, debug, or verboseDebug")
	}
	return nil
}

func (s *Service) prepareFoundryExecution(req *model.SimulateRequest, runID string) (foundryExecution, error) {
	localRoot := filepath.Join(s.cfg.RepoRoot, localFoundryDir)
	localScriptPath := filepath.Join(localRoot, filepath.FromSlash(localScriptRelPath))
	if req.ProjectPath == "" {
		return foundryExecution{
			Root:         localRoot,
			ScriptTarget: localScriptTarget,
			ScriptPath:   localScriptPath,
		}, nil
	}

	execution := foundryExecution{
		Root:      req.ProjectPath,
		ScriptDir: filepath.Join(req.ProjectPath, "script"),
		External:  true,
	}
	if err := os.MkdirAll(execution.ScriptDir, 0o755); err != nil {
		return foundryExecution{}, err
	}

	source, err := os.ReadFile(localScriptPath)
	if err != nil {
		return foundryExecution{}, err
	}

	scriptName := "TxSimulation_" + safeRunID(runID) + ".s.sol"
	scriptPath := filepath.Join(execution.ScriptDir, scriptName)
	if err := os.WriteFile(scriptPath, source, 0o644); err != nil {
		return foundryExecution{}, err
	}

	execution.ScriptPath = scriptPath
	execution.ScriptTarget = filepath.ToSlash(scriptPath) + ":" + scriptContractName
	execution.tempFiles = append(execution.tempFiles, scriptPath)
	return execution, nil
}

func (s *Service) buildProjectSrc(ctx context.Context, execution foundryExecution, compiler *model.CompilerConfig) forge.Result {
	args := []string{"build", "src", "--root", execution.Root, "--color", "never"}
	args = append(args, solidity.ForgeCompilerArgsExplicit(compiler)...)
	return s.forge.Run(ctx, args...)
}

func (s *Service) writeStateOverrideSource(execution *foundryExecution, runID string, source string) (string, error) {
	if execution.External {
		statePath := filepath.Join(execution.ScriptDir, "TxSimulationStateOverride_"+safeRunID(runID)+".sol")
		if err := os.WriteFile(statePath, []byte(source), 0o644); err != nil {
			return "", err
		}
		execution.tempFiles = append(execution.tempFiles, statePath)
		return statePath, nil
	}

	stateDir := filepath.Join(execution.Root, ".txsim", safeRunID(runID))
	if execution.Root == "" {
		stateDir = filepath.Join(s.cfg.WorkDir, "state-overrides", safeRunID(runID))
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return "", err
	}
	statePath := filepath.Join(stateDir, "StateOverride.sol")
	execution.tempFiles = append(execution.tempFiles, stateDir, statePath)
	if err := os.WriteFile(statePath, []byte(source), 0o644); err != nil {
		return "", err
	}
	return statePath, nil
}

func safeRunID(runID string) string {
	return strings.NewReplacer(".", "_", "-", "_").Replace(runID)
}

func (s *Service) compileStateOverride(ctx context.Context, projectRoot string, sourcePath string, contractName string, compiler *model.CompilerConfig) (string, forge.Result, error) {
	contractID, err := solidity.ContractIdentifier(projectRoot, sourcePath, contractName)
	if err != nil {
		return "", forge.Result{}, err
	}

	args := []string{"inspect", contractID, "bytecode", "--root", projectRoot, "--contracts", ".", "--color", "never"}
	args = append(args, solidity.ForgeCompilerArgs(compiler)...)
	result := s.forge.Run(ctx, args...)
	if result.Err != nil {
		return "", result, result.Err
	}

	bytecode, ok := solidity.ExtractBytecode(result.Stdout)
	if !ok {
		return "", result, fmt.Errorf("forge inspect did not return bytecode for %s", contractID)
	}
	return bytecode, result, nil
}

func (s *Service) fetchTokenPrices(ctx context.Context, runID string, chain string, transfers []model.ERC20Transfer) map[string]fundflow.TokenPrice {
	if len(transfers) == 0 {
		return nil
	}
	tokens := transferTokens(transfers)
	slog.Info("token price fetch started", "run_id", runID, "chain", chain, "token_count", len(tokens))
	priceMap := make(map[string]fundflow.TokenPrice)
	if s.prices != nil {
		if fetched, err := s.prices.Fetch(ctx, chain, tokens); err == nil {
			priceMap = fetched
		} else {
			slog.Warn("token price fetch failed", "run_id", runID, "chain", chain, "token_count", len(tokens), "error", err)
		}
	}
	slog.Info(
		"token price fetch completed",
		"run_id", runID,
		"chain", chain,
		"token_count", len(tokens),
		"price_metadata_tokens", len(priceMap),
		"usd_priced_tokens", fundflow.CountUSDPrices(priceMap),
	)
	logTokenPrices(runID, chain, tokens, priceMap)
	return priceMap
}

func logTokenPrices(runID string, chain string, tokens []string, prices map[string]fundflow.TokenPrice) {
	for _, token := range tokens {
		token = strings.ToLower(strings.TrimSpace(token))
		price, ok := prices[token]
		if !ok {
			slog.Warn("token price missing", "run_id", runID, "chain", chain, "token", token)
			continue
		}
		attrs := []any{
			"run_id", runID,
			"chain", chain,
			"token", token,
			"symbol", price.Symbol,
			"has_decimals", price.HasDecimals,
			"decimals", price.Decimals,
			"price_usd", price.PriceUSD,
		}
		if price.PriceUSD <= 0 {
			slog.Warn("token price missing usd", attrs...)
			continue
		}
		slog.Info("token price ready", attrs...)
	}
}

func logForgeResult(runID string, stage string, result forge.Result) {
	attrs := []any{
		"run_id", runID,
		"stage", stage,
		"exit_code", result.ExitCode,
		"duration_ms", result.DurationMillis,
		"stdout_bytes", len(result.Stdout),
		"stderr_bytes", len(result.Stderr),
	}
	if result.Err != nil {
		attrs = append(attrs, "error", result.Err)
		slog.Warn("forge command completed", attrs...)
		return
	}
	slog.Info("forge command completed", attrs...)
}

func normalizedHexBytes(value string) int {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		value = value[2:]
	}
	if value == "" {
		return 0
	}
	return (len(value) + 1) / 2
}

func populateForgeFailure(resp *model.SimulateResponse, start time.Time, result forge.Result, rpcURL string, chain string, prefix string, err error) int {
	resp.DurationMillis = time.Since(start).Milliseconds()
	resp.Stdout = solidity.RedactRPC(solidity.StripANSI(result.Stdout), rpcURL, chain)
	resp.Stderr = solidity.RedactRPC(solidity.StripANSI(result.Stderr), rpcURL, chain)
	resp.Trace = strings.TrimSpace(resp.Stdout + "\n" + resp.Stderr)
	resp.ExitCode = result.ExitCode
	resp.Error = prefix + ": " + err.Error()
	if result.Err != nil {
		return forge.StatusFromCommandError(result.Err)
	}
	return http.StatusInternalServerError
}

func transferTokens(transfers []model.ERC20Transfer) []string {
	seen := make(map[string]struct{}, len(transfers))
	tokens := make([]string, 0, len(transfers))
	for _, transfer := range transfers {
		token := strings.ToLower(strings.TrimSpace(transfer.Token))
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	return tokens
}

func (s *Service) acquireWorker(ctx context.Context) (*simulationWorker, func(), error) {
	select {
	case worker := <-s.workers:
		return worker, func() {
			s.workers <- worker
		}, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}
