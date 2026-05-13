package stages

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"iris/internal/constants"
	errpkg "iris/internal/error"
	"iris/internal/ssrf"
)

type FetchResult struct {
	Bytes      []byte
	MIMEType   string
	StatusCode int
}

type FetchConfig struct {
	Client                   *http.Client
	MaxBytes                 int
	UserAgent                string
	SSRFAllowPrivateNetworks bool
	Timeout                  time.Duration
}

func DefaultFetchConfig() FetchConfig {
	return FetchConfig{
		MaxBytes:  constants.MaxImageSize,
		UserAgent: constants.DefaultCrawlerUserAgent,
		Timeout:   constants.HTTPTimeout30s,
	}
}

func FetchImageBytes(ctx context.Context, rawURL string, cfg FetchConfig) (*FetchResult, error) {
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = constants.MaxImageSize
	}
	if strings.TrimSpace(cfg.UserAgent) == "" {
		cfg.UserAgent = constants.DefaultCrawlerUserAgent
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = constants.HTTPTimeout30s
	}

	validator := ssrf.NewValidator(ssrf.WithAllowPrivateNetworks(cfg.SSRFAllowPrivateNetworks))
	if err := validator.ValidateURL(ctx, rawURL); err != nil {
		return nil, fmt.Errorf("SSRF blocked: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set(constants.HeaderUserAgent, cfg.UserAgent)

	safeClient := validator.NewSafeClient(cfg.Timeout)
	if cfg.Client != nil {
		client := *cfg.Client
		if client.CheckRedirect == nil {
			client.CheckRedirect = validator.HTTPCheckRedirect
		}
		safeClient = &client
	}

	resp, err := safeClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch image url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch image url: status %d", resp.StatusCode)
	}
	contentType := strings.ToLower(resp.Header.Get(constants.HeaderContentType))
	if contentType != "" && !strings.HasPrefix(contentType, constants.MIMETypeImagePrefix) {
		return nil, errpkg.ErrUnsupportedContentType.ErrorWith(fmt.Errorf("content type: %s", contentType))
	}
	limited := io.LimitReader(resp.Body, int64(cfg.MaxBytes+1))
	buf, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read image bytes: %w", err)
	}
	if len(buf) > cfg.MaxBytes {
		return nil, errpkg.ErrImageExceedsLimit.ErrorWith(fmt.Errorf("size: %d bytes exceeds limit: %d bytes", len(buf), cfg.MaxBytes))
	}
	if contentType == "" {
		detected := http.DetectContentType(buf)
		if !strings.HasPrefix(detected, constants.MIMETypeImagePrefix) {
			return nil, errpkg.ErrUnsupportedContentType.ErrorWith(fmt.Errorf("detected content type: %s", detected))
		}
		contentType = detected
	}
	return &FetchResult{Bytes: buf, MIMEType: contentType, StatusCode: resp.StatusCode}, nil
}
