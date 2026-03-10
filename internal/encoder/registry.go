package encoder

import (
	"context"
	"fmt"
	"slices"

	"iris/pkg/models"
)

type Client interface {
	EmbedText(ctx context.Context, text string) (models.Embedding, error)
	EmbedImageBytes(ctx context.Context, imageBytes []byte) (models.Embedding, error)
	EmbedImageURL(ctx context.Context, imageURL string) (models.Embedding, error)
}

type Registry struct {
	defaultName models.Encoder
	clients     map[models.Encoder]Client
	names       []models.Encoder
}

func NewRegistry(defaultName models.Encoder, clients map[models.Encoder]Client) (*Registry, error) {
	normalizedClients := make(map[models.Encoder]Client, len(clients))
	names := make([]models.Encoder, 0, len(clients))
	for name, client := range clients {
		normalized := models.NormalizeEncoder(name)
		if normalized == "" || client == nil {
			continue
		}
		if _, exists := normalizedClients[normalized]; exists {
			return nil, fmt.Errorf("duplicate encoder configured: %s", normalized)
		}
		normalizedClients[normalized] = client
		names = append(names, normalized)
	}
	if len(normalizedClients) == 0 {
		return nil, fmt.Errorf("at least one encoder client is required")
	}
	slices.Sort(names)

	defaultName = models.NormalizeEncoder(defaultName)
	if defaultName == "" {
		defaultName = names[0]
	}
	if _, ok := normalizedClients[defaultName]; !ok {
		return nil, fmt.Errorf("default encoder %q is not configured", defaultName)
	}

	return &Registry{
		defaultName: defaultName,
		clients:     normalizedClients,
		names:       names,
	}, nil
}

func (r *Registry) Default() models.Encoder {
	return r.defaultName
}

func (r *Registry) Names() []models.Encoder {
	return append([]models.Encoder(nil), r.names...)
}

func (r *Registry) Resolve(name models.Encoder) (models.Encoder, Client, error) {
	if r == nil {
		return "", nil, fmt.Errorf("encoder registry is nil")
	}
	normalized := models.NormalizeEncoder(name)
	if normalized == "" {
		normalized = r.defaultName
	}
	client, ok := r.clients[normalized]
	if !ok {
		return "", nil, fmt.Errorf("encoder %q is not configured", normalized)
	}
	return normalized, client, nil
}

func (r *Registry) All() map[models.Encoder]Client {
	out := make(map[models.Encoder]Client, len(r.clients))
	for name, client := range r.clients {
		out[name] = client
	}
	return out
}
