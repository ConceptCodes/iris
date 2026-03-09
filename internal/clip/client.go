package clip

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	clipv1 "iris/internal/clip/clipv1"
	"iris/internal/constants"
	"iris/internal/ssrf"
	"iris/internal/tracing"
	"iris/pkg/models"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const (
	serviceName         = "clip.v1.ClipService"
	maxGRPCMessageBytes = constants.MaxImageSize + (1 << 20)
)

type Client struct {
	target  string
	conn    *grpc.ClientConn
	service clipv1.ClipServiceClient
	health  grpc_health_v1.HealthClient
}

var tracer = otel.Tracer("iris/clip")

func NewClient(target string) (*Client, error) {
	normalized, err := normalizeTarget(target)
	if err != nil {
		return nil, err
	}

	conn, err := grpc.NewClient(
		normalized,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxGRPCMessageBytes),
			grpc.MaxCallSendMsgSize(maxGRPCMessageBytes),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create grpc client: %w", err)
	}

	return &Client{
		target:  normalized,
		conn:    conn,
		service: clipv1.NewClipServiceClient(conn),
		health:  grpc_health_v1.NewHealthClient(conn),
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Client) EmbedText(ctx context.Context, text string) (models.Embedding, error) {
	ctx, span := tracing.StartSpanWithAttributes(ctx, tracer, "EmbedText",
		[]attribute.KeyValue{
			attribute.Int("text_length", len(text)),
		},
	)
	defer span.End()

	resp, err := c.service.EmbedText(ctx, &clipv1.EmbedTextRequest{Text: text})
	if err != nil {
		err = wrapRPCError("embed text", err)
		tracing.AddErrorToSpan(span, err)
		return nil, err
	}
	return resp.Embedding, nil
}

func (c *Client) EmbedImageBytes(ctx context.Context, imageBytes []byte) (models.Embedding, error) {
	ctx, span := tracing.StartSpanWithAttributes(ctx, tracer, "EmbedImageBytes",
		[]attribute.KeyValue{
			attribute.Int("image_size", len(imageBytes)),
		},
	)
	defer span.End()

	if len(imageBytes) > constants.MaxImageSize {
		err := fmt.Errorf("image exceeds %d bytes limit", constants.MaxImageSize)
		tracing.AddErrorToSpan(span, err)
		return nil, err
	}

	resp, err := c.service.EmbedImage(ctx, &clipv1.EmbedImageRequest{ImageBytes: imageBytes})
	if err != nil {
		err = wrapRPCError("embed image", err)
		tracing.AddErrorToSpan(span, err)
		return nil, err
	}
	return resp.Embedding, nil
}

func (c *Client) EmbedImageURL(ctx context.Context, imageURL string) (models.Embedding, error) {
	validator := ssrf.NewValidator()
	if err := validator.ValidateURL(ctx, imageURL); err != nil {
		return nil, fmt.Errorf("SSRF blocked: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	safeClient := validator.NewSafeClient(constants.HTTPTimeout30s)
	resp, err := safeClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch image url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch image url: status %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, constants.MaxImageSize+1)
	var buf bytes.Buffer
	n, err := buf.ReadFrom(limited)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if n > constants.MaxImageSize {
		return nil, fmt.Errorf("image exceeds %d bytes limit", constants.MaxImageSize)
	}

	return c.EmbedImageBytes(ctx, buf.Bytes())
}

func (c *Client) HealthCheck(ctx context.Context) error {
	resp, err := c.health.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: serviceName})
	if err != nil {
		return wrapRPCError("health check", err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("health check failed: status %s", resp.GetStatus().String())
	}
	return nil
}

func normalizeTarget(target string) (string, error) {
	trimmed := strings.TrimSpace(target)
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimSuffix(trimmed, "/")
	if trimmed == "" {
		return "", fmt.Errorf("clip target is required")
	}
	return trimmed, nil
}

func wrapRPCError(operation string, err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if st.Code() == codes.OK {
		return nil
	}
	return fmt.Errorf("%s: %s", operation, st.Message())
}
