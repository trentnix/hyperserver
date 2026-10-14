package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

// JSONSerializer writes [1, {session values}] and restores numbers as json.Number.
// It also reads unversioned JSON objects. Unknown versions and trailing data are
// rejected. Version metadata does not reserve keys in the session's data map.
type JSONSerializer struct{}

// Encode serializes session values using payload version 1.
func (JSONSerializer) Encode(data map[string]any) ([]byte, error) {
	if data == nil {
		data = map[string]any{}
	}
	return json.Marshal([2]any{1, data})
}

// Decode reads payload version 1 or an unversioned JSON object without losing
// integer precision. Reading does not rewrite the stored representation.
func (JSONSerializer) Decode(data []byte) (map[string]any, error) {
	data = bytes.Trim(data, " \t\r\n")
	if len(data) > 0 && data[0] == '[' {
		var envelope []json.RawMessage
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, err
		}
		if len(envelope) != 2 {
			return nil, NewErrSessionInvalid(errors.New("session payload must contain a version and an object"))
		}
		var version int
		if err := json.Unmarshal(envelope[0], &version); err != nil {
			return nil, NewErrSessionInvalid(fmt.Errorf("invalid session payload version: %w", err))
		}
		if version != 1 {
			return nil, NewErrSessionInvalid(fmt.Errorf("unsupported session payload version %d", version))
		}
		data = envelope[1]
	}

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
