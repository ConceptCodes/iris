package metadata

import (
	"context"
	"fmt"
	"strings"
	"time"

	"iris/internal/constants"
	"iris/internal/metadata/metadatav1"
	"iris/pkg/models"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

const (
	serviceName         = "metadata.v1.MetadataService"
	maxGRPCMessageBytes = constants.MaxImageSize + (1 << 20)
)

type Client struct {
	target  string
	conn    *grpc.ClientConn
	service metadatav1.MetadataServiceClient
	health  grpc_health_v1.HealthClient
}

func NewClient(target string, timeout time.Duration) *Client {
	normalized := normalizeTarget(target)
	if normalized == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
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
		return nil
	}
	_ = timeout
	return &Client{
		target:  normalized,
		conn:    conn,
		service: metadatav1.NewMetadataServiceClient(conn),
		health:  grpc_health_v1.NewHealthClient(conn),
	}
}

func (c *Client) Enrich(ctx context.Context, imageBytes []byte, record models.ImageRecord) (Result, error) {
	if c == nil {
		return Result{}, nil
	}
	if len(imageBytes) > constants.MaxImageSize {
		return Result{}, fmt.Errorf("image exceeds %d bytes limit", constants.MaxImageSize)
	}
	resp, err := c.service.AnalyzeImage(ctx, &metadatav1.AnalyzeImageRequest{
		ImageBytes: imageBytes,
		Filename:   record.Filename,
	})
	if err != nil {
		return Result{}, wrapRPCError("analyze image", err)
	}

	meta := make(map[string]string)
	if caption := strings.TrimSpace(resp.GetCaption()); caption != "" {
		meta["caption"] = caption
	}
	if ocr := strings.TrimSpace(resp.GetOcrText()); ocr != "" {
		meta["ocr_text"] = ocr
	}

	return Result{
		Tags: resp.GetTags(),
		Meta: meta,
	}, nil
}

func (c *Client) HealthCheck(ctx context.Context) error {
	if c == nil {
		return nil
	}
	resp, err := c.health.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: serviceName})
	if err != nil {
		return wrapRPCError("health check", err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("health check failed: status %s", resp.GetStatus().String())
	}
	return nil
}

func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func normalizeTarget(target string) string {
	trimmed := strings.TrimSpace(target)
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimPrefix(trimmed, "https://")
	trimmed = strings.TrimSuffix(trimmed, "/")
	return trimmed
}

func wrapRPCError(operation string, err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if st.Code() == codes.OK {
		return nil
	}
	return fmt.Errorf("%s: %s: %w", operation, st.Message(), err)
}

var _ Enricher = (*Client)(nil)
