package mcputil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ExecResult holds the output and metadata from a traced subprocess execution.
type ExecResult struct {
	Output   string
	ExitCode int
	Err      error
	TimedOut bool
	Duration time.Duration
}

// TracedExec runs a subprocess wrapped in an OpenTelemetry span.
//
// The span is named "exec.<binary>" (e.g. "exec.kubectl") and records:
//   - exec.command     — the binary name
//   - exec.args        — the arguments (joined, truncated to 500 chars)
//   - exec.exit_code   — process exit code
//   - exec.duration_ms — execution duration in milliseconds
//   - exec.timed_out   — true if the process was killed by timeout
//
// The timeout defaults to 30 seconds. Use TracedExecWithTimeout for custom values.
func TracedExec(ctx context.Context, name string, args ...string) ExecResult {
	return TracedExecWithTimeout(ctx, 30*time.Second, name, args...)
}

// TracedExecWithTimeout is like TracedExec but with an explicit timeout.
func TracedExecWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) ExecResult {
	binary := filepath.Base(name)

	ctx, span := Tracer.Start(ctx, "exec."+binary, trace.WithAttributes(
		attribute.String("exec.command", binary),
		attribute.String("exec.args", truncate(strings.Join(args, " "), 500)),
	))
	defer span.End()

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	cmd := exec.CommandContext(execCtx, name, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()

	result := ExecResult{
		Output:   strings.TrimSpace(string(out)),
		Duration: time.Since(start),
	}

	if execCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		result.Err = fmt.Errorf("timed out after %s", timeout)
		span.SetAttributes(attribute.Bool("exec.timed_out", true))
		span.SetStatus(codes.Error, result.Err.Error())
	} else if err != nil {
		result.Err = err
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	span.SetAttributes(
		attribute.Int("exec.exit_code", result.ExitCode),
		attribute.Float64("exec.duration_ms", float64(result.Duration.Milliseconds())),
	)

	return result
}
