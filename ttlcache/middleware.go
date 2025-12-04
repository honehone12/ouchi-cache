package ttlcache

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"ouchi/cache"
	"ouchi/log"
	"strings"

	"github.com/labstack/echo/v4"
)

func isWebSocketRequest(req *http.Request) bool {
	return strings.ToLower(req.Header.Get("Upgrade")) == "websocket"
}

type TtlCache struct {
	store    cache.ChacheStore
	logger   log.Logger
	proxyUrl *url.URL
	proxy    *httputil.ReverseProxy

	headers map[string]string
}

func NewTtlCache(
	logger log.Logger,
	store cache.ChacheStore,
	proxyUrl string,
	headers map[string]string,
) (*TtlCache, error) {
	p, err := url.Parse(proxyUrl)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(p)

	c := &TtlCache{
		logger:   logger,
		store:    store,
		proxyUrl: p,
		proxy:    proxy,

		headers: headers,
	}

	// use modifier for reading and caching response
	c.proxy.ModifyResponse = c.onProxyResponse

	return c, nil
}

func (c *TtlCache) setHeaders(
	ctx echo.Context,
	/* or receive map[string]string */
	contentEncoding string,
	cached bool,
) {
	headers := ctx.Response().Header()

	for k, v := range c.headers {
		headers.Set(k, v)
	}

	if len(contentEncoding) != 0 {
		headers.Set("Content-Encoding", contentEncoding)
	}

	if cached {
		headers.Set("XOuchiCache", "cachd")
	} else {
		headers.Set("XOuchiCache", "miss")
	}
}

func (c *TtlCache) onProxyResponse(res *http.Response) error {
	if res.StatusCode == http.StatusOK {
		h := res.Header
		cacheControl := h.Get("Cache-Control")
		if cacheControl != "no-cache" && cacheControl != "no-store" {
			b, err := io.ReadAll(res.Body)
			if err != nil {
				return err
			}
			// close now to set new body
			res.Body.Close()

			if err := c.store.Set(
				res.Request.URL.RequestURI(),
				h.Get("Content-Type"),
				h.Get("Content-Encoding"),
				b,
			); err != nil {
				return err
			}

			res.Body = io.NopCloser(bytes.NewReader(b))
		}
	}

	return nil
}

func (c *TtlCache) middlewareHandler(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		req := ctx.Request()

		// webSocket requests bypass cache and go directly to proxy
		if isWebSocketRequest(req) {
			c.logger.Infof("proxy websocket: %s", req.URL.String())
			req.Host = c.proxyUrl.Hostname()
			c.proxy.ServeHTTP(ctx.Response(), req)
			c.setHeaders(ctx, "", false)
			return nil
		}

		d, err := c.store.Get(req.URL.RequestURI())
		// cache miss - proxy the request
		if errors.Is(err, cache.ErrNoSuchKey) || errors.Is(err, cache.ErrExpired) {
			c.logger.Debug(err)
			req.Host = c.proxyUrl.Hostname()
			c.proxy.ServeHTTP(ctx.Response(), req)
			c.setHeaders(ctx, "", false)
			return nil
		} else if err != nil {
			return err
		}

		c.setHeaders(ctx, d.ContentEncoding, true)

		return ctx.Blob(
			http.StatusOK,
			d.ContentType,
			d.Data,
		)
	}
}

func (c *TtlCache) Middleware() echo.MiddlewareFunc {
	return c.middlewareHandler
}
