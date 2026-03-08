package metrics

import "sync/atomic"

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
