package main

import (
	"context"
	"testing"
	"time"
)

func TestZeroBatteryWarningDisablesNotificationsButKeepsTelemetry(t *testing.T) {
	percent := 5
	logged := false
	m := telemetryMonitor{
		options: telemetryOptions{WarningPercent: 0},
		log:     func(string, ...any) { logged = true },
		deps: telemetryDeps{
			read: func(deviceSet) (RemoteInfo, error) {
				return RemoteInfo{MAC: "AA:BB:CC:DD:EE:FF", BatteryPercent: &percent}, nil
			},
			now: func() time.Time { return time.Unix(100, 0) },
			notify: func(context.Context, string, string, string) error {
				t.Error("disabled notification was sent")
				return nil
			},
			registry: &telemetryRegistry{},
		},
	}
	m.sample(context.Background(), deviceSet{})
	if !logged {
		t.Fatal("telemetry logging unexpectedly disabled")
	}
}
