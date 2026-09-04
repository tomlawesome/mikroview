package main

import (
	"runtime"
	"syscall"
	"testing"
	"time"
)

// TestQuietLogDoesNotCloseFD0 is the regression test for #939. quietLog
// used to wrap descriptor 0 in an *os.File, and an *os.File's finalizer
// closes its descriptor when the value is collected -- so after enough
// loggers and a GC, fd 0 was gone, the next opened file took its number,
// and the next collected logger closed that file under a running read.
// Failed on the unfixed code in 0.06s; the sleeps give the finalizer
// goroutine its turn, since runtime.GC only queues finalizers.
func TestQuietLogDoesNotCloseFD0(t *testing.T) {
	var st syscall.Stat_t
	if err := syscall.Fstat(0, &st); err != nil {
		t.Skipf("fd 0 not open before the test: %v", err)
	}
	for i := 0; i < 200; i++ {
		_ = quietLog()
	}
	for i := 0; i < 3; i++ {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
	}
	if err := syscall.Fstat(0, &st); err != nil {
		t.Fatalf("fd 0 was closed behind the process's back: %v", err)
	}
}
