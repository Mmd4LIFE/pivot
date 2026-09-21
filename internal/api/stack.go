package api

import "runtime"

// maxStackBytes bounds a captured stack trace. A runaway recursion produces a
// stack large enough to be its own denial of service when written to a log.
const maxStackBytes = 8 << 10

// stackTrace returns the current goroutine's stack, bounded.
func stackTrace() string {
	buf := make([]byte, maxStackBytes)
	n := runtime.Stack(buf, false)

	return string(buf[:n])
}
