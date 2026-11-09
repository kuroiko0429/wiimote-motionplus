//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

type sessionDeps struct {
	telemetry func(context.Context, deviceSet, options) func()
	open      func(string) (*os.File, error)
	create    func() (*os.File, error)
	destroy   func(*os.File)
	readAbs   func(*os.File, uint16) (int32, error)
}

func runSession(ctx context.Context, d deviceSet, o options, bindings map[uint16]string) error {
	return runSessionWith(ctx, d, o, bindings, sessionDeps{
		telemetry: func(ctx context.Context, d deviceSet, o options) func() {
			return startTelemetry(ctx, d, telemetryOptions{WarningPercent: o.batteryWarning, Interval: o.batteryInterval}, func(f string, a ...any) { fmt.Printf(f+"\n", a...) })
		},
		// Nonblocking descriptors let os.File's poller interrupt Read on Close.
		open:    func(path string) (*os.File, error) { return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0) },
		create:  createUinput,
		destroy: func(f *os.File) { syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uiDevDestroy, 0); f.Close() },
		readAbs: readAbsValue,
	})
}

func runSessionWith(parent context.Context, d deviceSet, o options, bindings map[uint16]string, deps sessionDeps) error {
	ctx, cancel := context.WithCancel(parent)
	var motion, button, ui *os.File
	var readers sync.WaitGroup
	defer func() {
		cancel()
		if motion != nil {
			motion.Close()
		}
		if button != nil {
			button.Close()
		}
		readers.Wait()
		if ui != nil {
			deps.destroy(ui)
		}
	}()
	if ctx.Err() != nil {
		return nil
	}
	var err error
	motion, err = deps.open(d.motion)
	if err != nil {
		return fmt.Errorf("open motion %s: %w", d.motion, err)
	}
	if ctx.Err() != nil {
		return nil
	}
	button, err = deps.open(d.button)
	if err != nil {
		return fmt.Errorf("open buttons %s: %w", d.button, err)
	}
	if ctx.Err() != nil {
		return nil
	}
	ui, err = deps.create()
	if err != nil {
		return fmt.Errorf("create uinput: %w", err)
	}
	var outputErr error
	runtime := newRuntime(d.mode, o.gyroSensitivity, o.gyroDeadzone, time.Now(), func(e rawEvent) {
		if outputErr == nil {
			outputErr = writeEvent(ui, e.typ, e.code, e.value)
		}
	})
	runtime.bindings = bindings
	if err := runtime.initializeAxes(func(code uint16) (int32, error) { return deps.readAbs(motion, code) }); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	fmt.Printf("Mode: %s / Motion: %s / Buttons: %s\n", d.mode, d.motion, d.button)
	if d.mode == "gyro" {
		fmt.Println("Keep remote stationary for one second for gyro bias calibration. 1 recalibrates.")
	} else {
		fmt.Println("Press 1 to set current tilt as center.")
	}
	if deps.telemetry != nil {
		stop := deps.telemetry(ctx, d, o)
		defer stop()
	}
	events := make(chan taggedEvent, 64)
	for _, input := range []struct {
		f   *os.File
		src source
	}{{motion, srcAccel}, {button, srcButton}} {
		readers.Add(1)
		go func(f *os.File, src source) { defer readers.Done(); readEvents(ctx, f, src, events) }(input.f, input.src)
	}
	move := time.NewTicker(moveInterval)
	defer move.Stop()
	scroll := time.NewTicker(scrollInterval)
	defer scroll.Stop()
	return serve(ctx, runtime, events, move.C, scroll.C, func() error { return outputErr })
}
