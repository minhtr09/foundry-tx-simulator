package simulation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"

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
	localFoundryDir        = "contracts"
	localSimulationRelPath = "test/SimulateTxRunner.t.sol"
	simulationTestName     = "testSimulateTx"
	inputPathEnvName       = "TXSIM_INPUT_PATH"
	senderLabel            = "Sender"
	latestBlockTimeout     = 10 * time.Second
)

type forgeRunner interface {
	Run(ctx context.Context, args ...string) forge.Result
	RunWithEnv(ctx context.Context, env []string, args ...string) forge.Result
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
	cast    forgeRunner
	prices  priceProvider
	workers chan *simulationWorker

	testCopiesMu sync.Mutex
	testCopies   map[string]int
}

type foundryExecution struct {
	Root          string
	TestDir       string
	TestPath      string
	TestMatchPath string
	External      bool
	tempFiles     []string
	cleanupFuncs  []func()
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
		cast: forge.Runner{
			Bin:      cfg.CastBin,
			RepoRoot: cfg.RepoRoot,
		},
		prices:     prices.DefaultProvider(cfg.RPCURLs),
		workers:    newSimulationWorkers(cfg),
		testCopies: make(map[string]int),
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
	for i := len(e.cleanupFuncs) - 1; i >= 0; i-- {
		e.cleanupFuncs[i]()
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
			if err := s.SaveRecord(model.RecordKindSimulation, req, resp); err != nil {
				slog.Warn("persist simulation record", "run_id", runID, "error", err)
			}
		}
		return resp, status
	}

	rpcURL, err := s.validateRequest(parent, &req)
	if err != nil {
		resp.Error = err.Error()
		return finish(http.StatusBadRequest)
	}
	etherscanAPIKey := strings.TrimSpace(s.cfg.EtherscanAPIKey)
	source := ""
	contractName := ""
	if req.StateOverride != nil {
		source = req.StateOverride.Source
		contractName = req.StateOverride.ContractName
	}
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
	resp.TestPath = execution.TestPath
	slog.Info(
		"foundry execution prepared",
		"run_id", runID,
		"root", execution.Root,
		"test_path", execution.TestPath,
		"test_match_path", execution.TestMatchPath,
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

	testInput := req
	testInput.StateOverrideBytecode = stateBytecode
	inputPath, err := s.writeSimulationInput(&execution, runID, testInput)
	if err != nil {
		resp.Error = "write simulation input: " + err.Error()
		return finish(http.StatusInternalServerError)
	}
	slog.Info("simulation input written", "run_id", runID, "path", inputPath)

	slog.Info("anvil fork prepare started", "run_id", runID, "worker_id", worker.id, "chain", req.Chain, "block_number", req.BlockNumber.String())
	anvilRPCURL, err := worker.anvil.Fork(ctx, rpcURL, req.BlockNumber)
	if err != nil {
		resp.Error = "prepare anvil fork: " + err.Error()
		return finish(http.StatusBadGateway)
	}
	slog.Info("anvil fork ready", "run_id", runID, "worker_id", worker.id, "anvil_rpc", anvilRPCURL)

	forgeArgs := []string{
		"test",
		"--match-path", execution.TestMatchPath,
		"--match-test", simulationTestName,
		"--root", execution.Root,
		"--rpc-url", anvilRPCURL,
		"--json",
	}
	if req.DecodeInternal {
		forgeArgs = append(forgeArgs, "--decode-internal")
	}
	compilerArgs := solidity.ForgeCompilerArgs(req.Compiler)
	forgeArgs = append(forgeArgs, compilerArgs...)
	if etherscanAPIKey != "" {
		forgeArgs = append(forgeArgs, "--etherscan-api-key", etherscanAPIKey)
	}

	slog.Info(
		"forge test started",
		"run_id", runID,
		"root", execution.Root,
		"match_path", execution.TestMatchPath,
		"match_test", simulationTestName,
		"anvil_rpc", anvilRPCURL,
		"decode_internal", req.DecodeInternal,
		"compiler_args", len(compilerArgs),
	)
	result := s.forge.RunWithEnv(ctx, []string{inputPathEnvName + "=" + inputPath}, forgeArgs...)
	logForgeResult(runID, "forge test", result)
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
		resp.Error = "parse forge test json trace: " + err.Error()
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

func (s *Service) ReplayTx(parent context.Context, req model.TxRequest) (model.SimulateResponse, int) {
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
			"tx_hash", req.TxHash,
		}
		if resp.Error != "" {
			attrs = append(attrs, "error", resp.Error)
		}
		if status >= http.StatusBadRequest {
			slog.Warn("tx replay finished", attrs...)
		} else {
			slog.Info("tx replay finished", attrs...)
		}
		if runStarted {
			if err := s.SaveRecord(model.RecordKindTx, req, resp); err != nil {
				slog.Warn("persist tx replay record", "run_id", runID, "error", err)
			}
		}
		return resp, status
	}

	rpcURL, err := s.validateTxRequest(&req)
	if err != nil {
		resp.Error = err.Error()
		return finish(http.StatusBadRequest)
	}
	etherscanAPIKey := strings.TrimSpace(s.cfg.EtherscanAPIKey)

	slog.Info(
		"tx replay started",
		"run_id", runID,
		"chain", req.Chain,
		"tx_hash", req.TxHash,
		"decode_internal", req.DecodeInternal,
		"quick", req.Quick,
		"has_etherscan_key", etherscanAPIKey != "",
	)

	timeout := time.Duration(s.cfg.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	castArgs := []string{
		"run",
		req.TxHash,
		"--rpc-url", rpcURL,
		"--json",
	}
	if req.DecodeInternal {
		castArgs = append(castArgs, "--decode-internal")
	}
	if req.Quick {
		castArgs = append(castArgs, "--quick")
	}
	if etherscanAPIKey != "" {
		castArgs = append(castArgs, "--etherscan-api-key", etherscanAPIKey)
	}

	runStarted = true
	result := s.cast.Run(ctx, castArgs...)
	logForgeResult(runID, "cast run", result)
	resp.DurationMillis = time.Since(start).Milliseconds()
	resp.Stdout = solidity.RedactRPC(solidity.StripANSI(result.Stdout), rpcURL, req.Chain)
	resp.Stderr = solidity.RedactRPC(solidity.StripANSI(result.Stderr), rpcURL, req.Chain)
	combined := strings.TrimSpace(resp.Stdout + "\n" + resp.Stderr)
	resp.ExitCode = result.ExitCode
	resp.Success = result.Err == nil

	parsed, err := traceparser.ParseOutput(resp.Stdout)
	if err != nil {
		resp.Success = false
		resp.Trace = combined
		resp.Error = "parse cast run json trace: " + err.Error()
		return finish(http.StatusBadGateway)
	}
	resp.Trace = parsed.Trace
	slog.Info("cast output parsed", "run_id", runID, "trace_bytes", len(resp.Trace), "erc20_transfers", len(parsed.ERC20Transfers))
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

func (s *Service) validateRequest(ctx context.Context, req *model.SimulateRequest) (string, error) {
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

	if req.BlockNumber == "" {
		latest, err := fetchLatestBlockNumber(ctx, rpcURL)
		if err != nil {
			return "", fmt.Errorf("fetch latest block number: %w", err)
		}
		req.BlockNumber = model.Uint256(latest)
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

func (s *Service) validateTxRequest(req *model.TxRequest) (string, error) {
	req.Chain = strings.TrimSpace(req.Chain)
	req.TxHash = strings.TrimSpace(req.TxHash)
	if err := validateTxRequest(req); err != nil {
		return "", err
	}

	rpcURL, ok := s.cfg.RPCURLs[req.Chain]
	if !ok {
		return "", fmt.Errorf("unknown chain %q", req.Chain)
	}
	if strings.TrimSpace(rpcURL) == "" {
		return "", fmt.Errorf("rpc url for chain %q is empty after environment expansion", req.Chain)
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
	localTestPath := filepath.Join(localRoot, filepath.FromSlash(localSimulationRelPath))
	if req.ProjectPath == "" {
		return foundryExecution{
			Root:          localRoot,
			TestPath:      localTestPath,
			TestMatchPath: filepath.ToSlash(localSimulationRelPath),
		}, nil
	}

	execution := foundryExecution{
		Root:     req.ProjectPath,
		TestDir:  filepath.Join(req.ProjectPath, "test"),
		External: true,
	}
	if err := os.MkdirAll(execution.TestDir, 0o755); err != nil {
		return foundryExecution{}, err
	}

	source, err := os.ReadFile(localTestPath)
	if err != nil {
		return foundryExecution{}, err
	}

	testName := deterministicTestName(source)
	testPath := filepath.Join(execution.TestDir, testName)
	releaseTestCopy, err := s.retainTestCopy(testPath, source)
	if err != nil {
		return foundryExecution{}, err
	}

	execution.TestPath = testPath
	execution.TestMatchPath = filepath.ToSlash(filepath.Join("test", testName))
	execution.cleanupFuncs = append(execution.cleanupFuncs, releaseTestCopy)
	return execution, nil
}

func deterministicTestName(source []byte) string {
	hash := sha256.Sum256(source)
	return fmt.Sprintf("TxSimulation_%x.t.sol", hash)
}

func (s *Service) retainTestCopy(testPath string, source []byte) (func(), error) {
	s.testCopiesMu.Lock()
	defer s.testCopiesMu.Unlock()

	if s.testCopies == nil {
		s.testCopies = make(map[string]int)
	}
	if s.testCopies[testPath] == 0 {
		if err := os.WriteFile(testPath, source, 0o644); err != nil {
			return nil, err
		}
	}
	s.testCopies[testPath]++

	return func() {
		s.releaseTestCopy(testPath)
	}, nil
}

func (s *Service) releaseTestCopy(testPath string) {
	s.testCopiesMu.Lock()
	defer s.testCopiesMu.Unlock()

	count := s.testCopies[testPath]
	if count <= 1 {
		delete(s.testCopies, testPath)
		_ = os.Remove(testPath)
	} else {
		s.testCopies[testPath] = count - 1
	}
}

func (s *Service) buildProjectSrc(ctx context.Context, execution foundryExecution, compiler *model.CompilerConfig) forge.Result {
	args := []string{"build", "src", "--root", execution.Root, "--color", "never"}
	args = append(args, solidity.ForgeCompilerArgsExplicit(compiler)...)
	return s.forge.Run(ctx, args...)
}

func (s *Service) writeStateOverrideSource(execution *foundryExecution, runID string, source string) (string, error) {
	if execution.External {
		statePath := filepath.Join(execution.TestDir, "TxSimulationStateOverride_"+safeRunID(runID)+".sol")
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

func (s *Service) writeSimulationInput(execution *foundryExecution, runID string, req model.SimulateRequest) (string, error) {
	inputDir := filepath.Join(execution.Root, "out", "txsim-"+safeRunID(runID))
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return "", err
	}
	inputPath := filepath.Join(inputDir, "input.json")
	execution.tempFiles = append(execution.tempFiles, inputDir, inputPath)

	encoded, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(inputPath, encoded, 0o600); err != nil {
		return "", err
	}
	return inputPath, nil
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
		slog.Warn("foundry command completed", attrs...)
		return
	}
	slog.Info("foundry command completed", attrs...)
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

func fetchLatestBlockNumber(ctx context.Context, rpcURL string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, latestBlockTimeout)
	defer cancel()

	client, err := rpc.DialContext(ctx, rpcURL)
	if err != nil {
		return "", err
	}
	defer client.Close()

	var blockNumber hexutil.Uint64
	if err := client.CallContext(ctx, &blockNumber, "eth_blockNumber"); err != nil {
		return "", err
	}
	return strconv.FormatUint(uint64(blockNumber), 10), nil
}
