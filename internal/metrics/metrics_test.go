package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func counterValue(t *testing.T, metric *dto.Metric) float64 {
	t.Helper()
	if metric.GetCounter() == nil {
		t.Fatal("expected counter metric")
	}
	return metric.GetCounter().GetValue()
}

func histogramCount(t *testing.T, metric *dto.Metric) uint64 {
	t.Helper()
	if metric.GetHistogram() == nil {
		t.Fatal("expected histogram metric")
	}
	return metric.GetHistogram().GetSampleCount()
}

func TestCountersSnapshotReflectsInMemoryCounts(t *testing.T) {
	c := NewCounters()
	c.IncSearchRequest()
	c.IncSearchError()
	c.IncIndexRequest()
	c.IncIndexError()
	c.IncCrawlRunsQueued()
	c.IncJobSucceeded()
	c.IncJobFailed()

	snapshot := c.Snapshot()
	if snapshot.SearchRequests != 1 || snapshot.SearchErrors != 1 || snapshot.IndexRequests != 1 || snapshot.IndexErrors != 1 {
		t.Fatalf("unexpected snapshot counts: %+v", snapshot)
	}
	if snapshot.CrawlRunsQueued != 1 || snapshot.JobsSucceeded != 1 || snapshot.JobsFailed != 1 {
		t.Fatalf("unexpected snapshot counts: %+v", snapshot)
	}
}

func TestPrometheusMetricsFunctionsRecordData(t *testing.T) {
	beforeSearch := &dto.Metric{}
	if err := searchRequestsCounter.WithLabelValues("text").Write(beforeSearch); err != nil {
		t.Fatalf("write search counter: %v", err)
	}
	beforeSearchErr := &dto.Metric{}
	if err := searchErrorsCounter.WithLabelValues("text").Write(beforeSearchErr); err != nil {
		t.Fatalf("write search error counter: %v", err)
	}
	beforeIndex := &dto.Metric{}
	if err := indexRequestsCounter.WithLabelValues("upload").Write(beforeIndex); err != nil {
		t.Fatalf("write index counter: %v", err)
	}
	beforeLatency := &dto.Metric{}
	searchLatencyMetric := searchLatencyHistogram.WithLabelValues("text").(prometheus.Metric)
	if err := searchLatencyMetric.Write(beforeLatency); err != nil {
		t.Fatalf("write search histogram: %v", err)
	}

	IncSearchRequestPrometheus("text")
	IncSearchErrorPrometheus("text")
	IncIndexRequestPrometheus("upload")
	IncIndexErrorPrometheus("upload")
	ObserveSearchLatency("text", 50*time.Millisecond)
	ObserveIndexLatency("upload", 75*time.Millisecond)
	IncCrawlRunsQueuedPrometheus()
	IncCrawlJobsDiscovered()
	IncCrawlJobsIndexed()
	IncCrawlJobsDuplicate()
	IncCrawlJobsFailed()
	IncWorkerJobSucceeded()
	IncWorkerJobFailed()
	ObserveWorkerJobLatency(25 * time.Millisecond)
	IncDedupeEvent("content_sha256")
	IncCrawlSkip("robots")
	IncCrawlBudgetHit("images")
	ObserveSchedulerDecision("steady", 2*time.Minute)

	afterSearch := &dto.Metric{}
	if err := searchRequestsCounter.WithLabelValues("text").Write(afterSearch); err != nil {
		t.Fatalf("write search counter: %v", err)
	}
	if counterValue(t, afterSearch) != counterValue(t, beforeSearch)+1 {
		t.Fatalf("expected search request counter increment")
	}

	afterSearchErr := &dto.Metric{}
	if err := searchErrorsCounter.WithLabelValues("text").Write(afterSearchErr); err != nil {
		t.Fatalf("write search error counter: %v", err)
	}
	if counterValue(t, afterSearchErr) != counterValue(t, beforeSearchErr)+1 {
		t.Fatalf("expected search error counter increment")
	}

	afterIndex := &dto.Metric{}
	if err := indexRequestsCounter.WithLabelValues("upload").Write(afterIndex); err != nil {
		t.Fatalf("write index counter: %v", err)
	}
	if counterValue(t, afterIndex) != counterValue(t, beforeIndex)+1 {
		t.Fatalf("expected index request counter increment")
	}

	afterLatency := &dto.Metric{}
	if err := searchLatencyMetric.Write(afterLatency); err != nil {
		t.Fatalf("write search histogram: %v", err)
	}
	if histogramCount(t, afterLatency) != histogramCount(t, beforeLatency)+1 {
		t.Fatalf("expected search latency histogram sample count increment")
	}
}

func TestHandlerServesPrometheusMetrics(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	res := httptest.NewRecorder()

	Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if !strings.Contains(res.Body.String(), "iris_search_requests_total") {
		t.Fatal("expected Prometheus metrics body")
	}
}
