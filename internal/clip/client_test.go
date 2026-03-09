package clip

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	clipv1 "iris/internal/clip/clipv1"
	"iris/internal/constants"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

type testClipService struct {
	clipv1.UnimplementedClipServiceServer
	textEmbedding  []float32
	imageEmbedding []float32
	textErr        error
	imageErr       error
}

func (s *testClipService) EmbedText(_ context.Context, req *clipv1.EmbedTextRequest) (*clipv1.EmbedResponse, error) {
	if s.textErr != nil {
		return nil, s.textErr
	}
	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "text required")
	}
	return &clipv1.EmbedResponse{Embedding: s.textEmbedding, Dim: int32(len(s.textEmbedding))}, nil
}

func (s *testClipService) EmbedImage(_ context.Context, req *clipv1.EmbedImageRequest) (*clipv1.EmbedResponse, error) {
	if s.imageErr != nil {
		return nil, s.imageErr
	}
	if len(req.GetImageBytes()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "image required")
	}
	return &clipv1.EmbedResponse{Embedding: s.imageEmbedding, Dim: int32(len(s.imageEmbedding))}, nil
}

func TestNewClientRejectsEmptyTarget(t *testing.T) {
	_, err := NewClient("   ")
	if err == nil || !strings.Contains(err.Error(), "clip target is required") {
		t.Fatalf("expected empty target error, got %v", err)
	}
}

func TestClient_HealthCheck(t *testing.T) {
	client := newBufconnClient(t, &testClipService{}, grpc_health_v1.HealthCheckResponse_SERVING)
	defer client.Close()

	if err := client.HealthCheck(context.Background()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestClient_HealthCheckNotServing(t *testing.T) {
	client := newBufconnClient(t, &testClipService{}, grpc_health_v1.HealthCheckResponse_NOT_SERVING)
	defer client.Close()

	err := client.HealthCheck(context.Background())
	if err == nil || !strings.Contains(err.Error(), "NOT_SERVING") {
		t.Fatalf("expected not serving error, got %v", err)
	}
}

func TestClient_EmbedText(t *testing.T) {
	client := newBufconnClient(t, &testClipService{textEmbedding: []float32{1, 2}}, grpc_health_v1.HealthCheckResponse_SERVING)
	defer client.Close()

	emb, err := client.EmbedText(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(emb) != 2 || emb[0] != 1 {
		t.Fatalf("unexpected embedding: %#v", emb)
	}
}

func TestClient_EmbedImageBytes(t *testing.T) {
	t.Run("max size guard", func(t *testing.T) {
		client := newBufconnClient(t, &testClipService{}, grpc_health_v1.HealthCheckResponse_SERVING)
		defer client.Close()

		_, err := client.EmbedImageBytes(context.Background(), make([]byte, constants.MaxImageSize+1))
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("expected max-size error, got %v", err)
		}
	})

	t.Run("happy path", func(t *testing.T) {
		client := newBufconnClient(t, &testClipService{imageEmbedding: []float32{3, 4}}, grpc_health_v1.HealthCheckResponse_SERVING)
		defer client.Close()

		emb, err := client.EmbedImageBytes(context.Background(), []byte("data"))
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(emb) != 2 || emb[0] != 3 {
			t.Fatalf("unexpected embedding: %#v", emb)
		}
	})
}

func TestClient_EmbedImageURL(t *testing.T) {
	t.Run("invalid host blocked by validator", func(t *testing.T) {
		client := newBufconnClient(t, &testClipService{}, grpc_health_v1.HealthCheckResponse_SERVING)
		defer client.Close()

		_, err := client.EmbedImageURL(context.Background(), "http://broken.local:1234")
		if err == nil || !strings.Contains(err.Error(), "SSRF blocked") {
			t.Fatalf("expected SSRF validation failure, got %v", err)
		}
	})

	t.Run("loopback blocked by validator", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ts.Close()

		client := newBufconnClient(t, &testClipService{}, grpc_health_v1.HealthCheckResponse_SERVING)
		defer client.Close()

		_, err := client.EmbedImageURL(context.Background(), ts.URL)
		if err == nil || !strings.Contains(err.Error(), "SSRF blocked") {
			t.Fatalf("expected loopback SSRF failure, got %v", err)
		}
	})
}

func newBufconnClient(t *testing.T, svc clipv1.ClipServiceServer, healthStatus grpc_health_v1.HealthCheckResponse_ServingStatus) *Client {
	t.Helper()

	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	clipv1.RegisterClipServiceServer(server, svc)

	healthServer := health.NewServer()
	healthServer.SetServingStatus(serviceName, healthStatus)
	grpc_health_v1.RegisterHealthServer(server, healthServer)

	go func() {
		if err := server.Serve(listener); err != nil {
			t.Logf("grpc server stopped: %v", err)
		}
	}()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxGRPCMessageBytes),
			grpc.MaxCallSendMsgSize(maxGRPCMessageBytes),
		),
	)
	if err != nil {
		t.Fatalf("create grpc client: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
	})

	return &Client{
		target:  "passthrough:///bufnet",
		conn:    conn,
		service: clipv1.NewClipServiceClient(conn),
		health:  grpc_health_v1.NewHealthClient(conn),
	}
}
