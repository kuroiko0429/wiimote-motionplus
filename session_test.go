//go:build linux

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDaemonReconnectsRealSessionsWithoutStaleResources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sessions, initialized, destroyed := 0, 0, 0
	monitored, stopped := 0, 0
	var previous []*os.File
	deps := daemonDeps{now: time.Now, sleep: func(context.Context, time.Duration) error { return nil }, log: func(string, ...any) {}}
	deps.scan = func() ([]deviceSet, error) {
		for _, f := range previous {
			if _, err := f.Stat(); err == nil {
				t.Fatal("re-enumerated before closing old session")
			}
		}
		return []deviceSet{{button: fmt.Sprint("buttons", sessions), gyro: fmt.Sprint("gyro", sessions)}}, nil
	}
	deps.load = func(string) (map[uint16]string, error) {
		b := defaultBindings()
		if sessions == 1 {
			b[btnA] = "right_click"
		}
		return b, nil
	}
	deps.session = func(ctx context.Context, d deviceSet, o options, b map[uint16]string) error {
		sessions++
		if d.motion != fmt.Sprint("gyro", sessions-1) {
			t.Fatal("stale input path")
		}
		motion, mw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer mw.Close()
		button, bw, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer bw.Close()
		ui, err := os.CreateTemp(t.TempDir(), "ui")
		if err != nil {
			t.Fatal(err)
		}
		previous = []*os.File{motion, button, ui}
		sd := sessionDeps{telemetry: func(ctx context.Context, d deviceSet, o options) func() { monitored++; return func() { stopped++ } }, open: func(path string) (*os.File, error) {
			if path == d.motion {
				return motion, nil
			}
			return button, nil
		}, create: func() (*os.File, error) { return ui, nil }, destroy: func(f *os.File) { destroyed++; f.Close() }, readAbs: func(*os.File, uint16) (int32, error) { initialized++; return int32(sessions), nil }}
		done := make(chan struct{})
		go func() {
			defer close(done)
			if sessions == 3 {
				time.Sleep(20 * time.Millisecond)
				cancel()
				return
			}
			event := make([]byte, 24)
			binary.NativeEndian.PutUint16(event[16:], evKey)
			binary.NativeEndian.PutUint16(event[18:], btnA)
			binary.NativeEndian.PutUint32(event[20:], 1)
			bw.Write(event)
			if sessions == 2 {
				event = make([]byte, 24)
				binary.NativeEndian.PutUint16(event[18:], 3)
				bw.Write(event)
			}
			bw.Close()
		}()
		err = runSessionWith(ctx, d, o, b, sd)
		<-done
		if sessions < 3 && err == nil {
			t.Fatal("disconnect/drop not reported")
		}
		data, e := os.ReadFile(ui.Name())
		if e != nil {
			t.Fatal(e)
		}
		if sessions < 3 {
			code := uint16(btnLeft)
			if sessions == 2 {
				code = btnRight
			}
			if len(data) < 24 || binary.NativeEndian.Uint16(data[18:]) != code {
				t.Fatalf("stale bindings/output: %v", data)
			}
		} else if len(data) != 0 {
			t.Fatalf("stale motion: %v", data)
		}
		return err
	}
	if err := daemonLoop(ctx, options{mode: "gyro", gyroSensitivity: 10}, deps); err != nil {
		t.Fatal(err)
	}
	if monitored != 3 || stopped != 3 {
		t.Fatalf("telemetry started=%d stopped=%d", monitored, stopped)
	}
	if sessions != 3 || initialized != 6 || destroyed != 3 {
		t.Fatalf("sessions=%d axes=%d destroyed=%d", sessions, initialized, destroyed)
	}
	for _, f := range previous {
		if _, err := f.Stat(); err == nil {
			t.Fatal("last session leaked")
		}
	}
}

func TestUinputSetupFailureClosesDescriptor(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-uinput")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setupUinput(f); err == nil {
		t.Fatal("non-device accepted")
	}
	if _, err := f.Stat(); err == nil {
		f.Close()
		t.Fatal("failed uinput setup leaked descriptor")
	}
}

func TestSessionClosesResourcesBeforeReturning(t *testing.T) {
	for _, ending := range []string{"cancel", "disconnect", "dropped", "axis-error", "button-open-error", "uinput-error"} {
		t.Run(ending, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			motion, mw, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer mw.Close()
			button, bw, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer bw.Close()
			defer motion.Close()
			defer button.Close()
			ui, err := os.CreateTemp(t.TempDir(), "ui")
			if err != nil {
				t.Fatal(err)
			}
			defer ui.Close()
			destroyed := 0
			deps := sessionDeps{
				open: func(path string) (*os.File, error) {
					if path == "motion" {
						return motion, nil
					}
					if ending == "button-open-error" {
						return nil, fmt.Errorf("open race")
					}
					return button, nil
				},
				create: func() (*os.File, error) {
					if ending == "uinput-error" {
						return nil, fmt.Errorf("permissions")
					}
					return ui, nil
				},
				destroy: func(f *os.File) { destroyed++; f.Close() },
				readAbs: func(*os.File, uint16) (int32, error) {
					if ending == "axis-error" {
						return 0, fmt.Errorf("removed")
					}
					return 0, nil
				},
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				switch ending {
				case "cancel":
					time.Sleep(20 * time.Millisecond)
					cancel()
				case "disconnect":
					bw.Close()
				case "dropped":
					b := make([]byte, 24)
					binary.NativeEndian.PutUint16(b[16:], evSyn)
					binary.NativeEndian.PutUint16(b[18:], 3)
					bw.Write(b)
				}
			}()
			err = runSessionWith(ctx, deviceSet{motion: "motion", button: "button", mode: "gyro"}, options{gyroSensitivity: 10}, defaultBindings(), deps)
			<-done
			if ending == "cancel" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("lost session error")
			}
			if _, err := motion.Stat(); err == nil {
				t.Fatal("motion leaked")
			}
			if ending != "button-open-error" {
				if _, err := button.Stat(); err == nil {
					t.Fatal("button leaked")
				}
			}
			wantDestroyed := 1
			if strings.HasSuffix(ending, "open-error") || ending == "uinput-error" {
				wantDestroyed = 0
			}
			if destroyed != wantDestroyed {
				t.Fatalf("destroyed=%d", destroyed)
			}
		})
	}
}

func TestSessionCancelledDuringStartupDoesNotCreateUinput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer wr.Close()
	opened, created := 0, 0
	deps := sessionDeps{open: func(string) (*os.File, error) { opened++; cancel(); return rd, nil }, create: func() (*os.File, error) { created++; return nil, fmt.Errorf("unexpected") }}
	if err := runSessionWith(ctx, deviceSet{motion: "motion", button: "button"}, options{}, defaultBindings(), deps); err != nil {
		t.Fatal(err)
	}
	if opened != 1 || created != 0 {
		t.Fatalf("opened=%d created=%d", opened, created)
	}
	if _, err := rd.Stat(); err == nil {
		t.Fatal("startup resource leaked")
	}
}
