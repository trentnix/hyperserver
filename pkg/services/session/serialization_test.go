package session

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestSessionJSONRoundTrip(t *testing.T) {
	type message struct {
		Text string `json:"text"`
	}
	original := &Session{Data: map[string]any{
		"text": "hello <世界>", "version": int64(math.MaxInt64), "counter": 3,
		"messages": []message{{Text: "saved"}}, "enabled": true,
	}}
	encoded, err := original.EncodedData(nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession("id", "name", time.Now().Add(time.Hour), nil, encoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Data["text"] != original.Data["text"] || loaded.Data["enabled"] != true {
		t.Fatalf("changed values: %v", loaded.Data)
	}
	if number, ok := loaded.Data["version"].(json.Number); !ok || number.String() != "9223372036854775807" {
		t.Fatalf("integer precision lost: %v", loaded.Data["version"])
	}
	for _, s := range []*Session{original, loaded} {
		var version int64
		var counter int
		var messages []message
		if err := s.DecodeValue("version", &version); err != nil || version != math.MaxInt64 {
			t.Fatalf("version = %d, %v", version, err)
		}
		if err := s.DecodeValue("counter", &counter); err != nil || counter != 3 {
			t.Fatalf("counter = %d, %v", counter, err)
		}
		if err := s.DecodeValue("messages", &messages); err != nil || len(messages) != 1 || messages[0].Text != "saved" {
			t.Fatalf("messages = %v, %v", messages, err)
		}
		if err := s.DecodeValue("absent", &counter); err != nil || counter != 3 {
			t.Fatalf("missing key changed target: %d, %v", counter, err)
		}
		if err := s.DecodeValue("text", &counter); err == nil {
			t.Fatal("wrong value type accepted")
		}
	}
}

func TestSessionJSONSizeLimits(t *testing.T) {
	for _, size := range []int{MaxSessionDataSize - 1, MaxSessionDataSize, MaxSessionDataSize + 1} {
		// The object syntax and version envelope contribute twelve bytes.
		value := strings.Repeat("a", size-12)
		s := &Session{Data: map[string]any{"x": value}}
		encoded, err := s.EncodedData(nil)
		if size > MaxSessionDataSize {
			if !errors.Is(err, ErrSessionTooLarge) || encoded != "" {
				t.Fatalf("oversized encoding = %q, %v", encoded, err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		raw := base64.URLEncoding.EncodeToString([]byte(`[1,{"x":"` + value + `"}]`))
		loaded, err := LoadSession("id", "name", time.Now(), nil, raw, nil)
		if size > MaxSessionDataSize {
			if !errors.Is(err, ErrSessionTooLarge) || loaded != nil {
				t.Fatalf("oversized decoding accepted: %v", err)
			}
		} else if err != nil || loaded.Data["x"] != value {
			t.Fatalf("bounded decoding failed: %v", err)
		}
	}
	if _, err := LoadSession("id", "name", time.Now(), nil, strings.Repeat("!", MaxEncodedSessionSize+1), nil); !errors.Is(err, ErrSessionTooLarge) {
		t.Fatalf("encoded size was not checked first: %v", err)
	}
}

func TestSessionJSONRejectsInvalidData(t *testing.T) {
	var legacy bytes.Buffer
	if err := gob.NewEncoder(&legacy).Encode(map[string]any{"x": "legacy"}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", "null", "[]", "1", `"text"`, `{"x":`, `{} {}`, `{} garbage`, legacy.String()} {
		encoded := base64.URLEncoding.EncodeToString([]byte(raw))
		if loaded, err := LoadSession("id", "name", time.Now(), nil, encoded, nil); err == nil || loaded != nil {
			t.Fatalf("invalid data accepted: %.40q", raw)
		}
	}
	if _, err := LoadSession("id", "name", time.Now(), nil, "!invalid-base64!", nil); err == nil {
		t.Fatal("invalid base64 accepted")
	}
	for _, value := range []any{make(chan int), math.Inf(1), math.NaN()} {
		if _, err := (&Session{Data: map[string]any{"x": value}}).EncodedData(nil); err == nil {
			t.Fatal("unsupported JSON value accepted")
		}
	}
	encoded, err := (&Session{}).EncodedData(nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession("id", "name", time.Now(), nil, encoded, nil)
	if err != nil || loaded.Data == nil || len(loaded.Data) != 0 {
		t.Fatalf("empty session = %+v, %v", loaded, err)
	}
}

func TestSessionJSONNesting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		depth int
	}{
		{"within limit", 100},
		{"excessive nesting", 10001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Balanced arrays inside an object isolate nesting from syntax and root-type errors.
			raw := `{"nested":` + strings.Repeat("[", tc.depth) + "0" + strings.Repeat("]", tc.depth) + "}"
			if len(raw) > MaxSessionDataSize {
				t.Fatal("fixture exceeds the byte limit instead of testing nesting")
			}
			encoded := base64.URLEncoding.EncodeToString([]byte(raw))
			loaded, err := LoadSession("id", "name", time.Now(), nil, encoded, nil)
			if tc.depth == 100 {
				if err != nil || loaded == nil {
					t.Fatalf("valid nested object rejected: %v", err)
				}
				return
			}
			var syntaxErr *json.SyntaxError
			if loaded != nil || !errors.As(err, &syntaxErr) || !strings.Contains(syntaxErr.Error(), "exceeded max depth") {
				t.Fatalf("expected nesting-limit error, got %v", err)
			}
		})
	}
}

func FuzzLoadSession(f *testing.F) {
	for _, raw := range []string{`{}`, `{"n":9223372036854775807}`, `[1,{}]`, `[2,{}]`, `[1,{"version":2,"data":{}}]`, `null`, `{} {}`, "invalid"} {
		f.Add(base64.URLEncoding.EncodeToString([]byte(raw)))
	}
	f.Fuzz(func(t *testing.T, encoded string) {
		s, err := LoadSession("id", "name", time.Time{}, nil, encoded, nil)
		if err == nil && (s == nil || s.Data == nil) {
			t.Fatal("successful decode returned no session data")
		}
	})
}

type testSerializer struct {
	encode func(map[string]any) ([]byte, error)
	decode func([]byte) (map[string]any, error)
}

func (s testSerializer) Encode(data map[string]any) ([]byte, error) { return s.encode(data) }
func (s testSerializer) Decode(data []byte) (map[string]any, error) { return s.decode(data) }

func TestDefaultSerializerStoredFormat(t *testing.T) {
	// A fixed fixture prevents the encoder and decoder from changing together
	// while silently making existing stored sessions unreadable.
	const stored = "eyJuIjo5MjIzMzcyMDM2ODU0Nzc1ODA3LCJ0ZXh0Ijoic2F2ZWQifQ=="
	for _, serializer := range []Serializer{nil, JSONSerializer{}} {
		loaded, err := LoadSession("id", "name", time.Time{}, nil, stored, serializer)
		if err != nil || loaded.Data["text"] != "saved" || loaded.Data["n"] != json.Number("9223372036854775807") {
			t.Fatalf("existing data = %+v, %v", loaded, err)
		}
		encoded, err := loaded.EncodedData(serializer)
		want := base64.URLEncoding.EncodeToString([]byte(`[1,{"n":9223372036854775807,"text":"saved"}]`))
		if err != nil || encoded != want {
			t.Fatalf("version 1 encoding = %q, %v", encoded, err)
		}
	}
}

func TestDefaultSerializerPayloadVersions(t *testing.T) {
	for _, raw := range []string{
		`{"version":99,"data":{"version":2},"n":9223372036854775807}`,
		`[1,{"version":99,"data":{"version":2},"n":9223372036854775807}]`,
		" \n[ 1, {\"version\":99,\"data\":{\"version\":2},\"n\":9223372036854775807} ]\t",
	} {
		t.Run(raw, func(t *testing.T) {
			serializer := JSONSerializer{}
			values, err := serializer.Decode([]byte(raw))
			if err != nil || len(values) != 3 || values["version"] != json.Number("99") || values["n"] != json.Number("9223372036854775807") {
				t.Fatalf("session values = %v, %v", values, err)
			}
			nested, ok := values["data"].(map[string]any)
			if !ok || nested["version"] != json.Number("2") {
				t.Fatalf("application data key changed: %v", values)
			}
			encoded, err := serializer.Encode(values)
			if err != nil || string(encoded) != `[1,{"data":{"version":2},"n":9223372036854775807,"version":99}]` {
				t.Fatalf("versioned encoding = %s, %v", encoded, err)
			}
			values["version"] = "changed"
			independent, err := serializer.Decode([]byte(raw))
			if err != nil || independent["version"] != json.Number("99") {
				t.Fatalf("decoder reused a map: %v, %v", independent, err)
			}
		})
	}
	encoded, err := (JSONSerializer{}).Encode(nil)
	if err != nil || string(encoded) != `[1,{}]` {
		t.Fatalf("empty encoding = %s, %v", encoded, err)
	}
}

func TestDefaultSerializerRejectsInvalidVersions(t *testing.T) {
	for _, raw := range []string{
		`[]`, `[1]`, `[1,{},{}]`, `[0,{}]`, `[-1,{}]`, `[2,{}]`,
		`[null,{}]`, `[true,{}]`, `["1",{}]`, `[1.0,{}]`, `[1e0,{}]`,
		`[999999999999999999999999,{}]`, `[{},{}]`, `[[1],{}]`,
		`[1,null]`, `[1,[]]`, `[1,1]`, `[1,"text"]`, `[1,[1,{}]]`,
		`[1,{}] {}`, `[1,{}] garbage`, `[1,{]`, `[1,{}],`,
		"\u00a0[1,{}]", "[1,{}]\u00a0", "\u00a0{}", "{}\u00a0",
	} {
		t.Run(raw, func(t *testing.T) {
			encoded := base64.URLEncoding.EncodeToString([]byte(raw))
			if loaded, err := LoadSession("id", "name", time.Time{}, nil, encoded, nil); loaded != nil || err == nil {
				t.Fatalf("invalid payload accepted: %+v, %v", loaded, err)
			}
		})
	}
}

func TestUnversionedPayloadSizeLimit(t *testing.T) {
	for _, size := range []int{MaxSessionDataSize - 4, MaxSessionDataSize, MaxSessionDataSize + 1} {
		legacy := `{"x":"` + strings.Repeat("a", size-8) + `"}`
		encoded := base64.URLEncoding.EncodeToString([]byte(legacy))
		loaded, err := LoadSession("id", "name", time.Time{}, nil, encoded, nil)
		if size > MaxSessionDataSize {
			if loaded != nil || !errors.Is(err, ErrSessionTooLarge) {
				t.Fatalf("oversized legacy payload: %+v, %v", loaded, err)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		// The version envelope counts toward the existing byte limit. A readable
		// legacy payload can be too large to save without reducing its data.
		rewritten, err := loaded.EncodedData(nil)
		if size+4 > MaxSessionDataSize {
			if rewritten != "" || !errors.Is(err, ErrSessionTooLarge) {
				t.Fatalf("oversized rewrite = %q, %v", rewritten, err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCustomSerializerBoundaries(t *testing.T) {
	for _, size := range []int{MaxSessionDataSize - 1, MaxSessionDataSize, MaxSessionDataSize + 1} {
		data := bytes.Repeat([]byte{0xff}, size)
		decoded := false
		serializer := testSerializer{
			encode: func(values map[string]any) ([]byte, error) {
				if values == nil {
					t.Fatal("nil session data was not normalized")
				}
				return data, nil
			},
			decode: func(got []byte) (map[string]any, error) {
				decoded = true
				if !bytes.Equal(got, data) {
					t.Fatal("binary serialized data changed")
				}
				return map[string]any{"size": size}, nil
			},
		}
		encoded, err := (&Session{}).EncodedData(serializer)
		if size > MaxSessionDataSize {
			if encoded != "" || !errors.Is(err, ErrSessionTooLarge) {
				t.Fatalf("oversized custom output = %q, %v", encoded, err)
			}
		} else if err != nil {
			t.Fatal(err)
		}

		loaded, err := LoadSession("id", "name", time.Time{}, nil, base64.URLEncoding.EncodeToString(data), serializer)
		if size > MaxSessionDataSize {
			if loaded != nil || !errors.Is(err, ErrSessionTooLarge) || decoded {
				t.Fatalf("oversized data reached custom decoder: decoded=%t, error=%v", decoded, err)
			}
		} else if err != nil || !decoded || loaded.Data["size"] != size {
			t.Fatalf("custom round trip = %+v, %v", loaded, err)
		}
	}
}

func TestCustomSerializerFailures(t *testing.T) {
	failure := errors.New("serializer failed")
	serializer := testSerializer{
		encode: func(map[string]any) ([]byte, error) { return []byte("partial"), failure },
		decode: func([]byte) (map[string]any, error) { return map[string]any{"partial": true}, failure },
	}
	if encoded, err := (&Session{}).EncodedData(serializer); encoded != "" || !errors.Is(err, failure) {
		t.Fatalf("encode failure = %q, %v", encoded, err)
	}
	if loaded, err := LoadSession("id", "name", time.Time{}, nil, "e30=", serializer); loaded != nil || !errors.Is(err, failure) {
		t.Fatalf("decode failure = %+v, %v", loaded, err)
	}
	serializer.decode = func([]byte) (map[string]any, error) { return nil, nil }
	if loaded, err := LoadSession("id", "name", time.Time{}, nil, "e30=", serializer); loaded != nil || err == nil {
		t.Fatalf("nil custom data accepted: %+v, %v", loaded, err)
	}
	serializer.decode = func([]byte) (map[string]any, error) {
		t.Fatal("invalid framing reached custom decoder")
		return nil, nil
	}
	for _, value := range []string{"!bad-base64!", strings.Repeat("a", MaxEncodedSessionSize+1)} {
		if loaded, err := LoadSession("id", "name", time.Time{}, nil, value, serializer); loaded != nil || err == nil {
			t.Fatalf("invalid framing accepted: %v", err)
		}
	}
}
