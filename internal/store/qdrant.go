package store

import (
	"context"
	"fmt"
	"time"

	"iris/pkg/models"
	pb "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const collectionName = "images"

type QdrantStore struct {
	conn        *grpc.ClientConn
	collections pb.CollectionsClient
	points      pb.PointsClient
	dim         uint64
}

func NewQdrantStore(addr string, dim int, connectTimeout time.Duration) (*QdrantStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to qdrant: %w", err)
	}
	store := &QdrantStore{
		conn:        conn,
		collections: pb.NewCollectionsClient(conn),
		points:      pb.NewPointsClient(conn),
		dim:         uint64(dim),
	}
	if err := store.ensureCollection(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ensure collection: %w", err)
	}
	return store, nil
}

func (s *QdrantStore) ensureCollection(ctx context.Context) error {
	resp, err := s.collections.List(ctx, &pb.ListCollectionsRequest{})
	if err != nil {
		return fmt.Errorf("list collections: %w", err)
	}
	for _, c := range resp.GetCollections() {
		if c.Name == collectionName {
			return nil
		}
	}
	_, err = s.collections.Create(ctx, &pb.CreateCollection{
		CollectionName: collectionName,
		VectorsConfig: &pb.VectorsConfig{Config: &pb.VectorsConfig_Params{
			Params: &pb.VectorParams{
				Size:     s.dim,
				Distance: pb.Distance_Cosine,
			},
		}},
	})
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}
	return nil
}

func (s *QdrantStore) Upsert(ctx context.Context, record models.ImageRecord, embedding models.Embedding) (string, error) {
	id := record.ID
	point := &pb.PointStruct{
		Id:      &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: id}},
		Vectors: &pb.Vectors{VectorsOptions: &pb.Vectors_Vector{Vector: &pb.Vector{Data: embedding}}},
		Payload: s.recordToPayload(record),
	}
	_, err := s.points.Upsert(ctx, &pb.UpsertPoints{
		CollectionName: collectionName,
		Points:         []*pb.PointStruct{point},
	})
	if err != nil {
		return "", fmt.Errorf("upsert: %w", err)
	}
	return id, nil
}

func (s *QdrantStore) Search(ctx context.Context, embedding models.Embedding, topK int, filters map[string]string) ([]models.SearchResult, error) {
	var filter *pb.Filter
	if len(filters) > 0 {
		conditions := make([]*pb.Condition, 0, len(filters))
		for k, v := range filters {
			conditions = append(conditions, &pb.Condition{
				ConditionOneOf: &pb.Condition_Field{
					Field: &pb.FieldCondition{
						Key: k,
						Match: &pb.Match{
							MatchValue: &pb.Match_Keyword{Keyword: v},
						},
					},
				},
			})
		}
		filter = &pb.Filter{Must: conditions}
	}
	resp, err := s.points.Search(ctx, &pb.SearchPoints{
		CollectionName: collectionName,
		Vector:         embedding,
		Limit:          uint64(topK),
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
		Filter:         filter,
	})
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	results := make([]models.SearchResult, 0, len(resp.GetResult()))
	for _, hit := range resp.GetResult() {
		record := s.payloadToRecord(hit.Payload)
		results = append(results, models.SearchResult{
			Record: record,
			Score:  hit.Score,
		})
	}
	return results, nil
}

func (s *QdrantStore) Delete(ctx context.Context, id string) error {
	_, err := s.points.Delete(ctx, &pb.DeletePoints{
		CollectionName: collectionName,
		Points: &pb.PointsSelector{
			PointsSelectorOneOf: &pb.PointsSelector_Points{
				Points: &pb.PointsIdsList{
					Ids: []*pb.PointId{
						{PointIdOptions: &pb.PointId_Uuid{Uuid: id}},
					},
				},
			},
		},
	})
	return err
}

func (s *QdrantStore) GetVector(ctx context.Context, id string) (models.Embedding, error) {
	resp, err := s.points.Get(ctx, &pb.GetPoints{
		CollectionName: collectionName,
		Ids: []*pb.PointId{
			{PointIdOptions: &pb.PointId_Uuid{Uuid: id}},
		},
		WithVectors: &pb.WithVectorsSelector{SelectorOptions: &pb.WithVectorsSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, fmt.Errorf("get point: %w", err)
	}
	results := resp.GetResult()
	if len(results) == 0 {
		return nil, fmt.Errorf("point not found: %s", id)
	}
	vecs := results[0].GetVectors()
	if vecs == nil {
		return nil, fmt.Errorf("no vectors in response")
	}
	if v := vecs.GetVector(); v != nil {
		return v.Data, nil
	}
	return nil, fmt.Errorf("no vector data in response")
}

func (s *QdrantStore) recordToPayload(record models.ImageRecord) map[string]*pb.Value {
	payload := map[string]*pb.Value{
		"id":       {Kind: &pb.Value_StringValue{StringValue: record.ID}},
		"url":      {Kind: &pb.Value_StringValue{StringValue: record.URL}},
		"filename": {Kind: &pb.Value_StringValue{StringValue: record.Filename}},
	}
	if len(record.Tags) > 0 {
		tags := make([]*pb.Value, len(record.Tags))
		for i, t := range record.Tags {
			tags[i] = &pb.Value{Kind: &pb.Value_StringValue{StringValue: t}}
		}
		payload["tags"] = &pb.Value{Kind: &pb.Value_ListValue{ListValue: &pb.ListValue{Values: tags}}}
	}
	for k, v := range record.Meta {
		payload["meta_"+k] = &pb.Value{Kind: &pb.Value_StringValue{StringValue: v}}
	}
	return payload
}

func (s *QdrantStore) payloadToRecord(payload map[string]*pb.Value) models.ImageRecord {
	record := models.ImageRecord{
		Meta: make(map[string]string),
	}
	if v, ok := payload["id"]; ok {
		if sv, ok := v.Kind.(*pb.Value_StringValue); ok {
			record.ID = sv.StringValue
		}
	}
	if v, ok := payload["url"]; ok {
		if sv, ok := v.Kind.(*pb.Value_StringValue); ok {
			record.URL = sv.StringValue
		}
	}
	if v, ok := payload["filename"]; ok {
		if sv, ok := v.Kind.(*pb.Value_StringValue); ok {
			record.Filename = sv.StringValue
		}
	}
	if v, ok := payload["tags"]; ok {
		if lv, ok := v.Kind.(*pb.Value_ListValue); ok {
			for _, tag := range lv.ListValue.Values {
				if sv, ok := tag.Kind.(*pb.Value_StringValue); ok {
					record.Tags = append(record.Tags, sv.StringValue)
				}
			}
		}
	}
	for k, v := range payload {
		if len(k) > 5 && k[:5] == "meta_" {
			if sv, ok := v.Kind.(*pb.Value_StringValue); ok {
				record.Meta[k[5:]] = sv.StringValue
			}
		}
	}
	return record
}

func (s *QdrantStore) Close() error {
	return s.conn.Close()
}
