package memory

import (
	"bytes"
	"errors"
	"hash"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"ouchi/ttlcache"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

type MemoryTtlCache struct {
	origin   string
	ttl      time.Duration
	tick     time.Duration
	headers  map[string]string
	cacheMap sync.Map
	eolMap   sync.Map

	hasher hash.Hash
	logger ttlcache.Logger
	proxy  *httputil.ReverseProxy
}

func NewMemoryTtlCache(config ttlcache.TtlCacheConfig) *MemoryTtlCache {
	c := &MemoryTtlCache{
		origin:   config.Origin,
		ttl:      config.Ttl,
		tick:     config.Tick,
		headers:  config.Headers,
		cacheMap: sync.Map{},
		eolMap:   sync.Map{},

		hasher: fnv.New128a(),
		logger: config.Logger,
	}

	// Create reverse proxy
	target := &url.URL{
		Scheme: "http",
		Host:   config.Origin,
	}
	c.proxy = httputil.NewSingleHostReverseProxy(target)

	// Customize the proxy to handle caching
	c.proxy.ModifyResponse = c.modifyResponse

	c.startCleaning()
	return c
}

func (c *MemoryTtlCache) setHeaders(ctx echo.Context) {
	headers := ctx.Response().Header()
	for k, v := range c.headers {
		headers.Set(k, v)
	}
}

// modifyResponse intercepts the response to cache it if appropriate
func (c *MemoryTtlCache) modifyResponse(resp *http.Response) error {
	// Only cache successful responses
	if resp.StatusCode == http.StatusOK {
		cacheControl := resp.Header.Get("Cache-Control")
		if cacheControl != "no-cache" && cacheControl != "no-store" {
			// Read the body
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			resp.Body.Close()

			// Cache the response
			c.set(resp.Request.URL.String(), resp.Header.Get("Content-Type"), body)

			// Replace the body so it can be read again
			resp.Body = io.NopCloser(bytes.NewReader(body))

			// Mark as cache miss
			resp.Header.Set("XOuchCdn", "miss")
		}
	}

	return nil
}

func (c *MemoryTtlCache) isWebSocketRequest(req *http.Request) bool {
	return strings.ToLower(req.Header.Get("Upgrade")) == "websocket"
}

func (c *MemoryTtlCache) middlewareHandler(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		req := ctx.Request()

		// WebSocket requests bypass cache and go directly to proxy
		if c.isWebSocketRequest(req) {
			c.logger.Debugf("WebSocket request: %s", req.URL.String())
			c.proxy.ServeHTTP(ctx.Response(), req)
			c.setHeaders(ctx)
			return nil
		}

		// Try to get from cache
		cache, err := c.get(req.URL.String())
		if errors.Is(err, ttlcache.ErrNoSuchKey) || errors.Is(err, ttlcache.ErrExpired) {
			// Cache miss - proxy the request
			c.proxy.ServeHTTP(ctx.Response(), req)
			c.setHeaders(ctx)
			return nil
		} else if err != nil {
			return err
		}

		// Cache hit - serve from cache
		if err := ctx.Blob(
			http.StatusOK,
			cache.ContentType,
			cache.Data,
		); err != nil {
			return err
		}

		ctx.Response().Header().Set("XOuchCdn", "cached")
		c.setHeaders(ctx)

		return nil
	}
}

func (c *MemoryTtlCache) Middleware() echo.MiddlewareFunc {
	return c.middlewareHandler
}

func (c *MemoryTtlCache) startCleaning() {
	go c.cleaning()
}

func (c *MemoryTtlCache) clean(key, value any, now int64) bool {
	eol, ok := key.(int64)
	if !ok || eol < now {
		c.eolMap.Delete(key)
		c.cacheMap.Delete(value)
		c.logger.Debugf("deleted: %s", value)
	}

	return true
}

func (c *MemoryTtlCache) cleaning() {
	ticker := time.Tick(c.tick)

	for now := range ticker {
		c.logger.Debug("cleaning")
		nowUnix := now.Unix()

		c.eolMap.Range(func(k, v any) bool {
			return c.clean(k, v, nowUnix)
		})
	}
}

func (c *MemoryTtlCache) get(url string) (*ttlcache.ChacheData, error) {
	k := c.hasher.Sum([]byte(url))
	v, ok := c.cacheMap.Load(k)
	if !ok {
		return nil, ttlcache.ErrNoSuchKey
	}
	d, ok := v.(*ttlcache.ChacheData)
	if !ok {
		return nil, errors.New("failed to acquire value as expexted structure type")
	}

	now := time.Now().Unix()
	if d.Eol < now {
		return nil, ttlcache.ErrExpired
	}

	c.logger.Debugf("found cache: %s : %x", url, k)
	return d, nil
}

func (c *MemoryTtlCache) set(url string, contentType string, content []byte) {
	k := c.hasher.Sum([]byte(url))
	eol := time.Now().Add(c.ttl).Unix()
	d := &ttlcache.ChacheData{
		Eol:         eol,
		ContentType: contentType,
		Data:        content,
	}

	c.cacheMap.Store(k, d)
	c.eolMap.Store(eol, k)
	c.logger.Debugf("cached: %s : %x", url, k)
}
