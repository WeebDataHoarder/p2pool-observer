package utils

import (
	jsoniter "github.com/json-iterator/go"
	"io"
)

var jsonConfig = jsoniter.Config{
	EscapeHTML:              false,
	SortMapKeys:             true,
	ValidateJsonRawMessage:  true,
	MarshalFloatWith6Digits: true,
}.Froze()

func MarshalJSON(val any) ([]byte, error) {
	return jsonConfig.Marshal(val)
}

func MarshalJSONIndent(val any, indent string) ([]byte, error) {
	return jsonConfig.MarshalIndent(val, "", indent)
}

func UnmarshalJSON(data []byte, val any) error {
	return jsonConfig.Unmarshal(data, val)
}

func NewJSONEncoder(writer io.Writer) *jsoniter.Encoder {
	return jsonConfig.NewEncoder(writer)
}

func NewJSONDecoder(reader io.Reader) *jsoniter.Decoder {
	return jsonConfig.NewDecoder(reader)
}
