package main

import (
	"net"
	"os/exec"
	"testing"
)

func TestServiceOriginFollowsLaunchConfiguration(t *testing.T) {
	backend := &exec.Cmd{Env: []string{"API_ADDR=[::1]:8200"}}
	if got := serviceOrigin("backend", backend); got != "http://[::1]:8200" {
		t.Fatalf("backend origin = %q", got)
	}
	frontend := &exec.Cmd{Env: []string{"WEB_PORT=5190"}}
	if got := serviceOrigin("frontend", frontend); got != "http://127.0.0.1:5190" {
		t.Fatalf("frontend origin = %q", got)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	origin := "http://" + listener.Addr().String()
	if got := listeningServiceOrigin(origin); got != origin {
		t.Fatalf("listening origin = %q", got)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if got := listeningServiceOrigin(origin); got != "" {
		t.Fatalf("stopped service published origin %q", got)
	}
	if got := listeningServiceOrigin("http://192.0.2.1:8190"); got != "" {
		t.Fatalf("non-loopback origin was accepted: %q", got)
	}
}
