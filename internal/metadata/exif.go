package metadata

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"iris/pkg/models"

	"github.com/rwcarlsen/goexif/exif"
)

const (
	metaExifMake             = "exif_make"
	metaExifModel            = "exif_model"
	metaExifLensModel        = "exif_lens_model"
	metaExifDateTime         = "exif_datetime"
	metaExifDateTimeOriginal = "exif_datetime_original"
	metaExifLat              = "exif_gps_latitude"
	metaExifLon              = "exif_gps_longitude"
)

type EXIFEnricher struct{}

func (e EXIFEnricher) Enrich(ctx context.Context, imageBytes []byte, record models.ImageRecord) (Result, error) {
	_ = ctx
	_ = record

	x, err := exif.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return Result{}, nil
	}

	meta := make(map[string]string)
	copyTagString(meta, x, exif.Make, metaExifMake)
	copyTagString(meta, x, exif.Model, metaExifModel)
	copyTagString(meta, x, exif.LensModel, metaExifLensModel)
	copyDateTime(meta, x, exif.DateTime, metaExifDateTime)
	copyDateTime(meta, x, exif.DateTimeOriginal, metaExifDateTimeOriginal)

	if lat, lon, err := x.LatLong(); err == nil {
		meta[metaExifLat] = strconv.FormatFloat(lat, 'f', 6, 64)
		meta[metaExifLon] = strconv.FormatFloat(lon, 'f', 6, 64)
	}

	return Result{Meta: meta}, nil
}

func copyTagString(meta map[string]string, x *exif.Exif, field exif.FieldName, target string) {
	tag, err := x.Get(field)
	if err != nil || tag == nil {
		return
	}
	value, err := tag.StringVal()
	if err != nil {
		value = strings.Trim(tag.String(), "\"")
	}
	value = strings.TrimSpace(value)
	if value != "" {
		meta[target] = value
	}
}

func copyDateTime(meta map[string]string, x *exif.Exif, field exif.FieldName, target string) {
	tag, err := x.Get(field)
	if err != nil || tag == nil {
		return
	}
	raw, err := tag.StringVal()
	if err != nil {
		raw = strings.Trim(tag.String(), "\"")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	if parsed, err := time.Parse("2006:01:02 15:04:05", raw); err == nil {
		meta[target] = parsed.Format(time.RFC3339)
		return
	}
	meta[target] = raw
}

var _ Enricher = EXIFEnricher{}

func (r Result) String() string {
	return fmt.Sprintf("tags=%d meta=%d", len(r.Tags), len(r.Meta))
}
