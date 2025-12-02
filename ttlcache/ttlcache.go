package ttlcache

import (
	"errors"
	"time"

	"github.com/labstack/echo/v4"
)

type TtlCache interface {
	Middleware() echo.MiddlewareFunc
}

type TtlCacheConfig struct {
	ProxyUrl string
	TtlSec   time.Duration
	TickSec  time.Duration
	Headers  map[string]string
	Logger   Logger
}

type ChacheData struct {
	Eol             int64
	ContentType     string
	ContentEncoding string
	Data            []byte
}

type EolData struct {
	Key string
	Eol int64
}

func SortEolData(a, b EolData) int {
	if a == b {
		return 0
	} else if a.Eol > b.Eol {
		return 1
	} else {
		return -1
	}
}

var ErrNoSuchKey error = errors.New("no such key")
var ErrExpired error = errors.New("ttl expired")
