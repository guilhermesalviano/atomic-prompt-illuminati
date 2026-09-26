package agent

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ProcSpec describes a subprocess to run with line-level streaming.
type ProcSpec struct {
	Bin     string
	Args    []string
	Dir     string
	Env     []string
	Timeout time.Duration
	OnLine  func(stream, line string)
}

// ProcResult is the raw outcome of a subprocess.
type ProcResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	TimedOut bool
	Err      error
}

// Exec runs a subprocess, streaming stdout/stderr line by line, and enforces the
// timeout by killing the whole process group so child agents cannot outlive us.
func Exec(ctx context.Context, spec ProcSpec) ProcResult {
	start := time.Now()

	runCtx := ctx
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(runCtx, spec.Bin, spec.Args...)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid signals the whole process group.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	var outBuf, errBuf bytes.Buffer
	outLines := newLineWriter("stdout", spec.OnLine)
	errLines := newLineWriter("stderr", spec.OnLine)
	cmd.Stdout = io.MultiWriter(&outBuf, outLines)
	cmd.Stderr = io.MultiWriter(&errBuf, errLines)

	err := cmd.Run()
	outLines.Flush()
	errLines.Flush()

	res := ProcResult{
		ExitCode: -1, // No successful exit when the process could not start.
		Stdout:   outBuf.String(),
		Stderr:   errBuf.String(),
		Duration: time.Since(start),
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		res.TimedOut = true
		res.Err = runCtx.Err()
	case errors.Is(ctx.Err(), context.Canceled):
		res.Err = ctx.Err()
	case err != nil:
		res.Err = err
	}
	return res
}

// lineWriter splits writes on newlines and forwards each complete line.
type lineWriter struct {
	stream string
	fn     func(stream, line string)
	buf    []byte
}

func newLineWriter(stream string, fn func(stream, line string)) *lineWriter {
	return &lineWriter{stream: stream, fn: fn}
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]
		if w.fn != nil {
			w.fn(w.stream, line)
		}
	}
	return len(p), nil
}

func (w *lineWriter) Flush() {
	if len(w.buf) > 0 && w.fn != nil {
		w.fn(w.stream, strings.TrimRight(string(w.buf), "\r"))
		w.buf = nil
	}
}
