package ttlcache

import (
	"errors"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type TtlCache interface {
	Middleware() echo.MiddlewareFunc
	BodyDump() middleware.BodyDumpHandler
}

type TtlCacheConfig struct {
	Ttl     time.Duration
	Tick    time.Duration
	Headers map[string]string
	Logger  Logger
}

type ChacheData struct {
	Eol         int64
	ContentType string
	Data        []byte
}

var ErrNoSuchKey error = errors.New("no such key")
var ErrExpired error = errors.New("ttl expired")
