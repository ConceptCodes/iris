// Package ssrf provides Server-Side Request Forgery (SSRF) protection
// by validating URLs and blocking access to private networks and dangerous endpoints.
//
// The validator performs DNS resolution and checks all resolved IP addresses
// against a comprehensive blocklist of private and special-use IP ranges.
package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
)

// BlockReason identifies why a URL was blocked.
type BlockReason string

const (
	// BlockReasonInvalidScheme indicates the URL does not use HTTP or HTTPS.
	BlockReasonInvalidScheme BlockReason = "invalid_scheme"
	// BlockReasonLoopback indicates the resolved IP is in the loopback range.
	BlockReasonLoopback BlockReason = "loopback"
	// BlockReasonLinkLocal indicates the resolved IP is in the link-local range.
	BlockReasonLinkLocal BlockReason = "link_local"
	// BlockReasonPrivateNetwork indicates the resolved IP is in a private network range (RFC1918).
	BlockReasonPrivateNetwork BlockReason = "private_network"
	// BlockReasonMulticast indicates the resolved IP is in a multicast range.
	BlockReasonMulticast BlockReason = "multicast"
	// BlockReasonUnspecified indicates the resolved IP is the unspecified address (0.0.0.0 or ::).
	BlockReasonUnspecified BlockReason = "unspecified"
	// BlockReasonMetadataEndpoint indicates the resolved IP is a cloud metadata endpoint.
	BlockReasonMetadataEndpoint BlockReason = "metadata_endpoint"
)

var (
	// ErrInvalidScheme is returned when the URL scheme is not HTTP or HTTPS.
	ErrInvalidScheme = &ValidationError{Reason: BlockReasonInvalidScheme, Message: "only HTTP and HTTPS schemes are allowed"}
	// ErrLoopback is returned when the URL resolves to a loopback address.
	ErrLoopback = &ValidationError{Reason: BlockReasonLoopback, Message: "access to loopback address is blocked"}
	// ErrLinkLocal is returned when the URL resolves to a link-local address.
	ErrLinkLocal = &ValidationError{Reason: BlockReasonLinkLocal, Message: "access to link-local address is blocked"}
	// ErrPrivateNetwork is returned when the URL resolves to a private network address.
	ErrPrivateNetwork = &ValidationError{Reason: BlockReasonPrivateNetwork, Message: "access to private network is blocked"}
	// ErrMulticast is returned when the URL resolves to a multicast address.
	ErrMulticast = &ValidationError{Reason: BlockReasonMulticast, Message: "access to multicast address is blocked"}
	// ErrUnspecified is returned when the URL resolves to an unspecified address.
	ErrUnspecified = &ValidationError{Reason: BlockReasonUnspecified, Message: "access to unspecified address is blocked"}
	// ErrMetadataEndpoint is returned when the URL resolves to a cloud metadata endpoint.
	ErrMetadataEndpoint = &ValidationError{Reason: BlockReasonMetadataEndpoint, Message: "access to cloud metadata endpoint is blocked"}
)

// ValidationError represents an SSRF protection error with a specific block reason.
// It implements the error interface and supports errors.Is and errors.As for type checking.
type ValidationError struct {
	Reason  BlockReason
	Message string
	IP      net.IP
	URL     string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.IP != nil {
		return fmt.Sprintf("%s: %s (%s)", e.Message, e.URL, e.IP.String())
	}
	return fmt.Sprintf("%s: %s", e.Message, e.URL)
}

// Is allows error comparison with errors.Is.
func (e *ValidationError) Is(target error) bool {
	t, ok := target.(*ValidationError)
	if !ok {
		return false
	}
	return e.Reason == t.Reason
}

// Validator performs SSRF protection by validating URLs against blocked IP ranges.
type Validator struct {
	resolver             *net.Resolver
	blockedNetworks      []*net.IPNet
	metadataEndpoints    []net.IP
	allowPrivateNetworks bool
}

// ValidatorOption configures a Validator.
type ValidatorOption func(*Validator)

// WithAllowPrivateNetworks allows private network addresses (RFC1918, loopback, link-local).
// This should only be used in development/testing environments.
func WithAllowPrivateNetworks(allow bool) ValidatorOption {
	return func(v *Validator) {
		v.allowPrivateNetworks = allow
	}
}

// NewValidator creates a new SSRF validator with default blocked IP ranges.
func NewValidator(opts ...ValidatorOption) *Validator {
	v := &Validator{
		resolver: net.DefaultResolver,
	}
	for _, opt := range opts {
		opt(v)
	}

	// Initialize IPv4 blocked ranges
	v.blockedNetworks = append(v.blockedNetworks,
		mustParseCIDR("127.0.0.0/8"),    // Loopback
		mustParseCIDR("169.254.0.0/16"), // Link-local
		mustParseCIDR("10.0.0.0/8"),     // Private (RFC1918)
		mustParseCIDR("172.16.0.0/12"),  // Private (RFC1918)
		mustParseCIDR("192.168.0.0/16"), // Private (RFC1918)
		mustParseCIDR("224.0.0.0/4"),    // Multicast
		mustParseCIDR("0.0.0.0/32"),     // Unspecified
	)

	// Initialize IPv6 blocked ranges
	v.blockedNetworks = append(v.blockedNetworks,
		mustParseCIDR("::1/128"),   // IPv6 loopback
		mustParseCIDR("fc00::/7"),  // IPv6 unique local (private)
		mustParseCIDR("fe80::/10"), // IPv6 link-local
		mustParseCIDR("ff00::/8"),  // IPv6 multicast
		mustParseCIDR("::/128"),    // IPv6 unspecified
	)

	// Initialize cloud metadata endpoints
	v.metadataEndpoints = append(v.metadataEndpoints,
		net.ParseIP("169.254.169.254"), // AWS/GCP/Azure metadata
	)

	return v
}

// ValidateURL validates a URL and returns an error if it resolves to a blocked IP address.
// It performs the following checks:
//  1. Parses the URL and ensures it uses HTTP or HTTPS scheme
//  2. Resolves the hostname to IP addresses via DNS
//  3. Checks each resolved IP against the blocklist of private/special-use ranges
//  4. Returns a ValidationError if any IP is blocked
//
// The context is used for DNS resolution and can be cancelled to timeout the operation.
func (v *Validator) ValidateURL(ctx context.Context, rawURL string) error {
	// Parse the URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Validate scheme
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return &ValidationError{
			Reason:  BlockReasonInvalidScheme,
			Message: ErrInvalidScheme.Message,
			URL:     rawURL,
		}
	}

	// Get hostname
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL has no hostname: %s", rawURL)
	}

	// Resolve hostname to IPs
	ips, err := v.resolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return fmt.Errorf("DNS resolution failed for %s: %w", hostname, err)
	}

	if len(ips) == 0 {
		return fmt.Errorf("no IP addresses resolved for %s", hostname)
	}

	// Check each resolved IP
	for _, ipAddr := range ips {
		ip := ipAddr.IP

		// Check against metadata endpoints
		for _, metadataIP := range v.metadataEndpoints {
			if ip.Equal(metadataIP) {
				return &ValidationError{
					Reason:  BlockReasonMetadataEndpoint,
					Message: ErrMetadataEndpoint.Message,
					IP:      ip,
					URL:     rawURL,
				}
			}
		}

		// Check against blocked networks
		for _, network := range v.blockedNetworks {
			if network.Contains(ip) {
				var reason BlockReason
				var message string

				if isLoopback(ip) {
					if v.allowPrivateNetworks {
						continue
					}
					reason = BlockReasonLoopback
					message = ErrLoopback.Message
				} else if isLinkLocal(ip) {
					if v.allowPrivateNetworks {
						continue
					}
					reason = BlockReasonLinkLocal
					message = ErrLinkLocal.Message
				} else if isPrivateNetwork(ip) {
					if v.allowPrivateNetworks {
						continue
					}
					reason = BlockReasonPrivateNetwork
					message = ErrPrivateNetwork.Message
				} else if isMulticast(ip) {
					reason = BlockReasonMulticast
					message = ErrMulticast.Message
				} else if isUnspecified(ip) {
					if v.allowPrivateNetworks {
						continue
					}
					reason = BlockReasonUnspecified
					message = ErrUnspecified.Message
				} else {
					reason = BlockReasonPrivateNetwork
					message = "access to this IP range is blocked"
				}

				return &ValidationError{
					Reason:  reason,
					Message: message,
					IP:      ip,
					URL:     rawURL,
				}
			}
		}
	}

	return nil
}

// ValidateRedirect is a convenience wrapper for ValidateURL intended for use
// with HTTP redirect validation. It delegates directly to ValidateURL.
//
// This is useful when setting http.Client.CheckRedirect to validate redirect URLs
// before following them.
func (v *Validator) ValidateRedirect(ctx context.Context, rawURL string) error {
	return v.ValidateURL(ctx, rawURL)
}

// mustParseCIDR parses a CIDR string and panics if invalid.
// This is used for static initialization of known-good CIDR ranges.
func mustParseCIDR(cidr string) *net.IPNet {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(fmt.Sprintf("invalid CIDR %s: %v", cidr, err))
	}
	return network
}

// isLoopback checks if an IP address is in the loopback range.
func isLoopback(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	// IPv4 loopback: 127.0.0.0/8
	if ip.To4() != nil && ip[0] == 127 {
		return true
	}
	return false
}

// isLinkLocal checks if an IP address is in the link-local range.
func isLinkLocal(ip net.IP) bool {
	if ip.IsLinkLocalUnicast() {
		return true
	}
	// IPv4 link-local: 169.254.0.0/16
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4[0] == 169 && ipv4[1] == 254
	}
	return false
}

// isPrivateNetwork checks if an IP address is in a private network range (RFC1918).
func isPrivateNetwork(ip net.IP) bool {
	if ip.IsPrivate() {
		return true
	}
	// IPv4 private ranges: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4[0] == 10 ||
			(ipv4[0] == 172 && ipv4[1] >= 16 && ipv4[1] <= 31) ||
			(ipv4[0] == 192 && ipv4[1] == 168)
	}
	return false
}

// isMulticast checks if an IP address is in a multicast range.
func isMulticast(ip net.IP) bool {
	if ip.IsMulticast() {
		return true
	}
	// IPv4 multicast: 224.0.0.0/4
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4[0] >= 224 && ipv4[0] <= 239
	}
	return false
}

// isUnspecified checks if an IP address is the unspecified address.
func isUnspecified(ip net.IP) bool {
	if ip.IsUnspecified() {
		return true
	}
	// IPv4 unspecified: 0.0.0.0
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4[0] == 0 && ipv4[1] == 0 && ipv4[2] == 0 && ipv4[3] == 0
	}
	// IPv6 unspecified: ::
	if ipv6 := ip.To16(); ipv6 != nil {
		for i := 0; i < len(ipv6); i++ {
			if ipv6[i] != 0 {
				return false
			}
		}
		return true
	}
	return false
}

// IsValidationError checks if an error is a ValidationError.
// This is a convenience function for errors.As checking.
func IsValidationError(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

// GetBlockReason extracts the BlockReason from an error if it is a ValidationError.
// Returns the empty string if the error is not a ValidationError.
func GetBlockReason(err error) BlockReason {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve.Reason
	}
	return ""
}
