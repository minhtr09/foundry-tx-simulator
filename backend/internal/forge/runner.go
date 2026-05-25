package forge

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"time"
)

type Runner struct {
	Bin      string
	RepoRoot string
}

type Result struct {
	Stdout         string
	Stderr         string
	ExitCode       int
	DurationMillis int64
	Err            error
}

func (r Runner) Run(ctx context.Context, args ...string) Result {
	return r.run(ctx, nil, args...)
}

func (r Runner) RunWithEnv(ctx context.Context, env []string, args ...string) Result {
	return r.run(ctx, env, args...)
}

func (r Runner) run(ctx context.Context, env []string, args ...string) Result {
	start := time.Now()
	cmd := exec.CommandContext(ctx, r.Bin, args...)
	cmd.Dir = r.RepoRoot
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := Result{
		Stdout:         stdout.String(),
		Stderr:         stderr.String(),
		ExitCode:       0,
		DurationMillis: time.Since(start).Milliseconds(),
		Err:            err,
	}

	if err == nil {
		return result
	}

	if ctx.Err() != nil {
		result.ExitCode = -1
		result.Err = ctx.Err()
		return result
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	} else {
		result.ExitCode = -1
	}
	return result
}

func StatusFromCommandError(err error) int {
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	if err == nil {
		return http.StatusOK
	}
	return http.StatusInternalServerError
}
