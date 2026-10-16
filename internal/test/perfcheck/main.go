// Command perfcheck checks repeated Go benchmark results against explicit budgets.
// Timing checks are opt-in because latency comparisons require matching hardware.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
)

type budgets struct {
	GoVersion   string                        `json:"go_version"`
	GOOS        string                        `json:"goos"`
	GOARCH      string                        `json:"goarch"`
	CPU         string                        `json:"cpu"`
	Procs       int                           `json:"gomaxprocs"`
	Samples     int                           `json:"minimum_samples"`
	BinaryBytes int64                         `json:"max_binary_bytes"`
	Benchmarks  map[string]map[string]float64 `json:"benchmarks"`
}

func main() {
	baseline := flag.String("budgets", "docs/performance-budgets.json", "budget file")
	results := flag.String("results", "", "output of go version followed by repeated go test -bench runs")
	binary := flag.String("binary", "", "reference binary built with -trimpath -ldflags='-s -w'")
	timing := flag.Bool("timing", false, "also enforce ns/op budgets on the recorded CPU")
	flag.Parse()
	if err := run(*baseline, *results, *binary, *timing, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(baseline, results, binary string, timing bool, output io.Writer) error {
	data, err := os.ReadFile(baseline)
	if err != nil {
		return err
	}
	var limits budgets
	if err := json.Unmarshal(data, &limits); err != nil {
		return err
	}
	if err := limits.validate(); err != nil {
		return err
	}
	file, err := os.Open(results)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := checkBenchmarks(file, limits, timing, output); err != nil {
		return err
	}
	info, err := os.Stat(binary)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > limits.BinaryBytes {
		return fmt.Errorf("binary size %d exceeds budget %d or is not a nonempty regular file", info.Size(), limits.BinaryBytes)
	}
	fmt.Fprintf(output, "binary: %d bytes (budget %d)\n", info.Size(), limits.BinaryBytes)
	if !timing {
		fmt.Fprintln(output, "Timing budgets not checked. Use -timing only on matching, otherwise idle hardware.")
	}
	return nil
}

func (limits budgets) validate() error {
	if limits.GoVersion == "" || limits.GOOS == "" || limits.GOARCH == "" || limits.CPU == "" || limits.Procs < 1 || limits.Samples < 5 || limits.BinaryBytes <= 0 || len(limits.Benchmarks) == 0 {
		return fmt.Errorf("budgets require environment, at least five samples, binary size, and benchmarks")
	}
	for name, metrics := range limits.Benchmarks {
		if !strings.HasPrefix(name, "Benchmark") || len(metrics) != 3 {
			return fmt.Errorf("invalid benchmark budget %q", name)
		}
		for _, unit := range []string{"ns/op", "B/op", "allocs/op"} {
			value, ok := metrics[unit]
			if !ok || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("invalid %s budget for %s", unit, name)
			}
		}
	}
	return nil
}

func checkBenchmarks(input io.Reader, limits budgets, timing bool, output io.Writer) error {
	samples := make(map[string]map[string][]float64)
	var goVersion, goos, goarch, cpu string
	pending := false
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "go version "):
			fields := strings.Fields(line)
			if len(fields) != 4 {
				return fmt.Errorf("invalid Go version line")
			}
			goVersion = fields[2]
		case strings.HasPrefix(line, "goos: "):
			if pending {
				return fmt.Errorf("benchmark package did not finish with PASS")
			}
			goos = strings.TrimSpace(strings.TrimPrefix(line, "goos: "))
			goarch, cpu = "", ""
		case strings.HasPrefix(line, "goarch: "):
			goarch = strings.TrimSpace(strings.TrimPrefix(line, "goarch: "))
		case strings.HasPrefix(line, "cpu: "):
			cpu = strings.TrimSpace(strings.TrimPrefix(line, "cpu: "))
		case strings.HasPrefix(line, "FAIL") || strings.HasPrefix(line, "--- FAIL"):
			return fmt.Errorf("benchmark run failed: %s", line)
		case line == "PASS":
			pending = false
		case strings.HasPrefix(line, "Benchmark"):
			pending = true
			if goVersion != limits.GoVersion || goos != limits.GOOS || goarch != limits.GOARCH {
				return fmt.Errorf("benchmark environment %s %s/%s does not match %s %s/%s", goVersion, goos, goarch, limits.GoVersion, limits.GOOS, limits.GOARCH)
			}
			if cpu == "" || (timing && cpu != limits.CPU) {
				return fmt.Errorf("benchmark environment CPU %q does not match %q", cpu, limits.CPU)
			}
			fields := strings.Fields(line)
			if len(fields) < 8 || len(fields)%2 != 0 {
				return fmt.Errorf("incomplete benchmark: %s", line)
			}
			// Go omits the suffix at GOMAXPROCS=1. Names can also contain hyphens.
			name, procs := fields[0], "1"
			if limits.Benchmarks[name] == nil {
				if index := strings.LastIndexByte(name, '-'); index >= 0 {
					name, procs = name[:index], name[index+1:]
				}
			}
			iterations, err := strconv.ParseInt(fields[1], 10, 64)
			if procs != strconv.Itoa(limits.Procs) || err != nil || iterations < 1 || limits.Benchmarks[name] == nil {
				return fmt.Errorf("unexpected benchmark or iteration count: %s", line)
			}
			values := make(map[string]float64)
			for i := 2; i < len(fields); i += 2 {
				value, err := strconv.ParseFloat(fields[i], 64)
				_, duplicate := values[fields[i+1]]
				if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) || duplicate {
					return fmt.Errorf("invalid benchmark metric: %s", line)
				}
				values[fields[i+1]] = value
			}
			if samples[name] == nil {
				samples[name] = make(map[string][]float64)
			}
			for unit := range limits.Benchmarks[name] {
				value, exists := values[unit]
				if !exists {
					return fmt.Errorf("missing %s in %s", unit, name)
				}
				samples[name][unit] = append(samples[name][unit], value)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if pending {
		return fmt.Errorf("benchmark package did not finish with PASS")
	}
	var failures []string
	for _, name := range slices.Sorted(maps.Keys(limits.Benchmarks)) {
		for _, unit := range []string{"ns/op", "B/op", "allocs/op"} {
			values := samples[name][unit]
			if len(values) < limits.Samples {
				return fmt.Errorf("%s: need at least %d samples, got %d", name, limits.Samples, len(values))
			}
			slices.Sort(values)
			median := values[len(values)/2]
			if len(values)%2 == 0 {
				median = (values[len(values)/2-1] + median) / 2
			}
			limit := limits.Benchmarks[name][unit]
			fmt.Fprintf(output, "%s: median %.2f %s (budget %.2f, range %.2f–%.2f)\n", name, median, unit, limit, values[0], values[len(values)-1])
			if median > limit && (unit != "ns/op" || timing) {
				failures = append(failures, fmt.Sprintf("%s exceeds %s budget", name, unit))
			}
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("%s", strings.Join(failures, "\n"))
	}
	return nil
}
