package cache

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"strings"
)

func (d *ChacheData) CompressTextData(reqHeader, resHeader http.Header) ([]byte, error) {
	acceptEncoding := reqHeader.Get("Accept-Encoding")
	if !strings.Contains(acceptEncoding, "gzip") {
		return d.Data, nil
	}

	if !strings.Contains(d.ContentType, "text") &&
		!strings.Contains(d.ContentType, "application") {

		return d.Data, nil
	}

	buff := new(bytes.Buffer)
	g := gzip.NewWriter(buff)
	defer g.Close()

	// Write copies each bytes
	// so this is safe
	if _, err := g.Write(d.Data); err != nil {
		return nil, err
	}
	if err := g.Flush(); err != nil {
		return nil, err
	}

	resHeader.Set("Content-Encoding", "gzip")

	return buff.Bytes(), nil
}
