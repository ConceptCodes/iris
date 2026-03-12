package constants

import "time"

// HTTP Headers
const (
	HeaderUserAgent       = "User-Agent"
	HeaderIfNoneMatch     = "If-None-Match"
	HeaderIfModifiedSince = "If-Modified-Since"
	HeaderETag            = "ETag"
	HeaderLastModified    = "Last-Modified"
	HeaderCacheControl    = "Cache-Control"
	HeaderExpires         = "Expires"
	HeaderRetryAfter      = "Retry-After"
	HeaderContentType     = "Content-Type"
	HeaderXAdminKey       = "X-Admin-Key"
	HeaderAuthorization   = "Authorization"
	HeaderHXRequest       = "HX-Request"
	HeaderXCSRFToken      = "X-CSRF-Token"
)

// HTTP Methods
const (
	MethodGET     = "GET"
	MethodPOST    = "POST"
	MethodPUT     = "PUT"
	MethodDELETE  = "DELETE"
	MethodPATCH   = "PATCH"
	MethodOPTIONS = "OPTIONS"
)

// HTTP Paths / Endpoints
const (
	PathHealth           = "/health"
	PathEmbedText        = "/embed/text"
	PathEmbedImage       = "/embed/image"
	PathRobotsTxt        = "/robots.txt"
	PathAdminSources     = "/admin/sources"
	PathAdminSourceRun   = "/admin/sources/{id}/run"
	PathAdminIndexLocal  = "/admin/index/local"
	PathAdminReindex     = "/admin/reindex"
	PathAdminRuns        = "/admin/runs"
	PathAdminRunDetail   = "/admin/runs/{id}"
	PathAdminMetrics     = "/admin/metrics"
	PathSearchText       = "/search/text"
	PathSearchImage      = "/search/image"
	PathSearchImageURL   = "/search/image/url"
	PathIndexURL         = "/index/url"
	PathIndexUpload      = "/index/upload"
	PathLanding          = "/"
	PathSearch           = "/search"
	PathImage            = "/image"
	PathImageRelated     = "/image/{id}/related"
	PathSearchReverse    = "/search/reverse"
	PathSearchReverseURL = "/search/reverse/url"
	PathAssets           = "/assets"
)

// HTTP Status Messages
const (
	StatusMsgFileTooLarge          = "file too large"
	StatusMsgURLRequired           = "url required"
	StatusMsgUnauthorized          = "unauthorized"
	StatusMsgNotFound              = "not found"
	StatusMsgAdminAPIDisabled      = "admin api disabled"
	StatusMsgCrawlUnavailable      = "crawl service unavailable"
	StatusMsgJobStoreUnavailable   = "job store unavailable"
	MessageFileTooLarge            = "file too large"
	MessageURLRequired             = "url is required"
	MessageUnauthorized            = "unauthorized"
	MessageNotFound                = "not found"
	MessageInvalidJSON             = "invalid json"
	MessageImageRequired           = "image file is required"
	MessagePathRequired            = "path is required"
	MessageCrawlServiceUnavailable = "crawl service unavailable"
	MessageJobStoreUnavailable     = "job store unavailable"
	MessageAdminAPIDisabled        = "admin api disabled"
	MsgFailedToReadFile            = "failed to read file"
	ErrorMsgUnsupportedContent     = "unsupported content type"
	ErrorMsgImageExceeds           = "image exceeds"
	ErrorMsgNotFound               = "not found"
	ErrorMsgInvalid                = "invalid"
	ErrorMsgIsRequired             = "is required"
	ErrorMsgFailedToReadFile       = "failed to read file"
	ErrorMsgURLRequired            = "url required"
	ErrorMsgImageRequired          = "image required"
)

// MIME Types
const (
	MIMETypeJSON        = "application/json"
	MIMETypeImagePrefix = "image/"
	MIMETypeJPEG        = "image/jpeg"
	MIMETypePNG         = "image/png"
	MIMETypeWEBP        = "image/webp"
	MIMETypeGIF         = "image/gif"
	MIMETypeBMP         = "image/bmp"
	MIMETypeTIFF        = "image/tiff"
)

// Timeouts and Durations
const (
	HTTPTimeout30s     = 30 * time.Second
	HTTPTimeout60s     = 60 * time.Second
	HTTPTimeout120s    = 120 * time.Second
	MiddlewareTimeout  = 60 * time.Second
	DefaultTTL5m       = 5 * time.Minute
	DefaultTTL10m      = 10 * time.Minute
	DefaultTTL24h      = 24 * time.Hour
	DefaultTTL15m      = 15 * time.Minute
	DefaultRobotsTTL   = 24 * time.Hour
	BackoffDelay500ms  = 500 * time.Millisecond
	MaxRetryDelay      = 5 * time.Minute
	CORSMaxAge         = 300
	ShutdownTimeout10s = 10 * time.Second
)

// Sizes and Limits
const (
	MaxImageSize           = 20 << 20 // 20MB
	DefaultHostConcurrency = 2
	DefaultConcurrency     = 4
	DefaultListLimit       = 100
	DefaultLimit40         = 40
	DefaultLimit100        = 100
	DefaultLimit300        = 300
	CachePruneBatch        = 500
)

// Ports
const (
	HTTPPort  = "80"
	HTTPSPort = "443"
)

// URL Schemes
const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"
)

// Crawler / Robots
const (
	DefaultCrawlerUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	RobotsDirectiveAllow     = "allow"
	RobotsDirectiveDisallow  = "disallow"
	RobotsDirectiveUserAgent = "user-agent"
)

// Database / Storage
const (
	CollectionNameImages   = "images"
	PayloadFieldMetaPrefix = "meta_"
)

// Metadata Keys
const (
	MetaKeyOriginURL     = "origin_url"
	MetaKeySourceURL     = "source_url"
	MetaKeySourceDomain  = "source_domain"
	MetaKeyMIMEType      = "mime_type"
	MetaKeyContentSHA256 = "content_sha256"
	MetaKeySource        = "source"
	MetaKeySourcePath    = "source_path"
	MetaKeySourceID      = "source_id"
	MetaKeyRunID         = "run_id"
	MetaKeyType          = "type"
	MetaKeyContentType   = "type"
	MetaKeyPageURL       = "page_url"
	MetaKeyTitle         = "title"
	MetaKeyCrawlSourceID = "crawl_source_id"
)

// Qdrant Payload Fields
const (
	PayloadFieldID       = "id"
	PayloadFieldURL      = "url"
	PayloadFieldFilename = "filename"
	PayloadFieldTags     = "tags"
)

// Special Strings
const (
	BearerPrefix     = "Bearer "
	StringTrue       = "true"
	StringFalse      = "false"
	TriggerManual    = "manual"
	TriggerScheduled = "scheduled"
	StringImage      = "image"
	StringHash       = "#"
	StringDollar     = "$"
	StringAsterisk   = "*"
	EncodingUTF8     = "utf-8"
	PatternWildcard  = "*"
	PatternAnchor    = "$"
	PatternComment   = "#"
)

// Keywords / Values
const (
	ValueIndexer    = "indexer"
	ValueCrawler    = "crawler"
	ValueMemory     = "memory"
	ValuePostgres   = "postgres"
	ValueLocal      = "local"
	ValueNone       = "none"
	ValueLocalDir   = "local_dir"
	ValueURLList    = "url_list"
	ValueDomain     = "domain"
	ValueSitemap    = "sitemap"
	KeywordIndexer  = "indexer"
	KeywordCrawler  = "crawler"
	KeywordMemory   = "memory"
	KeywordPostgres = "postgres"
	KeywordLocal    = "local"
	KeywordNone     = "none"
	KeywordLocalDir = "local_dir"
	KeywordURLList  = "url_list"
	KeywordDomain   = "domain"
	KeywordSitemap  = "sitemap"
)

// Dropped Query Parameters for URL Normalization
var DroppedQueryParams = map[string]struct{}{
	"fbclid":       {},
	"gclid":        {},
	"mc_cid":       {},
	"mc_eid":       {},
	"msclkid":      {},
	"ref":          {},
	"ref_src":      {},
	"source":       {},
	"utm_campaign": {},
	"utm_content":  {},
	"utm_id":       {},
	"utm_medium":   {},
	"utm_name":     {},
	"utm_source":   {},
	"utm_term":     {},
}

// Job Types
const (
	JobTypeDiscoverSource = "discover_source"
	JobTypeFetchImage     = "fetch_image"
	JobTypeIndexLocalFile = "index_local_file"
	JobTypeReindexImage   = "reindex_image"
)

// Poll Intervals
const (
	PollInterval1s  = time.Second
	PollInterval30s = 30 * time.Second
	PollInterval15m = 15 * time.Minute
)

// Lease Duration
const (
	LeaseDuration30s = 30 * time.Second
)

// Schedule Poll Interval
const (
	SchedulePollInterval = 30 * time.Second
)
