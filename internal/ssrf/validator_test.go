package ssrf

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

func TestValidateURLRejectsNonHTTPSchemes(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"ftp", "ftp://example.com/file"},
		{"file", "file:///etc/passwd"},
		{"javascript", "javascript:alert(1)"},
		{"data", "data:text/plain,hello"},
		{"gopher", "gopher://example.com"},
	}

	validator := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateURL(context.Background(), tt.url)
			if err == nil {
				t.Fatalf("expected error for %s scheme", tt.name)
			}
			if !errors.Is(err, ErrInvalidScheme) {
				t.Fatalf("expected ErrInvalidScheme, got %v", err)
			}
			if reason := GetBlockReason(err); reason != BlockReasonInvalidScheme {
				t.Fatalf("expected BlockReasonInvalidScheme, got %s", reason)
			}
		})
	}
}

func TestValidateIPRejectsLoopback(t *testing.T) {
	tests := []struct {
		name string
		ip   string
	}{
		{"ipv4_127_0_0_1", "127.0.0.1"},
		{"ipv4_127_0_0_99", "127.0.0.99"},
		{"ipv4_127_255_255_255", "127.255.255.255"},
		{"ipv6_loopback", "::1"},
	}

	validator := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateIP(context.Background(), net.ParseIP(tt.ip), "http://test.local/path")
			if err == nil {
				t.Fatalf("expected error for loopback %s", tt.ip)
			}
			if !errors.Is(err, ErrLoopback) {
				t.Fatalf("expected ErrLoopback, got %v", err)
			}
		})
	}
}

func TestValidateIPRejectsRFC1918Private(t *testing.T) {
	tests := []struct {
		name string
		ip   string
	}{
		{"10_0_0_1", "10.0.0.1"},
		{"10_255_255_255", "10.255.255.255"},
		{"172_16_0_1", "172.16.0.1"},
		{"172_31_255_255", "172.31.255.255"},
		{"192_168_0_1", "192.168.0.1"},
		{"192_168_255_255", "192.168.255.255"},
	}

	validator := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateIP(context.Background(), net.ParseIP(tt.ip), "http://test.local/path")
			if err == nil {
				t.Fatalf("expected error for private %s", tt.ip)
			}
			if !errors.Is(err, ErrPrivateNetwork) {
				t.Fatalf("expected ErrPrivateNetwork, got %v", err)
			}
		})
	}
}

func TestValidateIPRejectsLinkLocal(t *testing.T) {
	tests := []struct {
		name string
		ip   string
	}{
		{"ipv4_169_254_0_1", "169.254.0.1"},
		{"ipv4_169_254_255_255", "169.254.255.255"},
		{"ipv6_fe80_1", "fe80::1"},
	}

	validator := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateIP(context.Background(), net.ParseIP(tt.ip), "http://test.local/path")
			if err == nil {
				t.Fatalf("expected error for link-local %s", tt.ip)
			}
			if !errors.Is(err, ErrLinkLocal) {
				t.Fatalf("expected ErrLinkLocal, got %v", err)
			}
		})
	}
}

func TestValidateIPRejectsMulticast(t *testing.T) {
	tests := []struct {
		name string
		ip   string
	}{
		{"ipv4_224_0_0_1", "224.0.0.1"},
		{"ipv4_239_255_255_255", "239.255.255.255"},
		{"ipv6_ff00_1", "ff00::1"},
		{"ipv6_ff02_1", "ff02::1"},
	}

	validator := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateIP(context.Background(), net.ParseIP(tt.ip), "http://test.local/path")
			if err == nil {
				t.Fatalf("expected error for multicast %s", tt.ip)
			}
			if !errors.Is(err, ErrMulticast) {
				t.Fatalf("expected ErrMulticast, got %v", err)
			}
		})
	}
}

func TestValidateIPRejectsUnspecified(t *testing.T) {
	tests := []struct {
		name string
		ip   string
	}{
		{"ipv4_0_0_0_0", "0.0.0.0"},
		{"ipv6_unspecified", "::"},
	}

	validator := NewValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateIP(context.Background(), net.ParseIP(tt.ip), "http://test.local/path")
			if err == nil {
				t.Fatalf("expected error for unspecified %s", tt.ip)
			}
			if !errors.Is(err, ErrUnspecified) {
				t.Fatalf("expected ErrUnspecified, got %v", err)
			}
		})
	}
}

func TestValidateIPRejectsMetadataEndpoint(t *testing.T) {
	validator := NewValidator()
	err := validator.validateIP(context.Background(), net.ParseIP("169.254.169.254"), "http://metadata.test/latest/meta-data/")
	if err == nil {
		t.Fatal("expected error for metadata endpoint")
	}
	if !errors.Is(err, ErrMetadataEndpoint) {
		t.Fatalf("expected ErrMetadataEndpoint, got %v", err)
	}
	if reason := GetBlockReason(err); reason != BlockReasonMetadataEndpoint {
		t.Fatalf("expected BlockReasonMetadataEndpoint, got %s", reason)
	}
}

func TestWithAllowPrivateNetworksAllowsPrivateButBlocksMetadataAndMulticast(t *testing.T) {
	tests := []struct {
		name        string
		ip          string
		shouldBlock bool
		expectedErr error
	}{
		{"loopback_allowed", "127.0.0.1", false, nil},
		{"10_x_allowed", "10.0.0.1", false, nil},
		{"172_16_allowed", "172.16.0.1", false, nil},
		{"192_168_allowed", "192.168.0.1", false, nil},
		{"link_local_allowed", "169.254.0.1", false, nil},
		{"metadata_blocked", "169.254.169.254", true, ErrMetadataEndpoint},
		{"multicast_blocked", "224.0.0.1", true, ErrMulticast},
	}

	validator := NewValidator(WithAllowPrivateNetworks(true))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.validateIP(context.Background(), net.ParseIP(tt.ip), "http://test.local/path")
			if tt.shouldBlock {
				if err == nil {
					t.Fatalf("expected error for %s", tt.name)
				}
				if !errors.Is(err, tt.expectedErr) {
					t.Fatalf("expected %v, got %v", tt.expectedErr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error for %s, got %v", tt.name, err)
				}
			}
		})
	}
}

func TestHTTPCheckRedirectRejectsUnsafeRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			w.Header().Set("Location", "http://127.0.0.1:9999/redirected")
			w.WriteHeader(http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	validator := NewValidator()
	client := validator.NewSafeClient(5 * time.Second)

	resp, err := client.Get(server.URL + "/start")
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected error for redirect to blocked host")
	}
	if !IsValidationError(err) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
}

func TestHTTPCheckRedirectStopsAfter10Redirects(t *testing.T) {
	redirectCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		if redirectCount > 15 {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, r.URL.Path+"x", http.StatusFound)
	}))
	defer server.Close()

	validator := NewValidator(WithAllowPrivateNetworks(true))
	client := validator.NewSafeClient(5 * time.Second)

	_, err := client.Get(server.URL + "/start")
	if err == nil {
		t.Fatal("expected error after 10 redirects")
	}
	if !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Fatalf("expected 'stopped after 10 redirects' error, got: %v", err)
	}
}

func TestIsValidationErrorAndGetBlockReason(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		isValidation   bool
		expectedReason BlockReason
	}{
		{
			name:           "validation_error",
			err:            &ValidationError{Reason: BlockReasonLoopback, Message: "test"},
			isValidation:   true,
			expectedReason: BlockReasonLoopback,
		},
		{
			name:           "wrapped_validation_error",
			err:            errors.Join(errors.New("wrapped"), &ValidationError{Reason: BlockReasonPrivateNetwork, Message: "test"}),
			isValidation:   true,
			expectedReason: BlockReasonPrivateNetwork,
		},
		{
			name:           "non_validation_error",
			err:            errors.New("some error"),
			isValidation:   false,
			expectedReason: BlockReason(""),
		},
		{
			name:           "nil_error",
			err:            nil,
			isValidation:   false,
			expectedReason: BlockReason(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsValidationError(tt.err) != tt.isValidation {
				t.Fatalf("IsValidationError(%v) = %v, want %v", tt.err, !tt.isValidation, tt.isValidation)
			}
			if reason := GetBlockReason(tt.err); reason != tt.expectedReason {
				t.Fatalf("GetBlockReason(%v) = %v, want %v", tt.err, reason, tt.expectedReason)
			}
		})
	}
}

func TestNewSafeClientHasRedirectCheckAndProxyDisabled(t *testing.T) {
	validator := NewValidator()
	client := validator.NewSafeClient(30 * time.Second)

	if client.CheckRedirect == nil {
		t.Fatal("expected CheckRedirect to be set")
	}
	if client.Transport == nil {
		t.Fatal("expected Transport to be set")
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected Transport to be *http.Transport")
	}
	if transport.Proxy != nil {
		t.Fatal("expected Proxy to be nil (disabled)")
	}
}

func TestValidationErrorIsComparable(t *testing.T) {
	err1 := &ValidationError{Reason: BlockReasonLoopback, Message: "test", URL: "http://example.com"}
	err2 := &ValidationError{Reason: BlockReasonLoopback, Message: "different", URL: "http://other.com"}
	err3 := &ValidationError{Reason: BlockReasonPrivateNetwork, Message: "test", URL: "http://example.com"}

	if !errors.Is(err1, err2) {
		t.Fatal("errors with same Reason should match via errors.Is")
	}
	if errors.Is(err1, err3) {
		t.Fatal("errors with different Reason should not match via errors.Is")
	}
}

func TestValidateURLRejectsURLWithNoHostname(t *testing.T) {
	validator := NewValidator()
	err := validator.ValidateURL(context.Background(), "http:///path")
	if err == nil {
		t.Fatal("expected error for URL with no hostname")
	}
}

func TestValidateURLWithHttptestServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	validator := NewValidator(WithAllowPrivateNetworks(true))
	err := validator.ValidateURL(context.Background(), server.URL+"/test")
	if err != nil {
		t.Fatalf("expected no error for httptest server with allow-private, got %v", err)
	}
}
