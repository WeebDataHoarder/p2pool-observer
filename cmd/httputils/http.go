package httputils

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"io"
	"net/http"
	"strings"
)

func EncodeJson(r *http.Request, writer io.Writer, d any) error {
	encoder := utils.NewJSONEncoder(writer)
	if strings.Index(strings.ToLower(r.Header.Get("user-agent")), "mozilla") != -1 {
		encoder.SetIndent("", "    ")
	}
	return encoder.EncodeWithOption(d, utils.JsonEncodeOptions...)
}

// StreamJsonSlice Streams a channel of values into a JSON list via a writer.
func StreamJsonSlice[T any](r *http.Request, writer io.Writer, stream <-chan T) error {
	encoder := utils.NewJSONEncoder(writer)
	if strings.Index(strings.ToLower(r.Header.Get("user-agent")), "mozilla") != -1 {
		encoder.SetIndent("", "    ")
	}
	// Write start of JSON list
	_, _ = writer.Write([]byte{'[', 0xa})
	var count uint64
	defer func() {
		// Write end of JSON list
		_, _ = writer.Write([]byte{0xa, ']'})

		// Empty channel
		for range stream {

		}
	}()

	for v := range stream {
		if count > 0 {
			// Write separator between list fields
			_, _ = writer.Write([]byte{',', 0xa})
		}
		if err := encoder.EncodeWithOption(v, utils.JsonEncodeOptions...); err != nil {
			return err
		}
		count++
	}
	return nil
}

// StreamJsonIterator Streams an iterator of values into a JSON list via a writer.
func StreamJsonIterator[T any](r *http.Request, writer io.Writer, next func() (int, *T)) error {
	encoder := utils.NewJSONEncoder(writer)
	if strings.Index(strings.ToLower(r.Header.Get("user-agent")), "mozilla") != -1 {
		encoder.SetIndent("", "    ")
	}
	// Write start of JSON list
	_, _ = writer.Write([]byte{'[', 0xa})
	var count uint64
	defer func() {
		// Write end of JSON list
		_, _ = writer.Write([]byte{0xa, ']'})
	}()

	for {
		_, v := next()
		if v == nil {
			break
		}
		if count > 0 {
			// Write separator between list fields
			_, _ = writer.Write([]byte{',', 0xa})
		}
		if err := encoder.EncodeWithOption(v, utils.JsonEncodeOptions...); err != nil {
			return err
		}
		count++
	}

	return nil
}
