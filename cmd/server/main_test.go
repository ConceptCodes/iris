package main

import (
	"net/http"
	"testing"
	"time"

	"iris/internal/constants"
)

func TestNewHTTPServerAppliesExpectedDefaults(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	srv := newHTTPServer(":9090", handler)

	if srv.Addr != ":9090" {
		t.Fatalf("expected addr :9090, got %q", srv.Addr)
	}
	if srv.Handler == nil {
		t.Fatal("expected handler to be set")
	}
	if srv.ReadTimeout != constants.HTTPTimeout30s {
		t.Fatalf("unexpected read timeout: %v", srv.ReadTimeout)
	}
	if srv.WriteTimeout != constants.HTTPTimeout60s {
		t.Fatalf("unexpected write timeout: %v", srv.WriteTimeout)
	}
	if srv.IdleTimeout != constants.HTTPTimeout120s {
		t.Fatalf("unexpected idle timeout: %v", srv.IdleTimeout)
	}
	if srv.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("unexpected read header timeout: %v", srv.ReadHeaderTimeout)
	}
	if srv.MaxHeaderBytes != 1<<20 {
		t.Fatalf("unexpected max header bytes: %d", srv.MaxHeaderBytes)
	}
}
