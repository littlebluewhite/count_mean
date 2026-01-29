package patterns

import (
	"fmt"
	"regexp"
	"sync"
)

// RegexCache provides thread-safe caching for compiled regular expressions.
type RegexCache struct {
	cache sync.Map
}

//nolint:gochecknoglobals // Singleton pattern for regex cache.
var (
	globalRegexCache *RegexCache
	regexOnce        sync.Once
)

// GlobalRegexCache returns the singleton regex cache.
func GlobalRegexCache() *RegexCache {
	regexOnce.Do(func() {
		globalRegexCache = &RegexCache{}
	})

	return globalRegexCache
}

// Get retrieves a compiled regex from cache or compiles and caches it.
func (c *RegexCache) Get(pattern string) (*regexp.Regexp, error) {
	if cached, ok := c.cache.Load(pattern); ok {
		if re, ok := cached.(*regexp.Regexp); ok {
			return re, nil
		}
	}

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to compile regex pattern %q: %w", pattern, err)
	}

	c.cache.Store(pattern, compiled)

	return compiled, nil
}

// MustGet retrieves a compiled regex, panics on compilation error.
func (c *RegexCache) MustGet(pattern string) *regexp.Regexp {
	re, err := c.Get(pattern)
	if err != nil {
		panic("failed to compile regex: " + pattern + ": " + err.Error())
	}

	return re
}

// Precompile compiles and caches multiple patterns.
func (c *RegexCache) Precompile(patterns []string) error {
	for _, pattern := range patterns {
		if _, err := c.Get(pattern); err != nil {
			return err
		}
	}

	return nil
}
