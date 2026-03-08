package clip

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"iris/pkg/models"
)

const maxImageSize = 20 << 20

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type embedTextRequest struct {
	Text string `json:"text"`
}

type embedImageRequest struct {
	ImageB64 string `json:"image_b64"`
}

type embedResponse struct {
	Embedding []float32 `json:"embedding"`
	Dim       int       `json:"dim"`
}

func (c *Client) EmbedText(ctx context.Context, text string) (models.Embedding, error) {
	reqBody := embedTextRequest{Text: text}
	var resp embedResponse
	if err := c.doPost(ctx, "/embed/text", reqBody, &resp); err != nil {
		return nil, fmt.Errorf("embed text: %w", err)
	}
	return resp.Embedding, nil
}

func (c *Client) EmbedImageBytes(ctx context.Context, imageBytes []byte) (models.Embedding, error) {
	if len(imageBytes) > maxImageSize {
		return nil, fmt.Errorf("image exceeds %d bytes limit", maxImageSize)
	}
	reqBody := embedImageRequest{ImageB64: base64.StdEncoding.EncodeToString(imageBytes)}
	var resp embedResponse
	if err := c.doPost(ctx, "/embed/image", reqBody, &resp); err != nil {
		return nil, fmt.Errorf("embed image: %w", err)
	}
	return resp.Embedding, nil
}

func (c *Client) EmbedImageURL(ctx context.Context, imageURL string) (models.Embedding, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch image url: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch image url: status %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, maxImageSize+1)
	var buf bytes.Buffer
	n, err := buf.ReadFrom(limited)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	if n > maxImageSize {
		return nil, fmt.Errorf("image exceeds %d bytes limit", maxImageSize)
	}
	return c.EmbedImageBytes(ctx, buf.Bytes())
}

func (c *Client) doPost(ctx context.Context, path string, reqBody, respBody any) error {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Detail string `json:"detail"`
		}
		if json.NewDecoder(resp.Body).Decode(&errResp) == nil && errResp.Detail != "" {
			return fmt.Errorf("sidecar error (status %d): %s", resp.StatusCode, errResp.Detail)
		}
		return fmt.Errorf("sidecar error: status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}
	return nil
}
