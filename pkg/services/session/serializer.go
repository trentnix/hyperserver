package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Serializer converts JSON-compatible session values to and from bytes.
// Implementations must support concurrent calls, preserve numeric precision,
// reject malformed input, and return independent, non-nil maps from Decode.
// They must bound decoding work and allocations and must not mutate Encode input.
// Size limits and base64 framing are enforced separately. Selecting a different
// format requires a plan for reading or invalidating existing sessions.
type Serializer interface {
	Encode(map[string]any) ([]byte, error)
	Decode([]byte) (map[string]any, error)
}

// JSONSerializer uses JSON objects and restores numbers as json.Number.
// It preserves the built-in session format and rejects trailing data.
type JSONSerializer struct{}

// Encode serializes session values as JSON.
func (JSONSerializer) Encode(data map[string]any) ([]byte, error) {
	return json.Marshal(data)
}

// Decode reads one JSON object without losing integer precision.
func (JSONSerializer) Decode(data []byte) (map[string]any, error) {
	var values map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&values); err != nil {
		return nil, err
	}
	if values == nil {
		return nil, NewErrSessionInvalid(errors.New("session data must be a JSON object"))
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, NewErrSessionInvalid(errors.New("unexpected data after session object"))
	}
	return values, nil
}
