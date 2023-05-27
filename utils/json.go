package utils

import (
	gojson "github.com/goccy/go-json"
	"io"
)

var JsonEncodeOptions = []gojson.EncodeOptionFunc{gojson.DisableHTMLEscape(), gojson.DisableNormalizeUTF8()}

func MarshalJSON(val any) ([]byte, error) {
	return gojson.MarshalWithOption(val, JsonEncodeOptions...)
}

func MarshalJSONIndent(val any, indent string) ([]byte, error) {
	return gojson.MarshalIndentWithOption(val, "", indent, JsonEncodeOptions...)
}

func UnmarshalJSON(data []byte, val any) error {
	return gojson.UnmarshalWithOption(data, val)
}

func NewJSONEncoder(writer io.Writer) *gojson.Encoder {
	return gojson.NewEncoder(writer)
}

func NewJSONDecoder(reader io.Reader) *gojson.Decoder {
	return gojson.NewDecoder(reader)
}
