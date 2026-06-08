package simulation

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"

	"foundry-tx-simulator/backend/internal/model"
)

const (
	defaultAnvilBin       = "anvil"
	defaultAnvilHost      = "127.0.0.1"
	defaultAnvilPortStart = 18545
	anvilStartTimeout     = 15 * time.Second
)

type anvilInstance struct {
	bin       string
	host      string
	port      int
	client    *http.Client
	rpcClient *rpc.Client

	mu      sync.Mutex
	cmd     *exec.Cmd
	done    chan error
	exited  bool
	exitErr error
	stdout  bytes.Buffer
	stderr  bytes.Buffer
}

func newAnvilInstance(bin string, host string, port int) *anvilInstance {
	return &anvilInstance{
		bin:    bin,
		host:   host,
		port:   port,
		client: &http.Client{Timeout: 2 * time.Second},
	}
}

func (a *anvilInstance) Fork(ctx context.Context, rpcURL string, blockNumber model.Uint256) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.runningLocked() {
		a.stopLocked()
		return a.startLocked(ctx, rpcURL, blockNumber)
	}
	resetStart := time.Now()
	slog.Info("anvil reset started", "anvil_rpc", a.rpcURL(), "port", a.port, "fork_block", blockNumber.String())
	if err := a.resetLocked(ctx, rpcURL, blockNumber); err != nil {
		slog.Warn("anvil reset failed; restarting", "anvil_rpc", a.rpcURL(), "port", a.port, "fork_block", blockNumber.String(), "error", err)
		a.stopLocked()
		return a.startLocked(ctx, rpcURL, blockNumber)
	}
	slog.Info("anvil reset completed", "anvil_rpc", a.rpcURL(), "port", a.port, "fork_block", blockNumber.String(), "duration_ms", time.Since(resetStart).Milliseconds())
	return a.rpcURL(), nil
}

func (a *anvilInstance) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopLocked()
}

func (a *anvilInstance) startLocked(ctx context.Context, rpcURL string, blockNumber model.Uint256) (string, error) {
	start := time.Now()
	a.stdout.Reset()
	a.stderr.Reset()
	slog.Info("anvil start started", "anvil_rpc", a.rpcURL(), "port", a.port, "fork_block", blockNumber.String())
	if err := a.ensurePortAvailable(); err != nil {
		slog.Warn("anvil start failed", "anvil_rpc", a.rpcURL(), "port", a.port, "fork_block", blockNumber.String(), "error", err)
		return "", err
	}

	args := []string{
		"--quiet",
		"--host", a.host,
		"--port", strconv.Itoa(a.port),
		"--fork-url", rpcURL,
		"--fork-block-number", blockNumber.String(),
	}
	cmd := exec.Command(a.bin, args...)
	cmd.Stdout = &a.stdout
	cmd.Stderr = &a.stderr
	if err := cmd.Start(); err != nil {
		slog.Warn("anvil start failed", "anvil_rpc", a.rpcURL(), "port", a.port, "fork_block", blockNumber.String(), "error", err)
		return "", fmt.Errorf("start anvil: %w", err)
	}

	a.cmd = cmd
	a.done = make(chan error, 1)
	a.exited = false
	a.exitErr = nil
	go func() {
		a.done <- cmd.Wait()
	}()

	startCtx, cancel := context.WithTimeout(ctx, anvilStartTimeout)
	defer cancel()
	if err := a.waitReady(startCtx); err != nil {
		a.stopLocked()
		slog.Warn("anvil start failed", "anvil_rpc", a.rpcURL(), "port", a.port, "fork_block", blockNumber.String(), "duration_ms", time.Since(start).Milliseconds(), "error", err)
		return "", err
	}
	slog.Info("anvil start completed", "anvil_rpc", a.rpcURL(), "port", a.port, "fork_block", blockNumber.String(), "duration_ms", time.Since(start).Milliseconds())
	return a.rpcURL(), nil
}

func (a *anvilInstance) resetLocked(ctx context.Context, rpcURL string, blockNumber model.Uint256) error {
	block, err := parseForkBlockNumber(blockNumber)
	if err != nil {
		return err
	}
	params := []any{
		map[string]any{
			"forking": map[string]any{
				"jsonRpcUrl":  rpcURL,
				"blockNumber": block,
			},
		},
	}
	var reset bool
	if err := a.callRPC(ctx, &reset, "anvil_reset", params...); err != nil {
		return fmt.Errorf("reset anvil fork: %w", err)
	}
	return nil
}

func (a *anvilInstance) waitReady(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err, ok := a.processExitedLocked(); ok {
			return a.anvilExitError(err)
		}
		var chainID hexutil.Uint64
		if err := a.callRPC(ctx, &chainID, "eth_chainId"); err == nil {
			if err, ok := a.processExitedLocked(); ok {
				return a.anvilExitError(err)
			}
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for anvil rpc at %s: %w: %s", a.rpcURL(), ctx.Err(), strings.TrimSpace(a.stderr.String()))
		case err := <-a.done:
			a.recordProcessExitLocked(err)
			return a.anvilExitError(err)
		case <-ticker.C:
		}
	}
}

func (a *anvilInstance) anvilExitError(err error) error {
	stderr := strings.TrimSpace(a.stderr.String())
	if err == nil {
		return fmt.Errorf("anvil exited before rpc was ready: clean exit: %s", stderr)
	}
	return fmt.Errorf("anvil exited before rpc was ready: %w: %s", err, stderr)
}

func (a *anvilInstance) ensurePortAvailable() error {
	listener, err := net.Listen("tcp", net.JoinHostPort(a.host, strconv.Itoa(a.port)))
	if err != nil {
		return fmt.Errorf("anvil port %s is already in use; set anvil_port_start to a free port: %w", a.rpcURL(), err)
	}
	return listener.Close()
}

func (a *anvilInstance) callRPC(ctx context.Context, result any, method string, params ...any) error {
	client, err := a.rpcClientLocked(ctx)
	if err != nil {
		return err
	}
	return client.CallContext(ctx, result, method, params...)
}

func (a *anvilInstance) rpcClientLocked(ctx context.Context) (*rpc.Client, error) {
	if a.rpcClient != nil {
		return a.rpcClient, nil
	}
	client, err := rpc.DialOptions(ctx, a.rpcURL(), rpc.WithHTTPClient(a.client))
	if err != nil {
		return nil, err
	}
	a.rpcClient = client
	return client, nil
}

func (a *anvilInstance) runningLocked() bool {
	if a.cmd == nil || a.done == nil {
		return false
	}
	if _, ok := a.processExitedLocked(); ok {
		return false
	}
	return true
}

func (a *anvilInstance) processExitedLocked() (error, bool) {
	if a.done == nil {
		return nil, false
	}
	if a.exited {
		return a.exitErr, true
	}
	select {
	case err := <-a.done:
		a.recordProcessExitLocked(err)
		return err, true
	default:
		return nil, false
	}
}

func (a *anvilInstance) recordProcessExitLocked(err error) {
	a.exited = true
	a.exitErr = err
}

func (a *anvilInstance) stopLocked() {
	if a.rpcClient != nil {
		a.rpcClient.Close()
		a.rpcClient = nil
	}
	if a.cmd != nil && a.cmd.Process != nil && a.runningLocked() {
		slog.Info("anvil stop started", "anvil_rpc", a.rpcURL(), "port", a.port)
		_ = a.cmd.Process.Kill()
		select {
		case err := <-a.done:
			a.recordProcessExitLocked(err)
		case <-time.After(2 * time.Second):
		}
		slog.Info("anvil stop completed", "anvil_rpc", a.rpcURL(), "port", a.port)
	}
	a.cmd = nil
	a.done = nil
	a.exited = false
	a.exitErr = nil
}

func (a *anvilInstance) rpcURL() string {
	return "http://" + a.host + ":" + strconv.Itoa(a.port)
}

func parseForkBlockNumber(blockNumber model.Uint256) (uint64, error) {
	block, err := strconv.ParseUint(blockNumber.String(), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("fork block number must fit uint64: %w", err)
	}
	return block, nil
}
