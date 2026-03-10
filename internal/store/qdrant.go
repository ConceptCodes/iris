package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	pb "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"iris/internal/constants"
	"iris/internal/tracing"
	"iris/pkg/models"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type QdrantStore struct {
	conn        *grpc.ClientConn
	collections pb.CollectionsClient
	points      pb.PointsClient
	dims        map[models.Encoder]uint64
}

var tracer = otel.Tracer("iris/qdrant")

func NewQdrantStore(addr string, dim int, connectTimeout time.Duration) (*QdrantStore, error) {
	return NewQdrantStoreWithEncoders(addr, map[models.Encoder]int{
		models.EncoderCLIP: dim,
	}, connectTimeout)
}

func NewQdrantStoreWithEncoders(addr string, dims map[models.Encoder]int, connectTimeout time.Duration) (*QdrantStore, error) {
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
		dims:        normalizeDims(dims),
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
		if c.Name == constants.CollectionNameImages {
			return nil
		}
	}
	_, err = s.collections.Create(ctx, &pb.CreateCollection{
		CollectionName: constants.CollectionNameImages,
		VectorsConfig: &pb.VectorsConfig{Config: &pb.VectorsConfig_ParamsMap{
			ParamsMap: &pb.VectorParamsMap{Map: s.vectorParamsMap()},
		}},
	})
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}
	return nil
}

func (s *QdrantStore) Upsert(ctx context.Context, record models.ImageRecord, embeddings models.Embeddings) (string, error) {
	ctx, span := tracing.StartSpanWithAttributes(ctx, tracer, "Upsert",
		[]attribute.KeyValue{
			attribute.String("id", record.ID),
			attribute.String("url", record.URL),
			attribute.Int("tags_count", len(record.Tags)),
			attribute.Int("encoders_count", len(embeddings)),
		},
	)
	defer span.End()

	id := record.ID
	namedVectors := make(map[string]*pb.Vector, len(embeddings))
	for name, embedding := range embeddings {
		namedVectors[string(name)] = &pb.Vector{Data: embedding}
	}
	point := &pb.PointStruct{
		Id:      &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: id}},
		Vectors: &pb.Vectors{VectorsOptions: &pb.Vectors_Vectors{Vectors: &pb.NamedVectors{Vectors: namedVectors}}},
		Payload: s.recordToPayload(record),
	}
	_, err := s.points.Upsert(ctx, &pb.UpsertPoints{
		CollectionName: constants.CollectionNameImages,
		Points:         []*pb.PointStruct{point},
	})
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return "", fmt.Errorf("upsert: %w", err)
	}
	return id, nil
}

func (s *QdrantStore) Search(ctx context.Context, enc models.Encoder, embedding models.Embedding, topK int, filters map[string]string) ([]models.SearchResult, error) {
	ctx, span := tracing.StartSpanWithAttributes(ctx, tracer, "Search",
		[]attribute.KeyValue{
			attribute.Int("top_k", topK),
			attribute.Int("embedding_dim", len(embedding)),
			attribute.Int("filters_count", len(filters)),
		},
	)
	defer span.End()

	var filter *pb.Filter
	if len(filters) > 0 {
		conditions := buildFilterConditions(filters)
		filter = &pb.Filter{Must: conditions}
	}
	resp, err := s.points.Search(ctx, &pb.SearchPoints{
		CollectionName: constants.CollectionNameImages,
		Vector:         embedding,
		VectorName:     pointer(string(enc)),
		Limit:          uint64(topK),
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
		Filter:         filter,
	})
	if err != nil {
		tracing.AddErrorToSpan(span, err)
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

func (s *QdrantStore) FindIDByMeta(ctx context.Context, key, value string) (string, bool, error) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return "", false, nil
	}
	filters := map[string]string{key: value}
	conditions := buildFilterConditions(filters)
	filter := &pb.Filter{Must: conditions}
	limit := uint32(1)
	resp, err := s.points.Scroll(ctx, &pb.ScrollPoints{
		CollectionName: constants.CollectionNameImages,
		Filter:         filter,
		Limit:          &limit,
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	})
	if err != nil {
		return "", false, fmt.Errorf("scroll: %w", err)
	}
	results := resp.GetResult()
	if len(results) == 0 {
		return "", false, nil
	}
	if id := results[0].GetId(); id != nil {
		if uuid := id.GetUuid(); uuid != "" {
			return uuid, true, nil
		}
	}
	if payload := results[0].GetPayload(); payload != nil {
		record := s.payloadToRecord(payload)
		if record.ID != "" {
			return record.ID, true, nil
		}
	}
	return "", false, nil
}

func (s *QdrantStore) Delete(ctx context.Context, id string) error {
	ctx, span := tracing.StartSpanWithAttributes(ctx, tracer, "Delete",
		[]attribute.KeyValue{
			attribute.String("id", id),
		},
	)
	defer span.End()

	_, err := s.points.Delete(ctx, &pb.DeletePoints{
		CollectionName: constants.CollectionNameImages,
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
	if err != nil {
		tracing.AddErrorToSpan(span, err)
		return err
	}
	return nil
}

func (s *QdrantStore) GetVector(ctx context.Context, id string, enc models.Encoder) (models.Embedding, error) {
	resp, err := s.points.Get(ctx, &pb.GetPoints{
		CollectionName: constants.CollectionNameImages,
		Ids: []*pb.PointId{
			{PointIdOptions: &pb.PointId_Uuid{Uuid: id}},
		},
		WithVectors: &pb.WithVectorsSelector{
			SelectorOptions: &pb.WithVectorsSelector_Include{
				Include: &pb.VectorsSelector{Names: []string{string(enc)}},
			},
		},
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
	if named := vecs.GetVectors(); named != nil {
		if vector, ok := named.GetVectors()[string(enc)]; ok && vector != nil {
			return vector.Data, nil
		}
	}
	return nil, fmt.Errorf("no vector data in response for encoder %s", enc)
}

func (s *QdrantStore) recordToPayload(record models.ImageRecord) map[string]*pb.Value {
	payload := map[string]*pb.Value{
		constants.PayloadFieldID:       {Kind: &pb.Value_StringValue{StringValue: record.ID}},
		constants.PayloadFieldURL:      {Kind: &pb.Value_StringValue{StringValue: record.URL}},
		constants.PayloadFieldFilename: {Kind: &pb.Value_StringValue{StringValue: record.Filename}},
	}
	if len(record.Tags) > 0 {
		tags := make([]*pb.Value, len(record.Tags))
		for i, t := range record.Tags {
			tags[i] = &pb.Value{Kind: &pb.Value_StringValue{StringValue: t}}
		}
		payload[constants.PayloadFieldTags] = &pb.Value{Kind: &pb.Value_ListValue{ListValue: &pb.ListValue{Values: tags}}}
	}
	for k, v := range record.Meta {
		payload[constants.PayloadFieldMetaPrefix+k] = &pb.Value{Kind: &pb.Value_StringValue{StringValue: v}}
	}
	return payload
}

func (s *QdrantStore) payloadToRecord(payload map[string]*pb.Value) models.ImageRecord {
	record := models.ImageRecord{
		Meta: make(map[string]string),
	}
	if v, ok := payload[constants.PayloadFieldID]; ok {
		if sv, ok := v.Kind.(*pb.Value_StringValue); ok {
			record.ID = sv.StringValue
		}
	}
	if v, ok := payload[constants.PayloadFieldURL]; ok {
		if sv, ok := v.Kind.(*pb.Value_StringValue); ok {
			record.URL = sv.StringValue
		}
	}
	if v, ok := payload[constants.PayloadFieldFilename]; ok {
		if sv, ok := v.Kind.(*pb.Value_StringValue); ok {
			record.Filename = sv.StringValue
		}
	}
	if v, ok := payload[constants.PayloadFieldTags]; ok {
		if lv, ok := v.Kind.(*pb.Value_ListValue); ok {
			for _, tag := range lv.ListValue.Values {
				if sv, ok := tag.Kind.(*pb.Value_StringValue); ok {
					record.Tags = append(record.Tags, sv.StringValue)
				}
			}
		}
	}
	for k, v := range payload {
		if len(k) > len(constants.PayloadFieldMetaPrefix) && k[:len(constants.PayloadFieldMetaPrefix)] == constants.PayloadFieldMetaPrefix {
			if sv, ok := v.Kind.(*pb.Value_StringValue); ok {
				record.Meta[k[len(constants.PayloadFieldMetaPrefix):]] = sv.StringValue
			}
		}
	}
	return record
}

func buildFilterConditions(filters map[string]string) []*pb.Condition {
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
	return conditions
}

func (s *QdrantStore) ListImages(ctx context.Context, filters map[string]string, limit, offset uint32) ([]models.ImageRecord, error) {
	if limit == 0 {
		limit = constants.DefaultLimit100
	}
	var filter *pb.Filter
	if len(filters) > 0 {
		conditions := buildFilterConditions(filters)
		filter = &pb.Filter{Must: conditions}
	}
	resp, err := s.points.Scroll(ctx, &pb.ScrollPoints{
		CollectionName: constants.CollectionNameImages,
		Filter:         filter,
		Limit:          &limit,
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, fmt.Errorf("scroll: %w", err)
	}
	records := make([]models.ImageRecord, 0, len(resp.GetResult()))
	for _, point := range resp.GetResult() {
		if payload := point.GetPayload(); payload != nil {
			record := s.payloadToRecord(payload)
			records = append(records, record)
		}
	}
	return records, nil
}

func (s *QdrantStore) Close() error {
	return s.conn.Close()
}

func (s *QdrantStore) vectorParamsMap() map[string]*pb.VectorParams {
	params := make(map[string]*pb.VectorParams, len(s.dims))
	for name, dim := range s.dims {
		params[string(name)] = &pb.VectorParams{
			Size:     dim,
			Distance: pb.Distance_Cosine,
		}
	}
	return params
}

func normalizeDims(dims map[models.Encoder]int) map[models.Encoder]uint64 {
	normalized := make(map[models.Encoder]uint64, len(dims))
	for name, dim := range dims {
		enc := models.NormalizeEncoder(name)
		if enc == "" || dim <= 0 {
			continue
		}
		normalized[enc] = uint64(dim)
	}
	if len(normalized) == 0 {
		normalized[models.EncoderCLIP] = 512
	}
	return normalized
}

func pointer[T any](value T) *T {
	return &value
}
