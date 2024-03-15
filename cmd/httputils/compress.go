package httputils

import (
	"slices"
	"strings"
)

type ContentEncoding string

/*
func (e ContentEncoding) NewPipe(r io.Reader) (reader io.Reader, err error) {
	switch e {
	case ContentEncodingNone:

	case ContentEncodingGzip:
	case ContentEncodingBrotli:
	case ContentEncodingZstd:
	default:
		panic("not supported")

	}
}

func (e ContentEncoding) Compress(in, buf []byte) (out []byte, err error) {
	switch e {
	case ContentEncodingNone:
		return append(buf, in...), nil
	case ContentEncodingGzip:
	case ContentEncodingBrotli:
	case ContentEncodingZstd:
	default:
		panic("not supported")

	}
}
*/

const (
	ContentEncodingNone   ContentEncoding = ""
	ContentEncodingGzip   ContentEncoding = "gzip"
	ContentEncodingBrotli ContentEncoding = "br"
	ContentEncodingZstd   ContentEncoding = "zstd"
)

func SelectEncodingServerPreference(acceptEncoding string) (contentEncoding ContentEncoding) {
	encodings := strings.Split(acceptEncoding, ",")
	for i := range encodings {
		//drop preference
		e := strings.Split(encodings[i], ";")
		encodings[i] = strings.TrimSpace(e[0])
	}

	if slices.Contains(encodings, string(ContentEncodingZstd)) {
		return ContentEncodingZstd
	} else if slices.Contains(encodings, string(ContentEncodingBrotli)) {
		return ContentEncodingBrotli
	} else if slices.Contains(encodings, string(ContentEncodingGzip)) {
		return ContentEncodingGzip
	} else {
		return ContentEncodingNone
	}
}
