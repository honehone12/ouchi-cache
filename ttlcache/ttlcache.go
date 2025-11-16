package ttlcache

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"lukechampine.com/blake3"
)

type TtlCacheConfig struct {
	Ttl     time.Duration
	Tick    time.Duration
	Headers map[string]string
	Logger  Logger
}

type ChacheData struct {
	eol         int64
	contentType string
	data        []byte
}

type TtlCache struct {
	ttl      time.Duration
	tick     time.Duration
	headers  map[string]string
	cacheMap sync.Map
	eolMap   sync.Map
	logger   Logger
}

var ErrNoSuchKey error = errors.New("no such key")
var ErrExpired error = errors.New("ttl expired")

func NewTtlCache(config TtlCacheConfig) *TtlCache {
	c := &TtlCache{
		ttl:      config.Ttl,
		tick:     config.Tick,
		headers:  config.Headers,
		cacheMap: sync.Map{},
		eolMap:   sync.Map{},
		logger:   config.Logger,
	}
	c.startCleaning()
	return c
}

func (c *TtlCache) setHeaders(ctx echo.Context) {
	headers := ctx.Response().Header()
	for k, v := range c.headers {
		headers.Set(k, v)
	}
}

func (c *TtlCache) middlewareHandler(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		url := ctx.Request().URL.String()

		cache, err := c.get(url)
		if errors.Is(err, ErrNoSuchKey) || errors.Is(err, ErrExpired) {
			c.setHeaders(ctx)
			ctx.Response().Header().Set("XOuchCdn", "miss")
			return next(ctx)
		} else if err != nil {
			return err
		}

		c.setHeaders(ctx)
		ctx.Response().Header().Set("XOuchCdn", "cached")
		if err := ctx.Blob(
			http.StatusOK,
			cache.contentType,
			cache.data,
		); err != nil {
			return err
		}

		return nil
	}
}

func (c *TtlCache) Middleware() echo.MiddlewareFunc {
	return c.middlewareHandler
}

func (c *TtlCache) bodyDumpHandler(ctx echo.Context, req, res []byte) {
	url := ctx.Request().URL.String()
	contentType := ctx.Response().Header().Get("Content-Type")

	c.set(url, contentType, res)
}

func (c *TtlCache) BodyDump() middleware.BodyDumpHandler {
	return c.bodyDumpHandler
}

func (c *TtlCache) startCleaning() {
	go c.cleaning()
}

func (c *TtlCache) clean(key, value any, now int64) bool {
	eol, ok := key.(int64)
	if !ok || eol < now {
		c.logger.Debugf("deleting key: %d, value: %s", key, value)
		c.eolMap.Delete(key)
		c.cacheMap.Delete(value)
	}

	return true
}

func (c *TtlCache) cleaning() {
	ticker := time.Tick(c.tick)

	for now := range ticker {
		nowUnix := now.Unix()

		c.eolMap.Range(func(k, v any) bool {
			return c.clean(k, v, nowUnix)
		})
	}
}

func (c *TtlCache) get(url string) (*ChacheData, error) {
	k := blake3.Sum256([]byte(url))
	v, ok := c.cacheMap.Load(k)
	if !ok {
		return nil, ErrNoSuchKey
	}
	d, ok := v.(*ChacheData)
	if !ok {
		return nil, errors.New("failed to acquire value as expexted structure type")
	}

	now := time.Now().Unix()
	if d.eol < now {
		return nil, ErrExpired
	}

	return d, nil
}

func (c *TtlCache) set(url string, contentType string, content []byte) {
	k := blake3.Sum256([]byte(url))
	eol := time.Now().Add(c.ttl).Unix()
	d := &ChacheData{
		eol:         eol,
		contentType: contentType,
		data:        content,
	}

	c.cacheMap.Store(k, d)
	c.eolMap.Store(eol, k)
}
