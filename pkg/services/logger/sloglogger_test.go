package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var _ Logger = (*SlogLogger)(nil)

func readLogRecords(t *testing.T, output *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	decoder := json.NewDecoder(output)
	for {
		var record map[string]any
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			return records
		}
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
}

func TestSlogLevelsAndFields(t *testing.T) {
	var output bytes.Buffer
	l := NewSlogLogger(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug}))
	for _, write := range []func(string, ...Field){l.Debug, l.Info, l.Warn, l.Error} {
		write("message\nwith newline", Field{"count", 3}, Field{"enabled", true}, Field{"error", errors.New("storage unavailable")}, Field{"details", map[string]string{"result": "failed"}})
	}
	records := readLogRecords(t, &output)
	if len(records) != 4 {
		t.Fatalf("got %d records, want 4", len(records))
	}
	for i, level := range []string{"DEBUG", "INFO", "WARN", "ERROR"} {
		record := records[i]
		if record["level"] != level || record["msg"] != "message\nwith newline" || record["count"] != float64(3) || record["enabled"] != true || record["error"] != "storage unavailable" {
			t.Fatalf("incorrect structured record: %v", record)
		}
		if details, ok := record["details"].(map[string]any); !ok || details["result"] != "failed" {
			t.Fatalf("lost nested fields: %v", record)
		}
		if timestamp, ok := record["time"].(string); !ok {
			t.Fatal("missing timestamp")
		} else if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSlogDefaultsAndCustomHandler(t *testing.T) {
	before := slog.Default()
	l := NewSlogLogger(nil)
	if slog.Default() != before || l.handler.Enabled(context.Background(), slog.LevelDebug) || !l.handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("default logger changed global state or selected an incorrect level")
	}
	if _, ok := l.handler.(*slog.JSONHandler); !ok {
		t.Fatal("default logger must use JSON")
	}

	var output bytes.Buffer
	level := new(slog.LevelVar)
	level.Set(slog.LevelWarn)
	l = NewSlogLogger(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: level}))
	l.Info("filtered")
	l.Warn("visible", Field{"component", "test"})
	level.Set(slog.LevelDebug)
	l.Debug("enabled")
	if got := output.String(); strings.Contains(got, "filtered") || !strings.Contains(got, "level=WARN msg=visible component=test") || !strings.Contains(got, "level=DEBUG msg=enabled") {
		t.Fatalf("handler configuration not respected: %s", got)
	}
}

func TestSlogWithIsolationAndConcurrency(t *testing.T) {
	var output bytes.Buffer
	l := NewSlogLogger(slog.NewJSONHandler(&output, nil))
	fields := []Field{{"component", "worker"}}
	base := l.With(fields...)
	fields[0] = Field{"component", "changed"}
	var workers sync.WaitGroup
	for i := range 32 {
		workers.Go(func() { base.With(Field{"worker", i}).Info("work", Field{"done", true}) })
	}
	workers.Wait()
	base.Info("base")
	l.Info("root")
	records := readLogRecords(t, &output)
	if len(records) != 34 {
		t.Fatalf("got %d records, want 34", len(records))
	}
	seen := make(map[float64]bool)
	for _, record := range records[:32] {
		id, ok := record["worker"].(float64)
		if !ok || seen[id] || record["component"] != "worker" || record["done"] != true {
			t.Fatalf("child fields leaked or concurrent record was corrupted: %v", record)
		}
		seen[id] = true
	}
	if records[32]["component"] != "worker" || records[32]["worker"] != nil || records[33]["component"] != nil || records[33]["worker"] != nil {
		t.Fatal("child fields modified a parent logger")
	}
}

type secretLogValue string

func (secretLogValue) LogValue() slog.Value { return slog.StringValue("[redacted]") }

func TestSlogRedaction(t *testing.T) {
	var output bytes.Buffer
	l := NewSlogLogger(slog.NewJSONHandler(&output, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == "password" {
				return slog.String(attr.Key, "[redacted]")
			}
			return attr
		},
	}))
	l.With(Field{"password", "bound-secret"}).Info("safe", Field{"token", secretLogValue("token-secret")})
	l.Info("safe", Field{"password", "entry-secret"})
	if strings.Contains(output.String(), "-secret") {
		t.Fatal("adapter bypassed handler or value redaction")
	}
	records := readLogRecords(t, &output)
	if len(records) != 2 || records[0]["password"] != "[redacted]" || records[0]["token"] != "[redacted]" || records[1]["password"] != "[redacted]" {
		t.Fatalf("incorrect redaction: %v", records)
	}
}

func TestSlogSourceIdentifiesCaller(t *testing.T) {
	var output bytes.Buffer
	l := NewSlogLogger(slog.NewJSONHandler(&output, &slog.HandlerOptions{AddSource: true}))
	_, file, line, _ := runtime.Caller(0)
	l.Info("source")
	records := readLogRecords(t, &output)
	source, ok := records[0]["source"].(map[string]any)
	if !ok || source["file"] != file || source["line"] != float64(line+1) {
		t.Fatalf("source points to the adapter instead of the caller: %v", records)
	}
}

// BenchmarkSlogLogger measures JSON encoding to io.Discard with three request
// fields and one bound field. INFO enables entries. ERROR filters them.
// It does not measure disk or transport latency.
func BenchmarkSlogLogger(b *testing.B) {
	for _, level := range []slog.Level{slog.LevelInfo, slog.LevelError} {
		b.Run(level.String(), func(b *testing.B) {
			l := NewSlogLogger(slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: level})).With(Field{"component", "benchmark"})
			b.ReportAllocs()
			for b.Loop() {
				l.Info("Incoming request", Field{"requestID", "request-123"}, Field{"method", "GET"}, Field{"path", "/account"})
			}
		})
	}
}
