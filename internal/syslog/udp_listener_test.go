package syslog

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestServeUDPReceivesDatagram(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan RawMessage, 4)
	go ServeUDP(ctx, conn, out)

	client, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	msg := "<134>Jan 15 10:22:31 MikroTik A|lan-wan|forward: in:ether1, len 60"
	if _, err := client.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}

	select {
	case raw := <-out:
		if string(raw.Data) != msg {
			t.Errorf("Data = %q, want %q", raw.Data, msg)
		}
		if raw.SourceIP != "127.0.0.1" {
			t.Errorf("SourceIP = %q, want 127.0.0.1", raw.SourceIP)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for datagram")
	}
}

func TestServeUDPDropsWhenChannelFull(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan RawMessage) // unbuffered: every send needs a reader
	go ServeUDP(ctx, conn, out)

	client, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	// With nobody reading `out`, this send must not block ServeUDP's
	// receive loop forever -- it should just be dropped.
	if _, err := client.Write([]byte("dropped message")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	// A second message, now with a reader in place, must still arrive --
	// proving the receive loop kept running after the drop.
	go func() {
		time.Sleep(50 * time.Millisecond)
		client.Write([]byte("second message"))
	}()

	select {
	case raw := <-out:
		if string(raw.Data) != "second message" {
			t.Errorf("Data = %q, want %q", raw.Data, "second message")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("receive loop appears blocked after a full-channel drop")
	}
}
