package store

import (
	"context"
	"testing"
	"time"

	pb "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"iris/internal/constants"
	"iris/pkg/models"
)

func TestNewQdrantStore_Timeout(t *testing.T) {
	// Attempt to connect to an unroutable IP
	_, err := NewQdrantStore("192.0.2.1:1234", 512, 50*time.Millisecond)
	if err == nil {
		t.Errorf("expected connection timeout error")
	}
}

func TestQdrantStore_PayloadRoundtrip(t *testing.T) {
	record := models.ImageRecord{
		ID:       "test-id",
		URL:      "http://x.com",
		Filename: "x.jpg",
		Tags:     []string{"a", "b"},
		Meta: map[string]string{
			"color": "red",
		},
	}
	s := &QdrantStore{}
	payload := s.recordToPayload(record)
	roundtrip := s.payloadToRecord(payload)

	if roundtrip.ID != record.ID || roundtrip.URL != record.URL || roundtrip.Filename != record.Filename || len(roundtrip.Tags) != 2 || roundtrip.Meta["color"] != "red" {
		t.Errorf("roundtrip mismatch: got %+v", roundtrip)
	}
}

type mockCollectionsClient struct {
	pb.CollectionsClient
	listResp  *pb.ListCollectionsResponse
	listErr   error
	createErr error
	created   bool
}

func (m *mockCollectionsClient) List(ctx context.Context, in *pb.ListCollectionsRequest, opts ...grpc.CallOption) (*pb.ListCollectionsResponse, error) {
	return m.listResp, m.listErr
}
func (m *mockCollectionsClient) Create(ctx context.Context, in *pb.CreateCollection, opts ...grpc.CallOption) (*pb.CollectionOperationResponse, error) {
	m.created = true
	return nil, m.createErr
}

func TestQdrantStore_ensureCollection(t *testing.T) {
	t.Run("existing", func(t *testing.T) {
		mockC := &mockCollectionsClient{
			listResp: &pb.ListCollectionsResponse{
				Collections: []*pb.CollectionDescription{
					{Name: constants.CollectionNameImages},
				},
			},
		}
		s := &QdrantStore{collections: mockC}
		err := s.ensureCollection(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if mockC.created {
			t.Errorf("should not call create")
		}
	})

	t.Run("create path", func(t *testing.T) {
		mockC := &mockCollectionsClient{
			listResp: &pb.ListCollectionsResponse{Collections: nil},
		}
		s := &QdrantStore{collections: mockC}
		err := s.ensureCollection(context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !mockC.created {
			t.Errorf("should have called create")
		}
	})
}

// Full interface implementations for mockPointsClient below

type mockPointsClient struct {
	pb.PointsClient
	upsertErr  error
	searchResp *pb.SearchResponse
	searchErr  error
	deleteErr  error
	getResp    *pb.GetResponse
	getErr     error
	scrollResp *pb.ScrollResponse
	scrollErr  error

	lastTopK   uint64
	lastFilter *pb.Filter
}

func (m *mockPointsClient) Upsert(ctx context.Context, in *pb.UpsertPoints, opts ...grpc.CallOption) (*pb.PointsOperationResponse, error) {
	return nil, m.upsertErr
}
func (m *mockPointsClient) Search(ctx context.Context, in *pb.SearchPoints, opts ...grpc.CallOption) (*pb.SearchResponse, error) {
	m.lastTopK = in.Limit
	m.lastFilter = in.Filter
	return m.searchResp, m.searchErr
}
func (m *mockPointsClient) Delete(ctx context.Context, in *pb.DeletePoints, opts ...grpc.CallOption) (*pb.PointsOperationResponse, error) {
	return nil, m.deleteErr
}
func (m *mockPointsClient) Get(ctx context.Context, in *pb.GetPoints, opts ...grpc.CallOption) (*pb.GetResponse, error) {
	return m.getResp, m.getErr
}

func (m *mockPointsClient) Scroll(ctx context.Context, in *pb.ScrollPoints, opts ...grpc.CallOption) (*pb.ScrollResponse, error) {
	return m.scrollResp, m.scrollErr
}

func TestQdrantStore_DataOperations(t *testing.T) {
	t.Run("Upsert success", func(t *testing.T) {
		s := &QdrantStore{points: &mockPointsClient{upsertErr: nil}}
		id, err := s.Upsert(context.Background(), models.ImageRecord{ID: "x"}, models.Embedding{1.0})
		if id != "x" || err != nil {
			t.Errorf("expected x and no error")
		}
	})

	t.Run("Delete success", func(t *testing.T) {
		s := &QdrantStore{points: &mockPointsClient{deleteErr: nil}}
		err := s.Delete(context.Background(), "x")
		if err != nil {
			t.Errorf("expected no error")
		}
	})

	t.Run("Search correct filter mapping and limits", func(t *testing.T) {
		mc := &mockPointsClient{
			searchResp: &pb.SearchResponse{Result: []*pb.ScoredPoint{}},
		}
		s := &QdrantStore{points: mc}

		_, err := s.Search(context.Background(), models.Embedding{1.0}, 42, map[string]string{"k": "v"})
		if err != nil {
			t.Errorf("expected no err")
		}
		if mc.lastTopK != 42 {
			t.Errorf("topK not mapped correctly")
		}
		if mc.lastFilter == nil || len(mc.lastFilter.Must) != 1 {
			t.Fatalf("filter not mapped correctly")
		}
		cond := mc.lastFilter.Must[0].GetField()
		if cond.Key != "k" || cond.Match.GetKeyword() != "v" {
			t.Errorf("filter mismatch")
		}
	})

	t.Run("GetVector missing point", func(t *testing.T) {
		mc := &mockPointsClient{
			getResp: &pb.GetResponse{Result: nil},
		}
		s := &QdrantStore{points: mc}
		_, err := s.GetVector(context.Background(), "x")
		if err == nil {
			t.Errorf("expected error for missing point")
		}
	})

	t.Run("GetVector success", func(t *testing.T) {
		mc := &mockPointsClient{
			getResp: &pb.GetResponse{Result: []*pb.RetrievedPoint{
				{Vectors: &pb.Vectors{VectorsOptions: &pb.Vectors_Vector{Vector: &pb.Vector{Data: []float32{4.2}}}}},
			}},
		}
		s := &QdrantStore{points: mc}
		vec, err := s.GetVector(context.Background(), "x")
		if err != nil || vec[0] != 4.2 {
			t.Errorf("expected success with 4.2")
		}
	})

	t.Run("FindIDByMeta", func(t *testing.T) {
		mc := &mockPointsClient{
			scrollResp: &pb.ScrollResponse{Result: []*pb.RetrievedPoint{
				{Payload: map[string]*pb.Value{"id": {Kind: &pb.Value_StringValue{StringValue: "point-id"}}}},
			}},
		}
		s := &QdrantStore{points: mc}
		id, ok, err := s.FindIDByMeta(context.Background(), "meta_content_sha256", "hash")
		if err != nil || !ok || id != "point-id" {
			t.Fatalf("expected id from scroll, got %q ok=%v err=%v", id, ok, err)
		}
	})
}
