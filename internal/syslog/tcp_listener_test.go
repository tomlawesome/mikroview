package syslog

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestServeTCPFramesOnNewlines(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan RawMessage, 4)
	go ServeTCP(ctx, ln, out)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("line one\nline two\n")); err != nil {
		t.Fatal(err)
	}

	want := []string{"line one", "line two"}
	for _, w := range want {
		select {
		case raw := <-out:
			if string(raw.Data) != w {
				t.Errorf("Data = %q, want %q", raw.Data, w)
			}
			if raw.SourceIP != "127.0.0.1" {
				t.Errorf("SourceIP = %q, want 127.0.0.1", raw.SourceIP)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for line %q", w)
		}
	}
}

func TestServeTCPHandlesMultipleConnections(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan RawMessage, 8)
	go ServeTCP(ctx, ln, out)

	for i := 0; i < 2; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.Write([]byte("hello\n")); err != nil {
			t.Fatal(err)
		}
	}

	received := 0
	for received < 2 {
		select {
		case <-out:
			received++
		case <-time.After(2 * time.Second):
			t.Fatalf("only received %d/2 messages", received)
		}
	}
}
