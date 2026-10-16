package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testBudgets() budgets {
	return budgets{
		GoVersion: "go1.27.2", GOOS: "linux", GOARCH: "amd64", CPU: "test CPU",
		Procs: 2, Samples: 5, BinaryBytes: 100,
		Benchmarks: map[string]map[string]float64{
			"BenchmarkExample/with-hyphen": {"ns/op": 100, "B/op": 50, "allocs/op": 5},
		},
	}
}

func benchmarkOutput() string {
	return "go version go1.27.2 linux/amd64\ngoos: linux\ngoarch: amd64\ncpu: test CPU\n" +
		strings.Repeat("BenchmarkExample/with-hyphen-2 1000 80 ns/op 40 B/op 4 allocs/op\n", 5) + "PASS\n"
}

func TestCheckBenchmarks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(string) string
		timing bool
		want   string
	}{
		{"valid", func(s string) string { return s }, true, ""},
		{"single slow outlier", func(s string) string { return strings.Replace(s, "80 ns/op", "9999 ns/op", 1) }, true, ""},
		{"timing regression", func(s string) string { return strings.ReplaceAll(s, "80 ns/op", "101 ns/op") }, true, "exceeds ns/op"},
		{"timing disabled", func(s string) string { return strings.ReplaceAll(s, "80 ns/op", "101 ns/op") }, false, ""},
		{"bytes regression", func(s string) string { return strings.ReplaceAll(s, "40 B/op", "51 B/op") }, false, "exceeds B/op"},
		{"allocation regression", func(s string) string { return strings.ReplaceAll(s, "4 allocs/op", "6 allocs/op") }, false, "exceeds allocs/op"},
		{"missing version", func(s string) string { return strings.TrimPrefix(s, "go version go1.27.2 linux/amd64\n") }, false, "environment"},
		{"wrong version", func(s string) string { return strings.ReplaceAll(s, "go1.27.2", "go1.26.0") }, false, "environment"},
		{"wrong OS", func(s string) string { return strings.ReplaceAll(s, "goos: linux", "goos: darwin") }, false, "environment"},
		{"wrong arch", func(s string) string { return strings.ReplaceAll(s, "goarch: amd64", "goarch: arm64") }, false, "environment"},
		{"wrong CPU", func(s string) string { return strings.ReplaceAll(s, "test CPU", "other CPU") }, true, "environment"},
		{"other CPU allocations", func(s string) string { return strings.ReplaceAll(s, "test CPU", "other CPU") }, false, ""},
		{"wrong parallelism", func(s string) string { return strings.ReplaceAll(s, "hyphen-2", "hyphen-4") }, false, "unexpected"},
		{"short run", func(s string) string {
			return strings.Replace(s, "BenchmarkExample/with-hyphen-2 1000 80 ns/op 40 B/op 4 allocs/op\n", "", 1)
		}, false, "samples"},
		{"empty input", func(string) string { return "" }, false, "samples"},
		{"unknown name", func(s string) string { return strings.ReplaceAll(s, "BenchmarkExample", "BenchmarkOther") }, false, "unexpected"},
		{"failed run", func(s string) string { return s + "FAIL\n" }, false, "run failed"},
		{"truncated run", func(s string) string { return strings.TrimSuffix(s, "PASS\n") }, false, "finish with PASS"},
		{"truncated package", func(s string) string { return strings.TrimSuffix(s, "PASS\n") + "goos: linux\n" }, false, "finish with PASS"},
		{"invalid metric", func(s string) string { return strings.ReplaceAll(s, "80 ns/op", "NaN ns/op") }, true, "invalid"},
		{"infinite metric", func(s string) string { return strings.ReplaceAll(s, "80 ns/op", "+Inf ns/op") }, true, "invalid"},
		{"negative metric", func(s string) string { return strings.ReplaceAll(s, "40 B/op", "-1 B/op") }, false, "invalid"},
		{"missing metric", func(s string) string { return strings.ReplaceAll(s, "40 B/op ", "") }, false, "incomplete"},
		{"duplicate metric", func(s string) string { return strings.ReplaceAll(s, "4 allocs/op", "4 B/op") }, false, "invalid"},
		{"zero iterations", func(s string) string { return strings.ReplaceAll(s, "1000", "0") }, false, "iteration"},
		{"even samples", func(s string) string {
			s = strings.Replace(s, "80 ns/op", "120 ns/op", 2)
			return strings.TrimSuffix(s, "PASS\n") + "BenchmarkExample/with-hyphen-2 1000 120 ns/op 40 B/op 4 allocs/op\nPASS\n"
		}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkBenchmarks(strings.NewReader(tc.change(benchmarkOutput())), testBudgets(), tc.timing, io.Discard)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBudgetValidation(t *testing.T) {
	for _, mutate := range []func(*budgets){
		func(b *budgets) { b.Samples = 4 },
		func(b *budgets) { b.BinaryBytes = 0 },
		func(b *budgets) { b.Procs = 0 },
		func(b *budgets) { b.Benchmarks = nil },
		func(b *budgets) { delete(b.Benchmarks["BenchmarkExample/with-hyphen"], "ns/op") },
		func(b *budgets) { b.Benchmarks["BenchmarkExample/with-hyphen"]["ns/op"] = -1 },
	} {
		limits := testBudgets()
		mutate(&limits)
		if err := limits.validate(); err == nil {
			t.Fatal("invalid budget accepted")
		}
	}
}

func TestSingleProcessorOutput(t *testing.T) {
	limits := testBudgets()
	limits.Procs = 1
	input := strings.ReplaceAll(benchmarkOutput(), "hyphen-2", "hyphen")
	if err := checkBenchmarks(strings.NewReader(input), limits, true, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	baseline, results, binary := filepath.Join(dir, "budgets.json"), filepath.Join(dir, "results.txt"), filepath.Join(dir, "web")
	data, err := json.Marshal(testBudgets())
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string][]byte{baseline: data, results: []byte(benchmarkOutput()), binary: []byte("binary")} {
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := run(baseline, results, binary, true, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 101} {
		if err := os.WriteFile(binary, make([]byte, size), 0600); err != nil {
			t.Fatal(err)
		}
		if err := run(baseline, results, binary, false, io.Discard); err == nil {
			t.Fatalf("accepted binary size %d", size)
		}
	}
	if err := run(baseline, results, dir, false, io.Discard); err == nil {
		t.Fatal("accepted directory as binary")
	}
	if err := run(baseline, results, filepath.Join(dir, "missing"), false, io.Discard); err == nil {
		t.Fatal("accepted missing binary")
	}
}
