//go:build !linux

package trace

import (
	"context"
	"fmt"
	"runtime"
)

func CaptureLockedThread(context.Context, CaptureOptions, func() error) (CaptureResult, error) {
	return CaptureResult{}, fmt.Errorf("trace is unsupported on %s", runtime.GOOS)
}
