// Package encoder provides a factory and registry for managing multiple
// vision encoder clients (CLIP, SigLIP2) with runtime encoder selection.
package encoder

import (
	"fmt"

	"iris/config"
	"iris/internal/clip"
	"iris/pkg/models"
)

func NewRegistryFromConfig(cfg config.Shared) (*Registry, func(), error) {
	clients := make(map[models.Encoder]Client)
	cleanups := make([]func(), 0, 2)

	clipClient, err := clip.NewClient(cfg.ClipAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("create clip client: %w", err)
	}
	clients[models.EncoderCLIP] = clipClient
	cleanups = append(cleanups, func() { _ = clipClient.Close() })

	if cfg.SigLIP2Addr != "" {
		siglipClient, err := clip.NewClient(cfg.SigLIP2Addr)
		if err != nil {
			for _, cleanup := range cleanups {
				cleanup()
			}
			return nil, nil, fmt.Errorf("create siglip2 client: %w", err)
		}
		clients[models.EncoderSigLIP2] = siglipClient
		cleanups = append(cleanups, func() { _ = siglipClient.Close() })
	}

	registry, err := NewRegistry(cfg.DefaultSearchEncoder, clients)
	if err != nil {
		for _, cleanup := range cleanups {
			cleanup()
		}
		return nil, nil, err
	}

	return registry, func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}, nil
}
