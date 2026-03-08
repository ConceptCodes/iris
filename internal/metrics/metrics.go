package metrics

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	searchRequestsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iris_search_requests_total",
			Help: "Total number of search requests",
		},
		[]string{"type"},
	)

	searchErrorsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iris_search_errors_total",
			Help: "Total number of search errors",
		},
		[]string{"type"},
	)

	searchLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "iris_search_latency_seconds",
			Help:    "Search request latency in seconds",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"type"},
	)

	indexRequestsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iris_index_requests_total",
			Help: "Total number of index requests",
		},
		[]string{"type"},
	)

	indexErrorsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iris_index_errors_total",
			Help: "Total number of index errors",
		},
		[]string{"type"},
	)

	indexLatencyHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "iris_index_latency_seconds",
			Help:    "Index request latency in seconds",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"type"},
	)

	crawlRunsQueuedCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "iris_crawl_runs_queued_total",
			Help: "Total number of crawl runs queued",
		},
	)

	crawlJobsDiscoveredCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "iris_crawl_jobs_discovered_total",
			Help: "Total number of crawl jobs discovered",
		},
	)

	crawlJobsIndexedCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "iris_crawl_jobs_indexed_total",
			Help: "Total number of crawl jobs indexed successfully",
		},
	)

	crawlJobsDuplicateCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "iris_crawl_jobs_duplicate_total",
			Help: "Total number of crawl jobs skipped because the image already exists",
		},
	)

	crawlJobsFailedCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "iris_crawl_jobs_failed_total",
			Help: "Total number of crawl jobs that failed",
		},
	)

	workerJobsSucceededCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "iris_worker_jobs_succeeded_total",
			Help: "Total number of worker jobs succeeded",
		},
	)

	workerJobsFailedCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "iris_worker_jobs_failed_total",
			Help: "Total number of worker jobs failed",
		},
	)

	workerJobLatencyHistogram = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "iris_worker_job_latency_seconds",
			Help:    "Worker job processing latency in seconds",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
	)

	dedupeEventsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iris_dedupe_events_total",
			Help: "Total number of corpus-wide dedupe events",
		},
		[]string{"reason"},
	)

	crawlSkipsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iris_crawl_skips_total",
			Help: "Total number of crawl items skipped by policy",
		},
		[]string{"reason"},
	)

	crawlBudgetHitsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iris_crawl_budget_hits_total",
			Help: "Total number of crawl budget limits hit",
		},
		[]string{"budget"},
	)

	schedulerDecisionsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "iris_scheduler_decisions_total",
			Help: "Total number of adaptive scheduler decisions",
		},
		[]string{"decision"},
	)

	schedulerNextRunHistogram = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "iris_scheduler_next_run_seconds",
			Help:    "Next-run delays selected by the adaptive scheduler",
			Buckets: []float64{60, 300, 900, 1800, 3600, 7200, 14400, 28800},
		},
		[]string{"decision"},
	)
)

func init() {
	prometheus.MustRegister(
		searchRequestsCounter,
		searchErrorsCounter,
		searchLatencyHistogram,
		indexRequestsCounter,
		indexErrorsCounter,
		indexLatencyHistogram,
		crawlRunsQueuedCounter,
		crawlJobsDiscoveredCounter,
		crawlJobsIndexedCounter,
		crawlJobsDuplicateCounter,
		crawlJobsFailedCounter,
		workerJobsSucceededCounter,
		workerJobsFailedCounter,
		workerJobLatencyHistogram,
		dedupeEventsCounter,
		crawlSkipsCounter,
		crawlBudgetHitsCounter,
		schedulerDecisionsCounter,
		schedulerNextRunHistogram,
	)
}

type Snapshot struct {
	SearchRequests  int64 `json:"search_requests"`
	SearchErrors    int64 `json:"search_errors"`
	IndexRequests   int64 `json:"index_requests"`
	IndexErrors     int64 `json:"index_errors"`
	CrawlRunsQueued int64 `json:"crawl_runs_queued"`
	JobsSucceeded   int64 `json:"jobs_succeeded"`
	JobsFailed      int64 `json:"jobs_failed"`
}

type Counters struct {
	searchRequests  atomic.Int64
	searchErrors    atomic.Int64
	indexRequests   atomic.Int64
	indexErrors     atomic.Int64
	crawlRunsQueued atomic.Int64
	jobsSucceeded   atomic.Int64
	jobsFailed      atomic.Int64
}

func NewCounters() *Counters {
	return &Counters{}
}

// In-memory counter methods (kept for backwards compatibility)

func (c *Counters) IncSearchRequest() {
	c.searchRequests.Add(1)
}

func (c *Counters) IncSearchError() {
	c.searchErrors.Add(1)
}

func (c *Counters) IncIndexRequest() {
	c.indexRequests.Add(1)
}

func (c *Counters) IncIndexError() {
	c.indexErrors.Add(1)
}

func (c *Counters) IncCrawlRunsQueued() {
	c.crawlRunsQueued.Add(1)
}

func (c *Counters) IncJobSucceeded() {
	c.jobsSucceeded.Add(1)
}

func (c *Counters) IncJobFailed() {
	c.jobsFailed.Add(1)
}

func (c *Counters) Snapshot() Snapshot {
	return Snapshot{
		SearchRequests:  c.searchRequests.Load(),
		SearchErrors:    c.searchErrors.Load(),
		IndexRequests:   c.indexRequests.Load(),
		IndexErrors:     c.indexErrors.Load(),
		CrawlRunsQueued: c.crawlRunsQueued.Load(),
		JobsSucceeded:   c.jobsSucceeded.Load(),
		JobsFailed:      c.jobsFailed.Load(),
	}
}

// Prometheus metrics methods

// IncSearchRequestPrometheus increments the search request counter
func IncSearchRequestPrometheus(searchType string) {
	searchRequestsCounter.WithLabelValues(searchType).Inc()
}

// IncSearchErrorPrometheus increments the search error counter
func IncSearchErrorPrometheus(searchType string) {
	searchErrorsCounter.WithLabelValues(searchType).Inc()
}

// ObserveSearchLatency records the latency of a search request
func ObserveSearchLatency(searchType string, duration time.Duration) {
	searchLatencyHistogram.WithLabelValues(searchType).Observe(duration.Seconds())
}

// IncIndexRequestPrometheus increments the index request counter
func IncIndexRequestPrometheus(indexType string) {
	indexRequestsCounter.WithLabelValues(indexType).Inc()
}

// IncIndexErrorPrometheus increments the index error counter
func IncIndexErrorPrometheus(indexType string) {
	indexErrorsCounter.WithLabelValues(indexType).Inc()
}

// ObserveIndexLatency records the latency of an index request
func ObserveIndexLatency(indexType string, duration time.Duration) {
	indexLatencyHistogram.WithLabelValues(indexType).Observe(duration.Seconds())
}

// IncCrawlRunsQueuedPrometheus increments the crawl runs queued counter
func IncCrawlRunsQueuedPrometheus() {
	crawlRunsQueuedCounter.Inc()
}

// IncCrawlJobsDiscovered increments the crawl jobs discovered counter
func IncCrawlJobsDiscovered() {
	crawlJobsDiscoveredCounter.Inc()
}

// IncCrawlJobsIndexed increments the crawl jobs indexed counter
func IncCrawlJobsIndexed() {
	crawlJobsIndexedCounter.Inc()
}

// IncCrawlJobsDuplicate increments the crawl duplicate counter
func IncCrawlJobsDuplicate() {
	crawlJobsDuplicateCounter.Inc()
}

// IncCrawlJobsFailed increments the crawl jobs failed counter
func IncCrawlJobsFailed() {
	crawlJobsFailedCounter.Inc()
}

// IncWorkerJobSucceeded increments the worker jobs succeeded counter
func IncWorkerJobSucceeded() {
	workerJobsSucceededCounter.Inc()
}

// IncWorkerJobFailed increments the worker jobs failed counter
func IncWorkerJobFailed() {
	workerJobsFailedCounter.Inc()
}

// ObserveWorkerJobLatency records the latency of a worker job
func ObserveWorkerJobLatency(duration time.Duration) {
	workerJobLatencyHistogram.Observe(duration.Seconds())
}

// IncDedupeEvent increments dedupe decisions by reason.
func IncDedupeEvent(reason string) {
	dedupeEventsCounter.WithLabelValues(reason).Inc()
}

// IncCrawlSkip increments crawl policy skips by reason.
func IncCrawlSkip(reason string) {
	crawlSkipsCounter.WithLabelValues(reason).Inc()
}

// IncCrawlBudgetHit increments crawl budget hit counters.
func IncCrawlBudgetHit(budget string) {
	crawlBudgetHitsCounter.WithLabelValues(budget).Inc()
}

// ObserveSchedulerDecision records adaptive scheduler choices.
func ObserveSchedulerDecision(decision string, nextIn time.Duration) {
	schedulerDecisionsCounter.WithLabelValues(decision).Inc()
	schedulerNextRunHistogram.WithLabelValues(decision).Observe(nextIn.Seconds())
}

// Handler returns an HTTP handler for the /metrics endpoint
func Handler() http.Handler {
	return promhttp.Handler()
}
