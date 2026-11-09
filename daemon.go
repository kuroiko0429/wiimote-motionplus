//go:build linux

package main

import (
	"context"
	"fmt"
	"time"
)

// Each session owns its devices/readers/runtime. It must finish cleanup before
// returning: the next iteration re-enumerates paths and reloads button mappings.
type daemonDeps struct {
	connect func(context.Context)
	scan    func() ([]deviceSet, error)
	load    func(string) (map[uint16]string, error)
	session func(context.Context, deviceSet, options, map[uint16]string) error
	now     func() time.Time
	sleep   func(context.Context, time.Duration) error
	log     func(string, ...any)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func daemonLoop(ctx context.Context, o options, deps daemonDeps) error {
	deps.log("Waiting for Wii Remote (%s). Press 1+2 or SYNC; Bluetooth cannot wake a powered-off remote.", o.mode)
	var connectCancel context.CancelFunc
	var connectDone chan struct{}
	stopConnect := func() {
		if connectCancel != nil {
			connectCancel()
			<-connectDone
			connectCancel = nil
		}
	}
	defer stopConnect()
	startConnect := func() {
		if o.autoConnect && deps.connect != nil && connectCancel == nil {
			var worker context.Context
			worker, connectCancel = context.WithCancel(ctx)
			connectDone = make(chan struct{})
			go func() { defer close(connectDone); deps.connect(worker) }()
		}
	}
	var accelSince time.Time
	var accelIdentity string
	retryDelay := 200 * time.Millisecond
	var lastLog time.Time
	var lastError string
	for ctx.Err() == nil {
		groups, err := deps.scan()
		if err != nil {
			return fmt.Errorf("device discovery: %w", err)
		}
		d, err := selectDevices(groups, o.mode)
		if err != nil {
			accelSince = time.Time{}
			startConnect()
			if deps.sleep(ctx, 200*time.Millisecond) != nil {
				return nil
			}
			continue
		}
		stopConnect()
		if o.mode == "auto" && d.mode == "accel" {
			if accelSince.IsZero() || accelIdentity != d.identity {
				accelSince = deps.now()
				accelIdentity = d.identity
			}
			if deps.now().Sub(accelSince) < time.Second {
				if deps.sleep(ctx, 200*time.Millisecond) != nil {
					return nil
				}
				continue
			}
		}
		accelSince = time.Time{}
		bindings, err := deps.load(o.configPath)
		if err != nil {
			return &configurationError{fmt.Errorf("before connection: %w", err)}
		}
		started := deps.now()
		err = deps.session(ctx, d, o, bindings)
		if ctx.Err() != nil {
			return nil
		}
		if deps.now().Sub(started) >= 5*time.Second {
			retryDelay = 200 * time.Millisecond
		}
		message := fmt.Sprint(err)
		if lastLog.IsZero() || message != lastError || deps.now().Sub(lastLog) >= 10*time.Second {
			deps.log("Session ended: %v; waiting to reconnect (retry backoff up to 2s).", err)
			lastLog = deps.now()
			lastError = message
		}
		if deps.sleep(ctx, retryDelay) != nil {
			return nil
		}
		retryDelay *= 2
		if retryDelay > 2*time.Second {
			retryDelay = 2 * time.Second
		}
	}
	return nil
}
