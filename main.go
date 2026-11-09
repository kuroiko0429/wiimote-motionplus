//go:build linux

package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
	"unsafe"
)

const (
	sensitivityDefault = 0.35
	deadzone           = 1
	sensitivityStep    = 0.05
	sensitivityMin     = 0.05
	sensitivityMax     = 2.0

	moveInterval   = 16 * time.Millisecond
	scrollInterval = 80 * time.Millisecond
)

const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03

	absRX = 0x03
	absRY = 0x04
	absRZ = 0x05

	relX      = 0x00
	relY      = 0x01
	relHWheel = 0x06
	relWheel  = 0x08

	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112

	btnA    = 0x130
	btnB    = 0x131
	btn1    = 0x101
	btn2    = 0x102
	btnMode = 0x13c

	keyUp       = 103
	keyDown     = 108
	keyLeft     = 105
	keyRight    = 106
	keyNext     = 0x197
	keyPrevious = 0x19c
)

type inputID struct {
	busType uint16
	vendor  uint16
	product uint16
	version uint16
}

type uinputSetup struct {
	id           inputID
	name         [80]byte
	ffEffectsMax uint32
}

func iocNr(dir, typ, nr, size uintptr) uintptr {
	return dir<<30 | typ<<8 | nr | size<<16
}
func ioW(typ byte, nr, size uintptr) uintptr { return iocNr(1, uintptr(typ), nr, size) }
func ioR(typ byte, nr, size uintptr) uintptr { return iocNr(2, uintptr(typ), nr, size) }
func ioC(typ byte, nr uintptr) uintptr       { return iocNr(0, uintptr(typ), nr, 0) }

var (
	uiSetEvbit   = ioW('U', 100, 4)
	uiSetKeybit  = ioW('U', 101, 4)
	uiSetRelbit  = ioW('U', 102, 4)
	uiDevCreate  = ioC('U', 1)
	uiDevDestroy = ioC('U', 2)
	uiDevSetup   = ioW('U', 3, unsafe.Sizeof(uinputSetup{}))
)

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func ioctlInt(fd uintptr, req uintptr, val int) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(val)); errno != 0 {
		return errno
	}
	return nil
}

func createUinput() (*os.File, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}

	return setupUinput(f)
}

func setupUinput(f *os.File) (*os.File, error) {
	ready := false
	defer func() {
		if !ready {
			f.Close()
		}
	}()
	if err := ioctlInt(f.Fd(), uiSetEvbit, evKey); err != nil {
		return nil, err
	}
	if err := ioctlInt(f.Fd(), uiSetEvbit, evRel); err != nil {
		return nil, err
	}
	for _, b := range []int{btnLeft, btnRight, btnMiddle} {
		if err := ioctlInt(f.Fd(), uiSetKeybit, b); err != nil {
			return nil, err
		}
	}
	for _, b := range []int{relX, relY, relWheel, relHWheel} {
		if err := ioctlInt(f.Fd(), uiSetRelbit, b); err != nil {
			return nil, err
		}
	}

	var setup uinputSetup
	copy(setup.name[:], "wiimote-air-mouse")
	setup.id.busType = 0x03 // BUS_USB
	setup.id.version = 1

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uiDevSetup, uintptr(unsafe.Pointer(&setup))); errno != 0 {
		return nil, errno
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uiDevCreate, 0); errno != 0 {
		return nil, errno
	}
	ready = true
	return f, nil
}

func writeEvent(f *os.File, typ, code uint16, value int32) error {
	buf := make([]byte, 24)
	now := time.Now()
	binary.NativeEndian.PutUint64(buf[0:8], uint64(now.Unix()))
	binary.NativeEndian.PutUint64(buf[8:16], uint64(now.Nanosecond()/1000))
	binary.NativeEndian.PutUint16(buf[16:18], typ)
	binary.NativeEndian.PutUint16(buf[18:20], code)
	binary.NativeEndian.PutUint32(buf[20:24], uint32(value))
	n, err := f.Write(buf)
	if err == nil && n != len(buf) {
		err = io.ErrShortWrite
	}
	return err
}

func syn(f *os.File) {
	writeEvent(f, evSyn, 0, 0)
}

type rawEvent struct {
	typ, code uint16
	value     int32
}

type source int

const (
	srcAccel source = iota
	srcButton
)

type taggedEvent struct {
	src source
	ev  rawEvent
	err error
	at  time.Time
}

// io.ReadFull retains split input_event records instead of discarding tails.
func readEvents(ctx context.Context, f io.Reader, src source, out chan<- taggedEvent) {
	buf := make([]byte, 24)
	for {
		_, err := io.ReadFull(f, buf)
		te := taggedEvent{src: src, err: err, at: time.Now()}
		if err == nil {
			te.ev = rawEvent{binary.NativeEndian.Uint16(buf[16:18]), binary.NativeEndian.Uint16(buf[18:20]), int32(binary.NativeEndian.Uint32(buf[20:24]))}
		}
		select {
		case out <- te:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func applyDeadzone(raw int32, sensitivity float64) int32 {
	if raw > -deadzone && raw < deadzone {
		return 0
	}
	sign := int32(1)
	if raw < 0 {
		sign = -1
	}
	return int32(float64(raw-deadzone*sign) * sensitivity)
}

func main() {
	o, err := parseOptions(os.Args[1:], os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err = run(o); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var configErr *configurationError
		if errors.As(err, &configErr) {
			os.Exit(78)
		}
		os.Exit(1)
	}
}
func readAbsValue(f *os.File, code uint16) (int32, error) {
	// struct input_absinfo: value, minimum, maximum, fuzz, flat, resolution.
	var info [6]int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), ioR('E', uintptr(0x40+code), unsafe.Sizeof(info)), uintptr(unsafe.Pointer(&info[0]))); errno != 0 {
		return 0, errno
	}
	return info[0], nil
}

func run(o options) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return runContext(ctx, o, daemonDeps{connect: autoConnect, scan: scanDevices, load: loadButtonConfig, session: runSession, now: time.Now, sleep: sleepContext, log: func(format string, args ...any) { fmt.Printf(format+"\n", args...) }})
}

// A bad config is not a transient device failure. systemd must not restart it
// repeatedly or keep using the previous connection's mappings.
type configurationError struct{ err error }

func (e *configurationError) Error() string { return "configuration: " + e.err.Error() }
func (e *configurationError) Unwrap() error { return e.err }

func runContext(ctx context.Context, o options, deps daemonDeps) error {
	if o.status {
		groups, err := deps.scan()
		if err != nil {
			return err
		}
		if len(groups) == 0 {
			deps.log("No connected Wii Remote found.")
			return nil
		}
		for _, d := range groups {
			info, err := readRemoteInfo(d)
			if err != nil {
				deps.log("Remote information unavailable: %v", err)
				continue
			}
			deps.log("%s", formatRemoteInfo(info))
		}
		return nil
	}
	bindings, err := deps.load(o.configPath)
	if err != nil {
		return &configurationError{err}
	}
	if o.daemon && o.connect {
		return fmt.Errorf("--daemon cannot be combined with --connect")
	}
	if ctx.Err() != nil {
		return nil
	}
	path, err := instanceLockPath()
	if err != nil {
		return err
	}
	lock, err := acquireInstanceLock(path)
	if err != nil {
		return err
	}
	defer lock.Close()
	if o.daemon {
		return daemonLoop(ctx, o, deps)
	}
	if o.connect {
		mac, err := connectWiimoteContext(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Printf("Connected over Bluetooth: %s\n", mac)
	}
	d, err := waitDevicesContext(ctx, o.mode, 15*time.Second, deps)
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w; press 1+2 or use --connect to pair", err)
	}
	return deps.session(ctx, d, o, bindings)
}

func waitDevicesContext(ctx context.Context, mode string, timeout time.Duration, deps daemonDeps) (deviceSet, error) {
	deadline := deps.now().Add(timeout)
	for ctx.Err() == nil {
		groups, err := deps.scan()
		if err != nil {
			return deviceSet{}, err
		}
		d, err := selectDevices(groups, mode)
		if err == nil && (mode != "auto" || d.mode == "gyro" || !deps.now().Before(deadline)) {
			return d, nil
		}
		if !deps.now().Before(deadline) {
			return deviceSet{}, err
		}
		delay := 200 * time.Millisecond
		if remaining := deadline.Sub(deps.now()); remaining < delay {
			delay = remaining
		}
		if err := deps.sleep(ctx, delay); err != nil {
			return deviceSet{}, err
		}
	}
	return deviceSet{}, ctx.Err()
}
func serve(ctx context.Context, r *mouseRuntime, events <-chan taggedEvent, move, scroll <-chan time.Time, outputError func() error) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case te, ok := <-events:
			if !ok {
				return io.EOF
			}
			at := te.at
			if at.IsZero() {
				at = time.Now()
			}
			if err := r.event(te, at); err != nil {
				return err
			}
		case now := <-move:
			r.move(now)
		case <-scroll:
			r.scrollTick()
		}
		if err := outputError(); err != nil {
			return fmt.Errorf("uinput write: %w", err)
		}
	}
}
