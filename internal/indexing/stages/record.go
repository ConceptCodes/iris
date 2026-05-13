package stages

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"iris/internal/assets"
	"iris/internal/constants"
	"iris/internal/metadata"
	"iris/internal/quality"
	"iris/pkg/models"

	"github.com/disintegration/imaging"
)

type RecordConfig struct {
	ThumbnailWidth   int
	ThumbnailHeight  int
	ThumbnailQuality int
	AssetStore       assets.Store
	Enricher         metadata.Enricher
}

func DefaultRecordConfig() RecordConfig {
	return RecordConfig{
		ThumbnailWidth:   250,
		ThumbnailHeight:  0,
		ThumbnailQuality: 80,
	}
}

type PreparedRecord struct {
	Record models.ImageRecord
	Bytes  []byte
}

func PrepareRecord(ctx context.Context, imageBytes []byte, record models.ImageRecord, force bool, enricher metadata.Enricher) (models.ImageRecord, error) {
	if record.Meta == nil {
		record.Meta = map[string]string{}
	}
	if _, ok := record.Meta[constants.MetaKeyContentSHA256]; !ok {
		record.Meta[constants.MetaKeyContentSHA256] = HashBytes(imageBytes)
	}
	if record.Meta[constants.MetaKeySourceURL] == "" {
		if original, ok := record.Meta[constants.MetaKeyOriginURL]; ok && original != "" {
			record.Meta[constants.MetaKeySourceURL] = original
		} else if record.URL != "" {
			record.Meta[constants.MetaKeySourceURL] = record.URL
		}
	}
	if record.Meta[constants.MetaKeySourceDomain] == "" && record.Meta[constants.MetaKeySourceURL] != "" {
		if u, err := url.Parse(record.Meta[constants.MetaKeySourceURL]); err == nil {
			record.Meta[constants.MetaKeySourceDomain] = u.Hostname()
		}
	}
	return record, nil
}

func EnrichRecord(ctx context.Context, imageBytes []byte, record models.ImageRecord, enricher metadata.Enricher) (models.ImageRecord, error) {
	if enricher == nil {
		return record, nil
	}
	enrichment, err := enricher.Enrich(ctx, imageBytes, record)
	if err != nil {
		return record, fmt.Errorf("enrich metadata: %w", err)
	}
	record.Tags = metadata.MergeTags(record.Tags, enrichment.Tags)
	if record.Meta == nil {
		record.Meta = map[string]string{}
	}
	for key, value := range enrichment.Meta {
		if strings.TrimSpace(value) == "" {
			continue
		}
		record.Meta[key] = value
	}
	return record, nil
}

func AnalyzeQuality(ctx context.Context, imageBytes []byte, record models.ImageRecord) models.ImageRecord {
	analyzer := quality.NewDefaultAnalyzer()
	signals, analyzeErr := analyzer.Analyze(ctx, imageBytes)
	if analyzeErr == nil && signals.Width > 0 {
		record.ImageWidth = signals.Width
		record.ImageHeight = signals.Height
		record.ColorDepth = signals.ColorDepth
		record.QualityScore = signals.EntropyScore
	}
	record.FileSize = int64(len(imageBytes))
	record.IndexedAt = time.Now().UTC().Format(time.RFC3339)
	return record
}

func GenerateThumbnail(data []byte, width, height, quality int) ([]byte, error) {
	img, err := imaging.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	thumb := imaging.Resize(img, width, height, imaging.Lanczos)
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, thumb, imaging.JPEG, imaging.JPEGQuality(quality)); err != nil {
		return nil, fmt.Errorf("encode thumbnail: %w", err)
	}
	return buf.Bytes(), nil
}

func SaveThumbnail(ctx context.Context, assetStore assets.Store, recordID, filename string, imageBytes []byte, cfg RecordConfig) (string, error) {
	if assetStore == nil || cfg.ThumbnailWidth <= 0 {
		return "", nil
	}
	thumbBytes, err := GenerateThumbnail(imageBytes, cfg.ThumbnailWidth, cfg.ThumbnailHeight, cfg.ThumbnailQuality)
	if err != nil {
		return "", err
	}
	thumbURL, err := assetStore.Save(ctx, recordID+"_thumb", filename, thumbBytes)
	if err != nil {
		return "", err
	}
	return thumbURL, nil
}

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
