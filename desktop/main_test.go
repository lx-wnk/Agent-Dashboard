//go:build darwin

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClaimAddrRejectsAnAddressAlreadyInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close() //nolint:errcheck

	if err := claimAddr(ln.Addr().String()); err == nil {
		t.Fatal("claimAddr accepted an address another listener already holds")
	}
}

func TestClaimAddrReleasesTheAddressItProbed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if err := claimAddr(addr); err != nil {
		t.Fatalf("claimAddr on a free address: %v", err)
	}
	// The probe must not keep the port, or the real server cannot bind it.
	again, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("address still held after claimAddr: %v", err)
	}
	again.Close() //nolint:errcheck
}

// waitForServer cannot tell our server from someone else's on the same address:
// a running instance answers 200 long before our own Serve reaches its listener,
// so its bind error never arrives in time to be noticed. This pins that blindness
// as the reason claimAddr has to run first.
func TestWaitForServerCannotTellAForeignServerFromOurOwn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := waitForServer(context.Background(), make(chan error, 1), srv.URL, 2*time.Second); err != nil {
		t.Fatalf("waitForServer on a foreign but healthy server: %v", err)
	}
}

func TestWaitForServerReportsAStartupFailureThatArrivesFirst(t *testing.T) {
	serverErr := make(chan error, 1)
	serverErr <- errors.New("listen: address already in use")

	// Nothing serves this address, so a timeout would also produce an error —
	// assert the text so deleting the serverErr arm cannot leave this green.
	err := waitForServer(context.Background(), serverErr, "http://127.0.0.1:1/health", 2*time.Second)
	if err == nil {
		t.Fatal("waitForServer ignored a startup failure")
	}
	if !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("want the startup failure, got %v", err)
	}
}
