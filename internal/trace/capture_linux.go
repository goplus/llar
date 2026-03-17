//go:build linux

package trace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const attachReadyTimeout = 2 * time.Second

type attachedTracer struct {
	cmd        *exec.Cmd
	statusFile string
}

// CaptureLockedThread traces the current goroutine on a dedicated OS thread.
// It is intended for best-effort OnBuild tracing rather than whole-process capture.
func CaptureLockedThread(ctx context.Context, opts CaptureOptions, run func() error) (CaptureResult, error) {
	if _, err := exec.LookPath("strace"); err != nil {
		return CaptureResult{}, fmt.Errorf("strace not found: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "llar-trace-*")
	if err != nil {
		return CaptureResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	outFile := tmpDir + "/trace.log"

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	clearPtracer, err := allowPtraceAttach()
	if err != nil {
		return CaptureResult{}, err
	}
	defer clearPtracer()

	tid := unix.Gettid()
	tracer, err := startAttachedTracer(ctx, tid, outFile)
	if err != nil {
		return CaptureResult{}, err
	}
	if err := waitForTracerReady(tracer, tid); err != nil {
		_ = stopAttachedTracer(tracer)
		return CaptureResult{}, err
	}

	runErr := run()
	stopErr := stopAttachedTracer(tracer)
	rawPersistErr := persistRawStraceIfRequested(outFile, tid)
	data, readErr := os.ReadFile(outFile)

	if runErr != nil {
		return CaptureResult{}, runErr
	}
	if stopErr != nil {
		return CaptureResult{}, stopErr
	}
	if rawPersistErr != nil {
		return CaptureResult{}, rawPersistErr
	}
	if readErr != nil {
		return CaptureResult{}, readErr
	}
	records, diagnostics := parseStraceRecordsDetailed(string(data), parseOptions{
		rootCwd:   opts.RootCwd,
		keepRoots: opts.KeepRoots,
	})
	return CaptureResult{Records: records, Diagnostics: diagnostics}, nil
}

func persistRawStraceIfRequested(outFile string, tid int) error {
	dir := strings.TrimSpace(os.Getenv("LLAR_RAW_STRACE_DIR"))
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create raw strace dir %s: %w", dir, err)
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		return fmt.Errorf("read raw strace log %s: %w", outFile, err)
	}
	path := filepath.Join(dir, fmt.Sprintf("raw-strace-tid-%d-%d.log", tid, time.Now().UnixNano()))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write raw strace log %s: %w", path, err)
	}
	return nil
}

func allowPtraceAttach() (func(), error) {
	err := unix.Prctl(unix.PR_SET_PTRACER, uintptr(unix.PR_SET_PTRACER_ANY), 0, 0, 0)
	switch {
	case err == nil:
		return func() {
			_ = unix.Prctl(unix.PR_SET_PTRACER, 0, 0, 0, 0)
		}, nil
	case errors.Is(err, unix.EINVAL):
		// PR_SET_PTRACER is only meaningful when Yama restricted ptrace is enabled.
		// On kernels without that support, classic same-UID ptrace rules still apply.
		return func() {}, nil
	default:
		return nil, fmt.Errorf("enable ptrace attach: %w", err)
	}
}

func startAttachedTracer(ctx context.Context, tid int, outFile string) (*attachedTracer, error) {
	statusFile := outFile + ".status"
	status, err := os.Create(statusFile)
	if err != nil {
		return nil, err
	}
	defer status.Close()

	cmd := exec.CommandContext(ctx, "strace",
		"-f",
		"-ttt",
		"-yy",
		"-s", "65535",
		"-e", "trace=execve,execveat,chdir,open,openat,openat2,creat,rename,renameat,renameat2,unlink,unlinkat,mkdir,mkdirat,symlink,symlinkat,clone,fork,vfork",
		"-o", outFile,
		"-p", strconv.Itoa(tid),
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = status
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &attachedTracer{cmd: cmd, statusFile: statusFile}, nil
}

func waitForTracerReady(tracer *attachedTracer, tid int) error {
	want := fmt.Sprintf("Process %d attached", tid)
	deadline := time.Now().Add(attachReadyTimeout)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(tracer.statusFile)
		text := string(data)
		if strings.Contains(text, want) {
			return nil
		}
		if attachFailure(text) {
			return fmt.Errorf("strace attach failed: %s", strings.TrimSpace(text))
		}
		if tracer.cmd.Process == nil || unix.Kill(tracer.cmd.Process.Pid, 0) != nil {
			if strings.TrimSpace(text) == "" {
				return fmt.Errorf("strace exited before attaching to tid %d", tid)
			}
			return fmt.Errorf("strace exited before attaching to tid %d: %s", tid, strings.TrimSpace(text))
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, _ := os.ReadFile(tracer.statusFile)
	return fmt.Errorf("timed out waiting for strace to attach to tid %d: %s", tid, strings.TrimSpace(string(data)))
}

func attachFailure(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "operation not permitted") ||
		strings.Contains(lower, "no such process") ||
		strings.Contains(lower, "ptrace")
}

func stopAttachedTracer(tracer *attachedTracer) error {
	if tracer == nil || tracer.cmd == nil || tracer.cmd.Process == nil {
		return nil
	}
	_ = tracer.cmd.Process.Signal(os.Interrupt)
	err := tracer.cmd.Wait()
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() && status.Signal() == syscall.SIGINT {
			return nil
		}
	}
	return err
}
