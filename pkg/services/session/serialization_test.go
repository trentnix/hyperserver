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
	encoded, err := original.EncodedData()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession("id", "name", time.Now().Add(time.Hour), nil, encoded)
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
		// The object syntax contributes eight bytes.
		value := strings.Repeat("a", size-8)
		s := &Session{Data: map[string]any{"x": value}}
		encoded, err := s.EncodedData()
		if size > MaxSessionDataSize {
			if !errors.Is(err, ErrSessionTooLarge) || encoded != "" {
				t.Fatalf("oversized encoding = %q, %v", encoded, err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		raw := base64.URLEncoding.EncodeToString([]byte(`{"x":"` + value + `"}`))
		loaded, err := LoadSession("id", "name", time.Now(), nil, raw)
		if size > MaxSessionDataSize {
			if !errors.Is(err, ErrSessionTooLarge) || loaded != nil {
				t.Fatalf("oversized decoding accepted: %v", err)
			}
		} else if err != nil || loaded.Data["x"] != value {
			t.Fatalf("bounded decoding failed: %v", err)
		}
	}
	if _, err := LoadSession("id", "name", time.Now(), nil, strings.Repeat("!", MaxEncodedSessionSize+1)); !errors.Is(err, ErrSessionTooLarge) {
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
		if loaded, err := LoadSession("id", "name", time.Now(), nil, encoded); err == nil || loaded != nil {
			t.Fatalf("invalid data accepted: %.40q", raw)
		}
	}
	if _, err := LoadSession("id", "name", time.Now(), nil, "!invalid-base64!"); err == nil {
		t.Fatal("invalid base64 accepted")
	}
	for _, value := range []any{make(chan int), math.Inf(1), math.NaN()} {
		if _, err := (&Session{Data: map[string]any{"x": value}}).EncodedData(); err == nil {
			t.Fatal("unsupported JSON value accepted")
		}
	}
	encoded, err := (&Session{}).EncodedData()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession("id", "name", time.Now(), nil, encoded)
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
			loaded, err := LoadSession("id", "name", time.Now(), nil, encoded)
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
	for _, raw := range []string{`{}`, `{"n":9223372036854775807}`, `null`, `{} {}`, "invalid"} {
		f.Add(base64.URLEncoding.EncodeToString([]byte(raw)))
	}
	f.Fuzz(func(t *testing.T, encoded string) {
		s, err := LoadSession("id", "name", time.Time{}, nil, encoded)
		if err == nil && (s == nil || s.Data == nil) {
			t.Fatal("successful decode returned no session data")
		}
	})
}
