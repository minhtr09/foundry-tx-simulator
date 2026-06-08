package simulation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"

	"foundry-tx-simulator/backend/internal/config"
	"foundry-tx-simulator/backend/internal/forge"
	"foundry-tx-simulator/backend/internal/fundflow"
	"foundry-tx-simulator/backend/internal/model"
)

const (
	wethAddress = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	baycAddress = "0xBC4CA0EdA7647A8aB7C2061c2E118A18a936f13D"
	baycTokenID = "1"
)

const simulationTestSource = `// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.0;
contract SimulateTxRunnerTest {
  function testSimulateTx() public {}
}
`

func writeSimulationTestHarness(t *testing.T, repoRoot string) []byte {
	t.Helper()

	testDir := filepath.Join(repoRoot, "contracts", "test")
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := []byte(simulationTestSource)
	if err := os.WriteFile(filepath.Join(testDir, "SimulateTxRunner.t.sol"), source, 0o644); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestSimulateWETHBalanceApprovalAndTransferFrom(t *testing.T) {
	cfg := loadTestConfig(t)
	blockNumber := mainnetBlockNumber(t, cfg.RPCURLs["mainnet"])

	owner := "0x0000000000000000000000000000000000000001"
	spender := "0x0000000000000000000000000000000000000002"
	recipient := "0x0000000000000000000000000000000000000003"
	amount := "1000000000000000000"

	req := model.SimulateRequest{
		Chain:       "mainnet",
		BlockNumber: model.Uint256(blockNumber),
		LabelOverrides: []model.LabelOverride{
			{Account: owner, Label: "WETHOwner"},
			{Account: spender, Label: "WETHSpender"},
			{Account: recipient, Label: "WETHRecipient"},
		},
		ERC20BalanceOverrides: []model.ERC20BalanceOverride{
			{
				Token:   wethAddress,
				Account: owner,
				Balance: model.Uint256(amount),
			},
		},
		ERC20ApprovalOverrides: []model.ERC20ApprovalOverride{
			{
				Token:   wethAddress,
				Owner:   owner,
				Spender: spender,
				Amount:  model.Uint256(amount),
			},
		},
		Compiler: &model.CompilerConfig{
			ViaIR:         boolPtr(true),
			Optimize:      boolPtr(true),
			OptimizerRuns: uint32Ptr(200),
			EVMVersion:    "cancun",
			RevertStrings: "default",
		},
		DecodeInternal: true,
		Sender:         spender,
		Target:         wethAddress,
		Data:           transferFromCalldata(owner, recipient, mustBigInt(t, amount)),
	}

	resp, status := newTestService(t, cfg).Simulate(context.Background(), req)
	t.Cleanup(func() {
		if resp.RunDir != "" {
			_ = os.RemoveAll(resp.RunDir)
		}
	})

	requireSimulationSuccess(t, status, resp)
	logResponseIfEnabled(t, resp)
	if !strings.Contains(resp.Trace, "transferFrom") {
		t.Fatalf("expected transferFrom in trace, got:\n%s", resp.Trace)
	}
	for _, want := range []string{"WETHOwner", "WETHSpender", "WETHRecipient"} {
		if !strings.Contains(resp.Trace, want) {
			t.Fatalf("expected label %q in trace, got:\n%s", want, resp.Trace)
		}
	}
	requireERC20Transfer(t, resp, amount, owner, recipient)
	requireBalanceAnalysis(t, resp, amount, "-2000", "2000")
}

func TestSimulateStateOverrideContractDealsWETHBalanceAndApproval(t *testing.T) {
	cfg := loadTestConfig(t)
	blockNumber := mainnetBlockNumber(t, cfg.RPCURLs["mainnet"])

	owner := "0x0000000000000000000000000000000000000011"
	spender := "0x0000000000000000000000000000000000000012"
	recipient := "0x0000000000000000000000000000000000000013"
	amount := "1000000000000000000"

	req := model.SimulateRequest{
		Chain:       "mainnet",
		BlockNumber: model.Uint256(blockNumber),
		StateOverride: &model.StateOverride{
			ContractName: "WETHStateOverride",
			Source:       wethStateOverrideSource(owner, spender, amount),
		},
		DecodeInternal: true,
		Sender:         spender,
		Target:         wethAddress,
		Data:           transferFromCalldata(owner, recipient, mustBigInt(t, amount)),
	}

	resp, status := newTestService(t, cfg).Simulate(context.Background(), req)
	t.Cleanup(func() {
		if resp.RunDir != "" {
			_ = os.RemoveAll(resp.RunDir)
		}
	})

	requireSimulationSuccess(t, status, resp)
	logResponseIfEnabled(t, resp)
	for _, want := range []string{"approve", "transferFrom"} {
		if !strings.Contains(resp.Trace, want) {
			t.Fatalf("expected %q in trace, got:\n%s", want, resp.Trace)
		}
	}
}

func TestSimulateNFTApprovalAndTransferFrom(t *testing.T) {
	cfg := loadTestConfig(t)
	rpcURL := cfg.RPCURLs["mainnet"]
	blockNumber := mainnetBlockNumber(t, rpcURL)
	owner := erc721OwnerOf(t, rpcURL, blockNumber, baycAddress, mustBigInt(t, baycTokenID))

	spender := "0x0000000000000000000000000000000000000002"
	recipient := "0x0000000000000000000000000000000000000003"

	req := model.SimulateRequest{
		Chain:       "mainnet",
		BlockNumber: model.Uint256(blockNumber),
		ERC721ApprovalOverrides: []model.ERC721ApprovalOverride{
			{
				Token:   baycAddress,
				Owner:   owner,
				Spender: spender,
				TokenID: model.Uint256(baycTokenID),
			},
		},
		DecodeInternal: true,
		Sender:         spender,
		Target:         baycAddress,
		Data:           transferFromCalldata(owner, recipient, mustBigInt(t, baycTokenID)),
	}

	resp, status := newTestService(t, cfg).Simulate(context.Background(), req)
	t.Cleanup(func() {
		if resp.RunDir != "" {
			_ = os.RemoveAll(resp.RunDir)
		}
	})

	requireSimulationSuccess(t, status, resp)
	logResponseIfEnabled(t, resp)
	if !strings.Contains(resp.Trace, "transferFrom") {
		t.Fatalf("expected transferFrom in trace, got:\n%s", resp.Trace)
	}
	if len(resp.ERC20Transfers) != 0 {
		t.Fatalf("expected no ERC20 transfers for NFT transfer, got %#v", resp.ERC20Transfers)
	}
}

func TestSimulateReturnsTraceWhenForgeTestFailsWithoutFundFlow(t *testing.T) {
	repoRoot := t.TempDir()
	writeSimulationTestHarness(t, repoRoot)

	cfg := config.Config{
		ListenAddr:     "127.0.0.1:0",
		RepoRoot:       repoRoot,
		WorkDir:        filepath.Join(t.TempDir(), "runs"),
		TimeoutSeconds: 30,
		MaxConcurrent:  1,
		ForgeBin:       "forge",
		RPCURLs: map[string]string{
			"mainnet": "http://127.0.0.1:8545",
		},
	}
	fake := &fakeForgeRunner{
		results: []forge.Result{
			{
				Stdout:   forgeJSONTraceWithCall(false, "transfer"),
				Stderr:   "Error: script failed\n",
				ExitCode: 1,
				Err:      errors.New("exit status 1"),
			},
		},
	}
	service := NewService(cfg)
	t.Cleanup(service.Close)
	service.forge = fake
	fakeAnvil := setFakeAnvilWorker(service, "http://127.0.0.1:19000")

	resp, status := service.Simulate(context.Background(), model.SimulateRequest{
		Chain:       "mainnet",
		BlockNumber: "1",
		Sender:      "0x0000000000000000000000000000000000000001",
		Target:      "0x0000000000000000000000000000000000000002",
		Data:        "0x",
	})

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; resp=%#v", status, resp)
	}
	if len(fakeAnvil.calls) != 1 {
		t.Fatalf("anvil calls = %#v, want one fork", fakeAnvil.calls)
	}
	if fakeAnvil.calls[0].rpcURL != "http://127.0.0.1:8545" || fakeAnvil.calls[0].blockNumber != "1" {
		t.Fatalf("anvil fork call = %#v", fakeAnvil.calls[0])
	}
	if len(fake.calls) != 1 ||
		!hasArgSequence(fake.calls[0], "test") ||
		!hasArgSequence(fake.calls[0], "--match-path", "test/SimulateTxRunner.t.sol") ||
		!hasArgSequence(fake.calls[0], "--match-test", "testSimulateTx") ||
		!hasArgSequence(fake.calls[0], "--rpc-url", "http://127.0.0.1:19000") ||
		!containsArg(fake.calls[0], "--json") ||
		containsArg(fake.calls[0], "--decode-internal") ||
		containsArg(fake.calls[0], "--fork-block-number") ||
		containsArg(fake.calls[0], "--non-interactive") {
		t.Fatalf("forge should run the simulation test against worker anvil rpc: %#v", fake.calls)
	}
	if len(fake.envs) != 1 || !hasEnvPrefix(fake.envs[0], inputPathEnvName+"=") {
		t.Fatalf("forge test should receive input path env: %#v", fake.envs)
	}
	if resp.Success {
		t.Fatalf("success = true, want false for failing forge test")
	}
	if resp.ExitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", resp.ExitCode)
	}
	if resp.Error != "" {
		t.Fatalf("error = %q, want empty", resp.Error)
	}
	if len(resp.ERC20Transfers) != 0 {
		t.Fatalf("ERC20 transfers should be skipped on forge test failure: %#v", resp.ERC20Transfers)
	}
	if resp.BalanceAnalysis != nil {
		t.Fatalf("balance analysis should be skipped on test failure: %#v", resp.BalanceAnalysis)
	}
	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"erc20Transfers", "balanceAnalysis", "error"} {
		if _, ok := payload[field]; ok {
			t.Fatalf("failed test response should omit %q: %s", field, encoded)
		}
	}
}

func TestSimulateDecodeInternalTrueAddsForgeFlag(t *testing.T) {
	repoRoot := t.TempDir()
	writeSimulationTestHarness(t, repoRoot)

	cfg := config.Config{
		ListenAddr:     "127.0.0.1:0",
		RepoRoot:       repoRoot,
		WorkDir:        filepath.Join(t.TempDir(), "runs"),
		TimeoutSeconds: 30,
		MaxConcurrent:  1,
		ForgeBin:       "forge",
		RPCURLs: map[string]string{
			"mainnet": "http://127.0.0.1:8545",
		},
	}
	fake := &fakeForgeRunner{}
	service := NewService(cfg)
	t.Cleanup(service.Close)
	service.forge = fake
	setFakeAnvilWorker(service, "http://127.0.0.1:19000")

	_, _ = service.Simulate(context.Background(), model.SimulateRequest{
		Chain:          "mainnet",
		BlockNumber:    "1",
		DecodeInternal: true,
		Sender:         "0x0000000000000000000000000000000000000001",
		Target:         "0x0000000000000000000000000000000000000002",
		Data:           "0x",
	})

	if len(fake.calls) != 1 {
		t.Fatalf("forge call count = %d, want 1: %#v", len(fake.calls), fake.calls)
	}
	if !containsArg(fake.calls[0], "--decode-internal") {
		t.Fatalf("decodeInternal=true should add --decode-internal: %#v", fake.calls[0])
	}
	if len(fake.inputPayloads) != 1 || !strings.Contains(fake.inputPayloads[0], `"decodeInternal":true`) {
		t.Fatalf("simulation input should preserve decodeInternal=true: %#v", fake.inputPayloads)
	}
}

func TestReplayTxRunsCastRun(t *testing.T) {
	cfg := config.Config{
		ListenAddr:      "127.0.0.1:0",
		RepoRoot:        t.TempDir(),
		WorkDir:         filepath.Join(t.TempDir(), "runs"),
		TimeoutSeconds:  30,
		MaxConcurrent:   1,
		CastBin:         "cast",
		EtherscanAPIKey: "etherscan-test-key",
		RPCURLs: map[string]string{
			"mainnet": "http://127.0.0.1:8545",
		},
	}
	fake := &fakeForgeRunner{
		results: []forge.Result{
			{Stdout: castRunJSONTrace()},
		},
	}
	service := NewService(cfg)
	t.Cleanup(service.Close)
	service.cast = fake

	txHash := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	resp, status := service.ReplayTx(context.Background(), model.TxRequest{
		Chain:          "mainnet",
		TxHash:         txHash,
		DecodeInternal: true,
		Quick:          true,
	})

	if status != http.StatusOK || !resp.Success {
		t.Fatalf("tx replay failed: status=%d resp=%#v", status, resp)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("cast call count = %d, want 1: %#v", len(fake.calls), fake.calls)
	}
	args := fake.calls[0]
	if !hasArgSequence(args, "run", txHash) ||
		!hasArgSequence(args, "--rpc-url", "http://127.0.0.1:8545") ||
		!hasArgSequence(args, "--etherscan-api-key", "etherscan-test-key") ||
		!containsArg(args, "--json") ||
		!containsArg(args, "--decode-internal") ||
		!containsArg(args, "--quick") {
		t.Fatalf("unexpected cast run args: %#v", args)
	}
	if len(fake.envs) != 1 || len(fake.envs[0]) != 0 {
		t.Fatalf("cast run should not receive forge test env: %#v", fake.envs)
	}

	record, err := service.LoadRecord(resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Kind != model.RecordKindTx || record.Request["txHash"] != txHash {
		t.Fatalf("unexpected tx replay record: %#v", record)
	}
}

func TestSimulateExternalProjectBuildsSrcCompilesOverrideAndRunsCopiedTest(t *testing.T) {
	repoRoot := t.TempDir()
	testSource := writeSimulationTestHarness(t, repoRoot)

	projectRoot := t.TempDir()
	cfg := config.Config{
		ListenAddr:      "127.0.0.1:0",
		RepoRoot:        repoRoot,
		WorkDir:         filepath.Join(t.TempDir(), "runs"),
		TimeoutSeconds:  30,
		MaxConcurrent:   1,
		ForgeBin:        "forge",
		EtherscanAPIKey: "etherscan-test-key",
		RPCURLs: map[string]string{
			"mainnet": "http://127.0.0.1:8545",
		},
	}
	fake := &fakeForgeRunner{
		results: []forge.Result{
			{Stdout: "build ok\n"},
			{Stdout: "0x6000\n"},
			{Stdout: forgeJSONTrace()},
		},
	}
	service := NewService(cfg)
	t.Cleanup(service.Close)
	service.forge = fake
	setFakeAnvilWorker(service, "http://127.0.0.1:19001")

	resp, status := service.Simulate(context.Background(), model.SimulateRequest{
		Chain:       "mainnet",
		BlockNumber: "1",
		ProjectPath: projectRoot,
		StateOverride: &model.StateOverride{
			ContractName: "OverrideState",
			Source:       "pragma solidity ^0.8.0; contract OverrideState {}",
		},
		Sender: "0x0000000000000000000000000000000000000001",
		Target: "0x0000000000000000000000000000000000000002",
		Data:   "0x",
	})

	if status != http.StatusOK || !resp.Success {
		t.Fatalf("simulation failed: status=%d resp=%#v", status, resp)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("forge call count = %d, want 3: %#v", len(fake.calls), fake.calls)
	}

	buildArgs := fake.calls[0]
	if !hasArgSequence(buildArgs, "build", "src") || !hasArgSequence(buildArgs, "--root", projectRoot) {
		t.Fatalf("unexpected build args: %#v", buildArgs)
	}
	if containsArg(buildArgs, "--via-ir") {
		t.Fatalf("build should use target project defaults unless request compiler fields are set: %#v", buildArgs)
	}

	inspectArgs := fake.calls[1]
	if !hasArgSequence(inspectArgs, "inspect") ||
		!strings.HasPrefix(inspectArgs[1], "test/TxSimulationStateOverride_") ||
		!strings.HasSuffix(inspectArgs[1], ".sol:OverrideState") ||
		!hasArgSequence(inspectArgs, "--root", projectRoot) {
		t.Fatalf("unexpected inspect args: %#v", inspectArgs)
	}

	testArgs := fake.calls[2]
	testHash := sha256.Sum256(testSource)
	wantMatchPath := fmt.Sprintf("test/TxSimulation_%x.t.sol", testHash)
	if !hasArgSequence(testArgs, "test") ||
		!hasArgSequence(testArgs, "--match-path", wantMatchPath) ||
		!hasArgSequence(testArgs, "--match-test", "testSimulateTx") ||
		!hasArgSequence(testArgs, "--root", projectRoot) ||
		!hasArgSequence(testArgs, "--rpc-url", "http://127.0.0.1:19001") ||
		!hasArgSequence(testArgs, "--etherscan-api-key", "etherscan-test-key") ||
		!containsArg(testArgs, "--json") ||
		containsArg(testArgs, "--decode-internal") ||
		containsArg(testArgs, "0x6000") ||
		containsArg(testArgs, "--fork-block-number") ||
		containsArg(testArgs, "--non-interactive") {
		t.Fatalf("unexpected test args: %#v", testArgs)
	}
	if _, ok := envValue(fake.envs[2], inputPathEnvName); !ok {
		t.Fatalf("forge test env missing input path: %#v", fake.envs)
	}
	if len(fake.inputPayloads) != 1 ||
		!strings.Contains(fake.inputPayloads[0], `"stateOverrideBytecode":"0x6000"`) ||
		!strings.Contains(fake.inputPayloads[0], `"labelOverrides":[{"account":"0x0000000000000000000000000000000000000001","label":"Sender"}]`) {
		t.Fatalf("unexpected simulation input: %#v", fake.inputPayloads)
	}

	entries, err := os.ReadDir(filepath.Join(projectRoot, "test"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary test files were not cleaned up: %#v", entries)
	}
}

func TestPrepareFoundryExecutionUsesContentBasedTestCopyWithReferenceCount(t *testing.T) {
	repoRoot := t.TempDir()
	testSource := writeSimulationTestHarness(t, repoRoot)

	projectRoot := t.TempDir()
	service := NewService(config.Config{
		RepoRoot:       repoRoot,
		TimeoutSeconds: 30,
		MaxConcurrent:  1,
		RPCURLs: map[string]string{
			"mainnet": "http://127.0.0.1:8545",
		},
	})
	t.Cleanup(service.Close)

	req := &model.SimulateRequest{ProjectPath: projectRoot}
	first, err := service.prepareFoundryExecution(req, "first-run")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.prepareFoundryExecution(req, "second-run")
	if err != nil {
		t.Fatal(err)
	}

	testHash := sha256.Sum256(testSource)
	wantTestPath := filepath.Join(projectRoot, "test", fmt.Sprintf("TxSimulation_%x.t.sol", testHash))
	if first.TestPath != wantTestPath || second.TestPath != wantTestPath {
		t.Fatalf("test paths = %q and %q, want %q", first.TestPath, second.TestPath, wantTestPath)
	}
	if first.TestMatchPath != fmt.Sprintf("test/TxSimulation_%x.t.sol", testHash) {
		t.Fatalf("test match path = %q", first.TestMatchPath)
	}
	if _, err := os.Stat(wantTestPath); err != nil {
		t.Fatalf("test copy missing before cleanup: %v", err)
	}

	first.cleanup()
	if _, err := os.Stat(wantTestPath); err != nil {
		t.Fatalf("test copy removed while still referenced: %v", err)
	}

	second.cleanup()
	if _, err := os.Stat(wantTestPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("test copy should be removed after last cleanup, stat err = %v", err)
	}
}

func TestSimulatePersistsRequestRecord(t *testing.T) {
	repoRoot := t.TempDir()
	writeSimulationTestHarness(t, repoRoot)

	cfg := config.Config{
		ListenAddr:     "127.0.0.1:0",
		RepoRoot:       repoRoot,
		WorkDir:        filepath.Join(t.TempDir(), "runs"),
		TimeoutSeconds: 30,
		MaxConcurrent:  1,
		ForgeBin:       "forge",
		RPCURLs: map[string]string{
			"mainnet": "http://127.0.0.1:8545",
		},
	}
	fake := &fakeForgeRunner{
		results: []forge.Result{
			{Stdout: forgeJSONTrace()},
		},
	}
	service := NewService(cfg)
	t.Cleanup(service.Close)
	service.forge = fake
	setFakeAnvilWorker(service, "http://127.0.0.1:19002")

	resp, status := service.Simulate(context.Background(), model.SimulateRequest{
		Chain:       "mainnet",
		BlockNumber: "1",
		Sender:      "0x0000000000000000000000000000000000000001",
		Target:      "0x0000000000000000000000000000000000000002",
		Data:        "0x",
	})

	if status != http.StatusOK || !resp.Success {
		t.Fatalf("simulation failed: status=%d resp=%#v", status, resp)
	}
	record, err := service.LoadRecord(resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != resp.ID || record.Response.ID != resp.ID {
		t.Fatalf("unexpected record IDs: %#v", record)
	}
	if record.Kind != model.RecordKindSimulation {
		t.Fatalf("record kind = %q, want simulation", record.Kind)
	}
	if record.Request["blockNumber"] != "1" || record.Request["sender"] != "0x0000000000000000000000000000000000000001" {
		t.Fatalf("unexpected saved request: %#v", record.Request)
	}
	labels, ok := record.Request["labelOverrides"].([]any)
	if !ok || len(labels) == 0 {
		t.Fatalf("saved request should include normalized sender label: %#v", record.Request)
	}
	firstLabel, ok := labels[0].(map[string]any)
	if !ok || firstLabel["label"] != "Sender" {
		t.Fatalf("saved request should include normalized sender label: %#v", record.Request)
	}
	if _, err := os.Stat(filepath.Join(cfg.WorkDir, recordDatabaseFile)); err != nil {
		t.Fatalf("record database was not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.WorkDir, resp.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected per-request record directory: %v", err)
	}
}

func TestSimulateLatestBlockPersistsResolvedBlock(t *testing.T) {
	repoRoot := t.TempDir()
	writeSimulationTestHarness(t, repoRoot)
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x2a"}`))
	}))
	t.Cleanup(rpc.Close)

	cfg := config.Config{
		ListenAddr:     "127.0.0.1:0",
		RepoRoot:       repoRoot,
		WorkDir:        filepath.Join(t.TempDir(), "runs"),
		TimeoutSeconds: 30,
		MaxConcurrent:  1,
		ForgeBin:       "forge",
		RPCURLs: map[string]string{
			"mainnet": rpc.URL,
		},
	}
	fake := &fakeForgeRunner{
		results: []forge.Result{
			{Stdout: forgeJSONTrace()},
		},
	}
	service := NewService(cfg)
	t.Cleanup(service.Close)
	service.forge = fake
	fakeAnvil := setFakeAnvilWorker(service, "http://127.0.0.1:19003")

	resp, status := service.Simulate(context.Background(), model.SimulateRequest{
		Chain:  "mainnet",
		Sender: "0x0000000000000000000000000000000000000001",
		Target: "0x0000000000000000000000000000000000000002",
		Data:   "0x",
	})

	if status != http.StatusOK || !resp.Success {
		t.Fatalf("simulation failed: status=%d resp=%#v", status, resp)
	}
	if len(fakeAnvil.calls) != 1 || fakeAnvil.calls[0].blockNumber != "42" {
		t.Fatalf("anvil should fork at resolved latest block: %#v", fakeAnvil.calls)
	}
	record, err := service.LoadRecord(resp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Request["blockNumber"] != "42" {
		t.Fatalf("saved request should use resolved block number: %#v", record.Request)
	}
}

func TestFetchLatestBlockNumberRejectsNonOKStatus(t *testing.T) {
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(rpc.Close)

	_, err := fetchLatestBlockNumber(context.Background(), rpc.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "429 Too Many Requests") || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchLatestBlockNumberRequiresHexResult(t *testing.T) {
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"42"}`))
	}))
	t.Cleanup(rpc.Close)

	_, err := fetchLatestBlockNumber(context.Background(), rpc.URL)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "without 0x prefix") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSimulateDoesNotPersistRecordWhenWorkerUnavailable(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "runs")
	service := NewService(config.Config{
		WorkDir:        workDir,
		TimeoutSeconds: 0,
		MaxConcurrent:  1,
		RPCURLs: map[string]string{
			"mainnet": "http://127.0.0.1:8545",
		},
	})
	t.Cleanup(service.Close)
	service.workers = make(chan *simulationWorker)

	resp, status := service.Simulate(context.Background(), model.SimulateRequest{
		Chain:       "mainnet",
		BlockNumber: "1",
		Sender:      "0x0000000000000000000000000000000000000001",
		Target:      "0x0000000000000000000000000000000000000002",
		Data:        "0x",
	})

	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d: %#v", status, http.StatusTooManyRequests, resp)
	}
	if resp.RunDir != "" {
		t.Fatalf("RunDir = %q, want empty for an unstarted run", resp.RunDir)
	}
	entries, err := os.ReadDir(workDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("unexpected persisted records for unstarted run: %#v", entries)
	}
}

func TestNormalizeProjectPathResolvesMountedProjectRoot(t *testing.T) {
	workspaceRoot := t.TempDir()
	projectRoot := filepath.Join(workspaceRoot, "ks-dex-aggregator-sc")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	service := NewService(config.Config{
		RepoRoot:     t.TempDir(),
		ProjectRoots: []string{workspaceRoot},
	})

	got, err := service.normalizeProjectPath("/host-only/workspaces/ks-dex-aggregator-sc")
	if err != nil {
		t.Fatal(err)
	}
	if got != projectRoot {
		t.Fatalf("normalized project path = %q, want %q", got, projectRoot)
	}
}

func TestNormalizeProjectPathExpandsHome(t *testing.T) {
	homeDir := t.TempDir()
	projectRoot := filepath.Join(homeDir, "foundry-project")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeDir)

	service := NewService(config.Config{
		RepoRoot: t.TempDir(),
	})

	got, err := service.normalizeProjectPath("~/foundry-project")
	if err != nil {
		t.Fatal(err)
	}
	if got != projectRoot {
		t.Fatalf("normalized project path = %q, want %q", got, projectRoot)
	}
}

func logResponseIfEnabled(t *testing.T, resp model.SimulateResponse) {
	t.Helper()

	if os.Getenv("TXSIM_DEBUG_RESPONSE") == "1" {
		encoded, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("response:\n%s", encoded)
	}
}

func loadTestConfig(t *testing.T) config.Config {
	t.Helper()

	oldConfigPath, hadConfigPath := os.LookupEnv("TXSIM_CONFIG")
	configPath := filepath.Clean(filepath.Join("..", "..", "..", "config.example.yml"))
	if err := os.Setenv("TXSIM_CONFIG", configPath); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadConfigPath {
			_ = os.Setenv("TXSIM_CONFIG", oldConfigPath)
		} else {
			_ = os.Unsetenv("TXSIM_CONFIG")
		}
	})

	cfg, _, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(cfg.RPCURLs["mainnet"]) == "" {
		t.Skip("mainnet RPC URL is required")
	}
	cfg.TimeoutSeconds = 180
	cfg.AnvilPortStart = freeTCPPort(t)
	return cfg
}

func freeTCPPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = listener.Close()
	}()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	value, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func requireSimulationSuccess(t *testing.T, status int, resp model.SimulateResponse) {
	t.Helper()

	if status != http.StatusOK || !resp.Success {
		t.Fatalf("simulation failed: status=%d success=%v exitCode=%d error=%q\nstdout:\n%s\nstderr:\n%s\ntrace:\n%s",
			status,
			resp.Success,
			resp.ExitCode,
			resp.Error,
			resp.Stdout,
			resp.Stderr,
			resp.Trace,
		)
	}
}

func requireERC20Transfer(t *testing.T, resp model.SimulateResponse, amount string, fromNeedle string, toNeedle string) {
	t.Helper()

	for _, transfer := range resp.ERC20Transfers {
		if transfer.Amount == amount && strings.Contains(transfer.From, fromNeedle) && strings.Contains(transfer.To, toNeedle) {
			if strings.TrimSpace(transfer.Token) == "" {
				t.Fatalf("expected transfer token: %#v", transfer)
			}
			return
		}
	}
	t.Fatalf("expected ERC20 transfer amount=%s from~%s to~%s in %#v", amount, fromNeedle, toNeedle, resp.ERC20Transfers)
}

func requireBalanceAnalysis(t *testing.T, resp model.SimulateResponse, rawAmount string, ownerUSD string, recipientUSD string) {
	t.Helper()

	if resp.BalanceAnalysis == nil {
		t.Fatal("expected balance analysis")
	}
	if len(resp.BalanceAnalysis.Changes) != 2 {
		t.Fatalf("balance changes = %#v, want 2", resp.BalanceAnalysis.Changes)
	}
	for _, change := range resp.BalanceAnalysis.Changes {
		if change.RawAmount != rawAmount && change.RawAmount != "-"+rawAmount {
			t.Fatalf("unexpected raw amount in change: %#v", change)
		}
		if change.USDValue == nil {
			t.Fatalf("expected USD value in change: %#v", change)
		}
	}

	wantTotals := map[string]string{
		"0x0000000000000000000000000000000000000001": ownerUSD,
		"0x0000000000000000000000000000000000000003": recipientUSD,
	}
	for _, total := range resp.BalanceAnalysis.UserTotals {
		if want, ok := wantTotals[total.User]; ok {
			got := fmt.Sprintf("%.0f", total.USDValue)
			if got != want {
				t.Fatalf("user %s usd total = %s, want %s", total.User, got, want)
			}
			delete(wantTotals, total.User)
		}
	}
	if len(wantTotals) != 0 {
		t.Fatalf("missing user USD totals: %#v in %#v", wantTotals, resp.BalanceAnalysis.UserTotals)
	}
}

func wethStateOverrideSource(owner string, spender string, amount string) string {
	return fmt.Sprintf(`// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.30;

import "forge-std/Test.sol";

interface IERC20 {
  function approve(address spender, uint256 amount) external returns (bool);
}

contract WETHStateOverride is Test {
  address internal constant WETH = %s;
  address internal constant OWNER = %s;
  address internal constant SPENDER = %s;
  uint256 internal constant AMOUNT = %s;

  fallback() external {
    deal(WETH, OWNER, AMOUNT);
    vm.prank(OWNER);
    IERC20(WETH).approve(SPENDER, AMOUNT);
  }
}
`, wethAddress, owner, spender, amount)
}

func mainnetBlockNumber(t *testing.T, rpcURL string) string {
	t.Helper()

	var result hexutil.Uint64
	callRPC(t, rpcURL, &result, "eth_blockNumber")
	return strconv.FormatUint(uint64(result), 10)
}

func erc721OwnerOf(t *testing.T, rpcURL string, blockNumber string, token string, tokenID *big.Int) string {
	t.Helper()

	blockHex := "0x" + mustBigInt(t, blockNumber).Text(16)
	data := "0x6352211e" + encodeUint256(tokenID)
	var result string
	callRPC(t, rpcURL, &result, "eth_call", map[string]string{
		"to":   token,
		"data": data,
	}, blockHex)
	result = strings.TrimPrefix(result, "0x")
	if len(result) != 64 {
		t.Fatalf("unexpected ownerOf result length: %q", result)
	}
	return "0x" + result[24:]
}

func callRPC(t *testing.T, rpcURL string, result any, method string, params ...any) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := rpc.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.CallContext(ctx, result, method, params...); err != nil {
		t.Fatal(err)
	}
}

func transferFromCalldata(from string, to string, tokenIDOrAmount *big.Int) string {
	return "0x23b872dd" + encodeAddress(from) + encodeAddress(to) + encodeUint256(tokenIDOrAmount)
}

func encodeAddress(address string) string {
	return leftPadHex(strings.TrimPrefix(strings.ToLower(address), "0x"), 64)
}

func encodeUint256(value *big.Int) string {
	return leftPadHex(value.Text(16), 64)
}

func leftPadHex(value string, length int) string {
	if len(value) >= length {
		return value
	}
	return strings.Repeat("0", length-len(value)) + value
}

func boolPtr(value bool) *bool {
	return &value
}

func uint32Ptr(value uint32) *uint32 {
	return &value
}

func forgeJSONTrace() string {
	return forgeJSONTraceWithCall(true, "transfer")
}

func castRunJSONTrace() string {
	return fmt.Sprintf(`{
  "traces": [
    [
      "Execution",
      {
        "arena": [
          {
            "parent": null,
            "children": [1],
            "idx": 0,
            "trace": {
              "depth": 0,
              "success": true,
              "caller": "0x0000000000000000000000000000000000000000",
              "address": "0x0000000000000000000000000000000000000001",
              "kind": "CALL",
              "value": "0x0",
              "data": "0x",
              "output": "0x",
              "gas_used": 1000,
              "gas_limit": 1000000,
              "status": "Return",
              "steps": []
            },
            "logs": [],
            "ordering": [{"Call": 0}]
          },
          {
            "parent": 0,
            "children": [],
            "idx": 1,
            "trace": {
              "depth": 1,
              "success": true,
              "caller": "0x0000000000000000000000000000000000000001",
              "address": "%s",
              "kind": "CALL",
              "value": "0x0",
              "data": "0x",
              "output": "0x",
              "gas_used": 500,
              "gas_limit": 100000,
              "status": "Return",
              "steps": []
            },
            "logs": [],
            "ordering": []
          }
        ]
      }
    ]
  ],
  "labeled_addresses": {}
}`, wethAddress)
}

func forgeJSONTraceWithCall(success bool, function string) string {
	status := "Return"
	testStatus := "Success"
	if !success {
		status = "Revert"
		testStatus = "Failure"
	}
	return fmt.Sprintf(`{
  "test/SimulateTxRunner.t.sol:SimulateTxRunnerTest": {
    "duration": "1ms",
    "test_results": {
      "testSimulateTx()": {
        "status": "%s",
        "reason": null,
        "counterexample": null,
        "logs": [],
        "decoded_logs": [],
        "kind": {"Unit": {"gas": 1500}},
        "traces": [
          [
            "Execution",
            {
              "arena": [
                {
                  "parent": null,
                  "children": [1],
                  "idx": 0,
                  "trace": {
                    "depth": 0,
                    "success": %t,
                    "caller": "0x0000000000000000000000000000000000000000",
                    "address": "0x0000000000000000000000000000000000000001",
                    "kind": "CALL",
                    "value": "0x0",
                    "data": "0x",
                    "output": "0x",
                    "gas_used": 1000,
                    "gas_limit": 1000000,
                    "status": "%s",
                    "steps": [],
                    "decoded": {
                      "label": "SimulateTxRunnerTest",
                      "return_data": "",
                      "call_data": {
                        "signature": "testSimulateTx()",
                        "args": []
                      }
                    }
                  },
                  "logs": [],
                  "ordering": [{"Call": 0}]
                },
                {
                  "parent": 0,
                  "children": [],
                  "idx": 1,
                  "trace": {
                    "depth": 1,
                    "success": %t,
                    "caller": "0x0000000000000000000000000000000000000001",
                    "address": "%s",
                    "kind": "CALL",
                    "value": "0x0",
                    "data": "0x",
                    "output": "0x",
                    "gas_used": 500,
                    "gas_limit": 100000,
                    "status": "%s",
                    "steps": [],
                    "decoded": {
                      "label": "WETH9",
                      "return_data": "",
                      "call_data": {
                        "signature": "%s(address,uint256)",
                        "args": ["0x0000000000000000000000000000000000000003", "1"]
                      }
                    }
                  },
                  "logs": [],
                  "ordering": []
                }
              ]
            }
          ]
        ],
        "labeled_addresses": {},
        "duration": "1ms",
        "breakpoints": {},
        "gas_snapshots": {}
      }
    },
    "warnings": []
  }
}`, testStatus, success, status, success, wethAddress, status, function)
}

type fakeForgeRunner struct {
	calls         [][]string
	envs          [][]string
	inputPayloads []string
	results       []forge.Result
}

func (f *fakeForgeRunner) Run(_ context.Context, args ...string) forge.Result {
	return f.run(nil, args...)
}

func (f *fakeForgeRunner) RunWithEnv(_ context.Context, env []string, args ...string) forge.Result {
	return f.run(env, args...)
}

func (f *fakeForgeRunner) run(env []string, args ...string) forge.Result {
	copiedArgs := append([]string(nil), args...)
	f.calls = append(f.calls, copiedArgs)
	f.envs = append(f.envs, append([]string(nil), env...))
	if inputPath, ok := envValue(env, inputPathEnvName); ok {
		payload, err := os.ReadFile(inputPath)
		if err == nil {
			f.inputPayloads = append(f.inputPayloads, string(payload))
		}
	}
	if len(f.results) == 0 {
		return forge.Result{}
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result
}

type fakePriceProvider struct {
	prices map[string]fundflow.TokenPrice
}

func (f fakePriceProvider) Fetch(_ context.Context, _ string, _ []string) (map[string]fundflow.TokenPrice, error) {
	return f.prices, nil
}

func newTestService(t *testing.T, cfg config.Config) *Service {
	t.Helper()

	service := NewService(cfg)
	t.Cleanup(service.Close)
	service.prices = fakePriceProvider{
		prices: map[string]fundflow.TokenPrice{
			strings.ToLower(wethAddress): {
				PriceUSD:    2000,
				Decimals:    18,
				HasDecimals: true,
			},
		},
	}
	return service
}

type fakeAnvilWorker struct {
	rpcURL string
	calls  []fakeAnvilCall
}

type fakeAnvilCall struct {
	rpcURL      string
	blockNumber string
}

func (f *fakeAnvilWorker) Fork(_ context.Context, rpcURL string, blockNumber model.Uint256) (string, error) {
	f.calls = append(f.calls, fakeAnvilCall{rpcURL: rpcURL, blockNumber: blockNumber.String()})
	return f.rpcURL, nil
}

func (f *fakeAnvilWorker) Stop() {}

func setFakeAnvilWorker(service *Service, rpcURL string) *fakeAnvilWorker {
	fake := &fakeAnvilWorker{rpcURL: rpcURL}
	service.workers = make(chan *simulationWorker, 1)
	service.workers <- &simulationWorker{id: 0, anvil: fake}
	return fake
}

func hasArgSequence(args []string, want ...string) bool {
	if len(want) == 0 || len(want) > len(args) {
		return false
	}
	for i := 0; i <= len(args)-len(want); i++ {
		matched := true
		for j := range want {
			if args[i+j] != want[j] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func hasEnvPrefix(env []string, prefix string) bool {
	_, ok := envValueByPrefix(env, prefix)
	return ok
}

func envValue(env []string, key string) (string, bool) {
	return envValueByPrefix(env, key+"=")
}

func envValueByPrefix(env []string, prefix string) (string, bool) {
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix), true
		}
	}
	return "", false
}

func mustBigInt(t *testing.T, value string) *big.Int {
	t.Helper()

	n, ok := new(big.Int).SetString(value, 10)
	if !ok {
		t.Fatalf("invalid integer %q", value)
	}
	return n
}

func TestTransferFromCalldata(t *testing.T) {
	got := transferFromCalldata(
		"0x0000000000000000000000000000000000000001",
		"0x0000000000000000000000000000000000000003",
		mustBigInt(t, "1000000000000000000"),
	)
	want := "0x23b872dd000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000030000000000000000000000000000000000000000000000000de0b6b3a7640000"
	if got != want {
		t.Fatalf("transferFromCalldata() = %s, want %s", got, want)
	}
}

func TestSimulateResponseJSONOmitsInternalFields(t *testing.T) {
	resp := model.SimulateResponse{
		ID:             "run",
		Success:        true,
		ExitCode:       0,
		DurationMillis: 1,
		Trace:          "Traces:",
		Stdout:         "stdout",
		Stderr:         "stderr",
		RunDir:         "/tmp/run",
		TestPath:       "/tmp/test.sol",
	}

	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	payload := string(encoded)
	for _, field := range []string{"stdout", "stderr", "runDir", "testPath"} {
		if strings.Contains(payload, field) {
			t.Fatalf("response JSON should not include %q: %s", field, payload)
		}
	}
	for _, field := range []string{"trace", "success"} {
		if !strings.Contains(payload, field) {
			t.Fatalf("response JSON should include %q: %s", field, payload)
		}
	}
}

func Example_transferFromCalldata() {
	fmt.Println(transferFromCalldata(
		"0x0000000000000000000000000000000000000001",
		"0x0000000000000000000000000000000000000003",
		big.NewInt(1),
	))
	// Output:
	// 0x23b872dd000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000030000000000000000000000000000000000000000000000000000000000000001
}
