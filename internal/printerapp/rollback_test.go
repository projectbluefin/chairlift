package printerapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFailedSetupPreservesUnitUntilServiceStops(t *testing.T) {
	for _, state := range []string{"active", "activating", ""} {
		t.Run(state, func(t *testing.T) {
			dir, _ := stubUnitDir(t)
			app := Select(Families()[0])
			startErr := errors.New("start did not finish")
			stopErr := errors.New("stop refused")
			runSystemctl = func(_ context.Context, args ...string) error {
				switch args[0] {
				case "start":
					return startErr
				case "stop":
					return stopErr
				}
				return nil
			}
			runSystemctlOutput = func(context.Context, ...string) (string, error) { return state, errors.New("state query failed") }
			err := Enable(context.Background(), app)
			if !errors.Is(err, startErr) || !errors.Is(err, stopErr) {
				t.Fatalf("Enable error = %v; want both setup and rollback errors", err)
			}
			if _, err := os.Stat(filepath.Join(dir, app.UnitName())); err != nil {
				t.Fatalf("lost management file without a confirmed stop: %v", err)
			}
		})
	}
}

func TestCancelledSetupRollsBackWithFreshBoundedContext(t *testing.T) {
	dir, _ := stubUnitDir(t)
	app := Select(Families()[0])
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopped := false
	runSystemctl = func(callCtx context.Context, args ...string) error {
		switch args[0] {
		case "start":
			cancel()
			return ctx.Err()
		case "stop":
			if err := callCtx.Err(); err != nil {
				t.Fatalf("rollback inherited cancellation: %v", err)
			}
			if _, ok := callCtx.Deadline(); !ok {
				t.Fatal("rollback is unbounded")
			}
			stopped = true
		}
		return nil
	}
	if err := Enable(ctx, app); !errors.Is(err, context.Canceled) {
		t.Fatalf("Enable error = %v", err)
	}
	if !stopped {
		t.Fatal("rollback did not stop service")
	}
	if _, err := os.Stat(filepath.Join(dir, app.UnitName())); !os.IsNotExist(err) {
		t.Fatalf("confirmed stop left unit: %v", err)
	}
}

func TestFailedSetupRemovesUnitWhenServiceAlreadyStopped(t *testing.T) {
	dir, _ := stubUnitDir(t)
	app := Select(Families()[0])
	startErr := errors.New("start failed")
	runSystemctl = func(_ context.Context, args ...string) error {
		if args[0] == "start" {
			return startErr
		}
		if args[0] == "stop" {
			return errors.New("unit not loaded")
		}
		return nil
	}
	runSystemctlOutput = func(context.Context, ...string) (string, error) { return "inactive", errors.New("exit status 3") }
	if err := Enable(context.Background(), app); !errors.Is(err, startErr) {
		t.Fatalf("Enable error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, app.UnitName())); !os.IsNotExist(err) {
		t.Fatalf("stopped service left unit: %v", err)
	}
}
