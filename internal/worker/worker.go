package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	errpkg "iris/internal/error"
	"iris/internal/jobs"
)

// permanentErrCodes lists the errpkg codes that indicate a job should not be retried.
var permanentErrCodes = []errpkg.ErrorCode{
	errpkg.ErrUnsupportedContentType,
	errpkg.ErrImageExceedsLimit,
	errpkg.ErrNotFound,
	errpkg.ErrInvalidInput,
	errpkg.ErrRequiredField,
	errpkg.ErrFailedToReadFile,
	errpkg.ErrURLRequired,
	errpkg.ErrImageRequired,
	errpkg.ErrAdminAPIDisabled,
}

type ErrorType int

const (
	ErrorTypeTransient ErrorType = iota
	ErrorTypePermanent
)

func ClassifyError(err error) ErrorType {
	var httpErr interface{ StatusCode() int }
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode() {
		case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return ErrorTypePermanent
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return ErrorTypeTransient
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrorTypeTransient
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrorTypeTransient
	}
	var targetError errpkg.ErrorCode
	if errors.As(err, &targetError) {
		for _, code := range permanentErrCodes {
			if targetError == code {
				return ErrorTypePermanent
			}
		}
	}
	// Also classify by message substring for string-backed errors whose text matches a known
	// permanent errpkg code. This handles callers that construct errors.New() with the same
	// text as an errpkg.ErrorCode rather than returning the typed code directly.
	if err != nil {
		msg := strings.ToLower(err.Error())
		for _, code := range permanentErrCodes {
			if strings.Contains(msg, strings.ToLower(code.Error())) {
				return ErrorTypePermanent
			}
		}
	}
	return ErrorTypeTransient
}

func CalculateRetryBackoff(attempt int, baseDelay time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	backoff := baseDelay * time.Duration(1<<(attempt-1))
	jitterWindow := int64(baseDelay / 2)
	var jitter time.Duration
	if jitterWindow > 0 {
		jitter = time.Duration(rand.Int63n(jitterWindow))
	}
	backoff += jitter
	maxDelay := 5 * time.Minute
	if backoff > maxDelay {
		backoff = maxDelay
	}
	return backoff
}

func SleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		delay = time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func EnqueueLocalDirJobs(ctx context.Context, enqueueFn func(ctx context.Context, jobType, dedupKey string, payload json.RawMessage) error, dir, runID string, maxImages int, isImageExt func(string) bool, onBudgetHit func(string)) (int, error) {
	count := 0
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if maxImages > 0 && count >= maxImages {
			if onBudgetHit != nil {
				onBudgetHit("images")
			}
			return filepath.SkipAll
		}
		if !isImageExt(strings.ToLower(filepath.Ext(path))) {
			return nil
		}
		payload, err := json.Marshal(jobs.IndexLocalFilePayload{Path: path, RunID: runID})
		if err != nil {
			return err
		}
		if err := enqueueFn(ctx, string(jobs.TypeIndexLocalFile), DedupKey(string(jobs.TypeIndexLocalFile), runID, path), payload); err != nil {
			return err
		}
		count++
		return nil
	})
	return count, err
}

func SourceThrottle(rps int) func(context.Context) error {
	if rps <= 0 {
		return func(context.Context) error { return nil }
	}
	interval := time.Second / time.Duration(rps)
	var last time.Time
	return func(ctx context.Context) error {
		if last.IsZero() {
			last = time.Now()
			return nil
		}
		wait := time.Until(last.Add(interval))
		if wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
		last = time.Now()
		return nil
	}
}

func DedupKey(jobType, runID, target string) string {
	if target == "" {
		return ""
	}
	if runID == "" {
		return jobType + ":" + target
	}
	return jobType + ":" + runID + ":" + target
}

func SchedulerDecision(failureCount int, indexedCount, discoveredCount, duplicateCount int, hasLastRun bool) string {
	switch {
	case failureCount > 0:
		return "failure_backoff"
	case indexedCount > 0:
		return "maintain"
	case discoveredCount == 0 && hasLastRun:
		return "low_yield_backoff"
	case duplicateCount > 0 && indexedCount == 0:
		return "duplicate_backoff"
	default:
		return "initial"
	}
}

func ProcessSchedules(ctx context.Context, service ScheduleService, now time.Time) {
	sources, err := service.DueSources(ctx, now)
	if err != nil {
		slog.Warn("schedule check failed", "error", err)
		return
	}
	for _, src := range sources {
		next := now.Add(src.ScheduleEvery)
		if err := service.SetSourceNextRun(ctx, src.ID, next); err != nil {
			slog.Warn("failed to set next run", "source_id", src.ID, "error", err)
			continue
		}
		if _, err := service.TriggerRunForSource(ctx, src, "scheduled", now); err != nil {
			slog.Warn("failed to trigger scheduled run", "source_id", src.ID, "error", err)
		}
	}
}

type ScheduleSource struct {
	ID                  string
	ScheduleEvery       time.Duration
	ConsecutiveFailures int
	LastIndexedCount    int
	LastDiscoveredCount int
	LastDuplicateCount  int
	LastRunAt           time.Time
}

type ScheduleService interface {
	DueSources(ctx context.Context, now time.Time) ([]ScheduleSource, error)
	SetSourceNextRun(ctx context.Context, sourceID string, nextRun time.Time) error
	TriggerRunForSource(ctx context.Context, src ScheduleSource, trigger string, now time.Time) (string, error)
}

func CheckCrawlBudgets(maxPages, maxImages, processedPages, discoveredImages int) (pagesOK, imagesOK bool) {
	pagesOK = maxPages == 0 || processedPages < maxPages
	imagesOK = maxImages == 0 || discoveredImages < maxImages
	return
}

var ImageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
	".bmp":  true,
}

func IsImageExt(ext string) bool {
	return ImageExts[ext]
}
