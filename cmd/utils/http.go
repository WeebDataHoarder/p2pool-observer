package utils

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"net/http"
	"strings"
)

func EncodeJson(r *http.Request, writer http.ResponseWriter, d any) error {
	encoder := utils.NewJSONEncoder(writer)
	if strings.Index(strings.ToLower(r.Header.Get("user-agent")), "mozilla") != -1 {
		encoder.SetIndent("", "    ")
	}
	return encoder.EncodeWithOption(d, utils.JsonEncodeOptions...)
}

func StreamJsonSlice[T any](r *http.Request, writer http.ResponseWriter, stream chan T) error {
	encoder := utils.NewJSONEncoder(writer)
	if strings.Index(strings.ToLower(r.Header.Get("user-agent")), "mozilla") != -1 {
		encoder.SetIndent("", "    ")
	}
	_, _ = writer.Write([]byte{'[', 0xa})
	var count uint64
	defer func() {
		_, _ = writer.Write([]byte{0xa, ']'})
		for range stream {

		}
	}()

	for v := range stream {
		if count > 0 {
			_, _ = writer.Write([]byte{',', 0xa})
		}
		if err := encoder.EncodeWithOption(v, utils.JsonEncodeOptions...); err != nil {
			return err
		}
		count++
	}
	return nil
}
