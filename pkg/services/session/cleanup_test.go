package session

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type maintenanceFunc func(context.Context, int) (int64, error)

func (f maintenanceFunc) DeleteExpired(ctx context.Context, limit int) (int64, error) {
	return f(ctx, limit)
}

type maintenanceValue struct{}

func (maintenanceValue) DeleteExpired(context.Context, int) (int64, error) {
	return 1, nil
}

func TestCleanupRejectsTypedNilMaintenance(t *testing.T) {
	for _, tc := range []struct {
		name        string
		maintenance ExpiredSessionMaintenance
		invalid     bool
		count       int64
	}{
		{"nil means no cleanup", nil, false, 0},
		{"nil function", maintenanceFunc(nil), true, 0},
		{"nil pointer", (*maintenanceValue)(nil), true, 0},
		{"value", maintenanceValue{}, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, closeFails := range []bool{false, true} {
				closeErr := errors.New("close failed")
				closes, continued := 0, false
				factories := map[string]MaintenanceFactory{
					"a": func(context.Context) (ExpiredSessionMaintenance, func() error, error) {
						return tc.maintenance, func() error {
							closes++
							if closeFails {
								return closeErr
							}
							return nil
						}, nil
					},
					"b": func(context.Context) (ExpiredSessionMaintenance, func() error, error) {
						continued = true
						return nil, nil, nil
					},
				}

				count, err := CleanupExpired(t.Context(), map[string]string{"a": "a", "b": "b"}, factories, 1)
				if count != tc.count || closes != 1 || !continued || (err != nil) != (tc.invalid || closeFails) || errors.Is(err, closeErr) != closeFails {
					t.Fatalf("closeFails=%t: count=%d closes=%d continued=%t error=%v", closeFails, count, closes, continued, err)
				}
				if tc.invalid && !strings.Contains(err.Error(), `clean up session store "a": provider returned nil session maintenance`) {
					t.Fatalf("missing provider error: %v", err)
				}
			}
		})
	}
}

func TestCleanupSelectedProviders(t *testing.T) {
	var order []string
	factory := func(name string) MaintenanceFactory {
		return func(ctx context.Context) (ExpiredSessionMaintenance, func() error, error) {
			order = append(order, "open "+name)
			return maintenanceFunc(func(got context.Context, limit int) (int64, error) {
				if got != ctx || limit != 2 {
					t.Fatal("cleanup lost its context or batch limit")
				}
				order = append(order, "clean "+name)
				return 2, nil
			}), func() error { order = append(order, "close "+name); return nil }, nil
		}
	}
	factories := map[string]MaintenanceFactory{"files": factory("files"), "remote": factory("remote"), "unused": factory("unused")}
	types := map[string]string{"default": "REMOTE", "alias": "remote", "other": "files"}
	count, err := CleanupExpired(t.Context(), types, factories, 2)
	want := []string{"open files", "clean files", "close files", "open remote", "clean remote", "close remote"}
	if err != nil || count != 4 || !reflect.DeepEqual(order, want) {
		t.Fatalf("cleanup = %d, %v, %v", count, err, order)
	}
}

func TestCleanupFailuresCloseResourcesAndContinue(t *testing.T) {
	for _, failure := range []string{"open", "delete", "close"} {
		t.Run(failure, func(t *testing.T) {
			want := errors.New("provider failed")
			closeErr := errors.New("close failed")
			closed, deleted, continued := false, false, false
			factories := map[string]MaintenanceFactory{
				"a": func(context.Context) (ExpiredSessionMaintenance, func() error, error) {
					var openErr error
					if failure == "open" {
						openErr = want
					}
					return maintenanceFunc(func(context.Context, int) (int64, error) {
						deleted = true
						if failure == "delete" {
							return 0, want
						}
						return 1, nil
					}), func() error { closed = true; return closeErr }, openErr
				},
				"b": func(context.Context) (ExpiredSessionMaintenance, func() error, error) {
					continued = true
					return nil, nil, nil
				},
			}
			count, err := CleanupExpired(t.Context(), map[string]string{"a": "a", "b": "b"}, factories, 3)
			if !closed || !continued || deleted != (failure != "open") || !errors.Is(err, closeErr) {
				t.Fatalf("failure cleanup: closed=%t continued=%t deleted=%t error=%v", closed, continued, deleted, err)
			}
			if failure == "close" {
				if count != 1 {
					t.Fatal("close failure lost committed count:", count)
				}
			} else if count != 0 || !errors.Is(err, want) {
				t.Fatalf("provider error lost: %d, %v", count, err)
			}
		})
	}
}

func TestCleanupCancellation(t *testing.T) {
	for _, when := range []string{"before opening", "after opening", "during deletion"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			opened, closed, deleted := false, false, false
			if when == "before opening" {
				cancel()
			}
			factories := map[string]MaintenanceFactory{
				"a": func(context.Context) (ExpiredSessionMaintenance, func() error, error) {
					opened = true
					if when == "after opening" {
						cancel()
					}
					return maintenanceFunc(func(ctx context.Context, _ int) (int64, error) {
						deleted = true
						cancel()
						return 0, ctx.Err()
					}), func() error { closed = true; return nil }, nil
				},
				"b": func(context.Context) (ExpiredSessionMaintenance, func() error, error) {
					t.Fatal("opened another provider after cancellation")
					return nil, nil, nil
				},
			}
			count, err := CleanupExpired(ctx, map[string]string{"a": "a", "b": "b"}, factories, 1)
			if count != 0 || !errors.Is(err, context.Canceled) || opened != (when != "before opening") || closed != opened || deleted != (when == "during deletion") {
				t.Fatalf("canceled cleanup: count=%d error=%v opened=%t closed=%t deleted=%t", count, err, opened, closed, deleted)
			}
		})
	}
}

func TestCleanupInvalidConfiguration(t *testing.T) {
	unexpected := func(context.Context) (ExpiredSessionMaintenance, func() error, error) {
		t.Fatal("opened storage for invalid configuration")
		return nil, nil, nil
	}
	for _, tc := range []struct {
		name      string
		limit     int
		selected  string
		factories map[string]MaintenanceFactory
	}{
		{"zero limit", 0, "a", map[string]MaintenanceFactory{"a": unexpected}},
		{"negative limit", -1, "a", map[string]MaintenanceFactory{"a": unexpected}},
		{"duplicate provider", 1, "a", map[string]MaintenanceFactory{"A": unexpected, "a": unexpected}},
		{"empty provider", 1, "", map[string]MaintenanceFactory{"": unexpected}},
		{"whitespace provider", 1, " a", map[string]MaintenanceFactory{" a": unexpected}},
		{"unknown provider", 1, "missing", nil},
		{"unsupported maintenance", 1, "a", map[string]MaintenanceFactory{"a": nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count, err := CleanupExpired(t.Context(), map[string]string{"default": tc.selected}, tc.factories, tc.limit)
			if count != 0 || err == nil {
				t.Fatalf("invalid cleanup = %d, %v", count, err)
			}
		})
	}
	if count, err := CleanupExpired(t.Context(), nil, map[string]MaintenanceFactory{"unused": unexpected}, 1); count != 0 || err != nil {
		t.Fatalf("inactive cleanup = %d, %v", count, err)
	}
}
