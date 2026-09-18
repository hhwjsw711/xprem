package types

import "time"

const MaxBuildCacheObjectBytes int64 = 512 << 20
const MaxBuildCacheBytes int64 = 10 << 30

// BuildCacheNamespace identifies the cache protocol, independently of the build platform.
type BuildCacheNamespace string

const (
	BuildCacheGradle BuildCacheNamespace = "gradle"
	BuildCacheCcache BuildCacheNamespace = "ccache"
)

// A key is opaque to storage; each protocol validates its own keys.
type BuildCacheObject struct {
	ID              string              `json:"id"`
	AppID           string              `json:"-"`
	AppIdentifierID string              `json:"-"`
	Namespace       BuildCacheNamespace `json:"namespace"`
	CacheKey        string              `json:"key"`
	Size            int64               `json:"size"`
	SHA256          string              `json:"sha256"`
	CreatedAt       time.Time           `json:"-"`
	ExpiresAt       time.Time           `json:"-"`
	PublishedAt     *time.Time          `json:"-"`
}
