package httputil

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"iris/internal/constants"
	"iris/pkg/models"
)

var SupportedUploadMIMETypes = map[string]struct{}{
	constants.MIMETypeJPEG: {},
	constants.MIMETypePNG:  {},
	constants.MIMETypeWEBP: {},
	constants.MIMETypeGIF:  {},
	constants.MIMETypeBMP:  {},
	constants.MIMETypeTIFF: {},
}

const maxJSONBodyBytes = 1 << 20

type ImageUpload struct {
	Bytes     []byte
	Filename  string
	MIMEType  string
}

type ImageSearchInput struct {
	ImageBytes []byte
	TopK       int
	Filters    map[string]string
	Encoder    models.Encoder
}

type ImageURLSearchInput struct {
	URL     string
	TopK    int
	Filters map[string]string
	Encoder models.Encoder
}

func ParseMultipartImage(r *http.Request, maxBytes int64) (*ImageUpload, error) {
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		return nil, &HTTPError{Status: http.StatusBadRequest, Message: constants.StatusMsgFileTooLarge}
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		return nil, &HTTPError{Status: http.StatusBadRequest, Message: constants.MessageImageRequired}
	}
	defer file.Close()

	contentType := header.Header.Get(constants.HeaderContentType)
	if contentType == "" {
		sniff := make([]byte, 512)
		n, _ := io.ReadFull(file, sniff)
		contentType = http.DetectContentType(sniff[:n])
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, &HTTPError{Status: http.StatusInternalServerError, Message: constants.MsgFailedToReadFile}
		}
	}
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))

	buf, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, &HTTPError{Status: http.StatusInternalServerError, Message: constants.MsgFailedToReadFile}
	}
	if len(buf) > int(maxBytes) {
		return nil, &HTTPError{Status: http.StatusBadRequest, Message: constants.MessageFileTooLarge}
	}
	if len(buf) == 0 {
		return nil, &HTTPError{Status: http.StatusBadRequest, Message: constants.MsgFailedToReadFile}
	}

	filename := r.FormValue("filename")
	if filename == "" {
		filename = header.Filename
	}

	return &ImageUpload{
		Bytes:    buf,
		Filename: filename,
		MIMEType: contentType,
	}, nil
}

func ValidateImageMIME(contentType string) bool {
	_, ok := SupportedUploadMIMETypes[contentType]
	return ok
}

func ParseUploadTags(r *http.Request) []string {
	if t := r.FormValue("tags"); t != "" {
		return strings.Split(t, ",")
	}
	return nil
}

func ParseUploadMeta(r *http.Request) map[string]string {
	meta := make(map[string]string)
	for key, values := range r.MultipartForm.Value {
		if strings.HasPrefix(key, constants.PayloadFieldMetaPrefix) {
			meta[strings.TrimPrefix(key, constants.PayloadFieldMetaPrefix)] = values[0]
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func ParseSearchImageInput(r *http.Request) (*ImageSearchInput, error) {
	upload, err := ParseMultipartImage(r, constants.MaxImageSize)
	if err != nil {
		return nil, err
	}
	return &ImageSearchInput{
		ImageBytes: upload.Bytes,
		TopK:       ParseIntFormValue(r.FormValue("top_k"), 0),
		Filters:    ParseFilters(r.FormValue("filters")),
		Encoder:    models.NormalizeEncoder(models.Encoder(r.FormValue("encoder"))),
	}, nil
}

func ParseSearchImageURLInput(r *http.Request) (*ImageURLSearchInput, error) {
	var req struct {
		URL     string            `json:"url"`
		TopK    int               `json:"top_k"`
		Filters map[string]string `json:"filters"`
		Encoder models.Encoder    `json:"encoder,omitempty"`
	}
	if err := DecodeJSONBody(r, &req); err != nil {
		return nil, &HTTPError{Status: http.StatusBadRequest, Message: "invalid json"}
	}
	if req.URL == "" {
		return nil, &HTTPError{Status: http.StatusBadRequest, Message: constants.MessageURLRequired}
	}
	return &ImageURLSearchInput{
		URL:     req.URL,
		TopK:    req.TopK,
		Filters: req.Filters,
		Encoder: models.NormalizeEncoder(req.Encoder),
	}, nil
}

type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	return e.Message
}

func DecodeJSONBody(r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxJSONBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func ParseIntFormValue(s string, def int) int {
	if s == "" {
		return def
	}
	var v int
	if err := json.NewDecoder(strings.NewReader(s)).Decode(&v); err == nil {
		return v
	}
	return def
}

func ParseFilters(s string) map[string]string {
	if s == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}
