//go:build linux

package main

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	scanTimeout    = 30 * time.Second
	knownTimeout   = 1 * time.Second
	connectTimeout = 15 * time.Second
	connectRetries = 3
)

// wiimoteAdvertRe matches the Bluetooth name any Wii Remote advertises:
// "Nintendo RVL-CNT-01" (original) or "Nintendo RVL-CNT-01-TR" (Wii Remote Plus).
var wiimoteAdvertRe = regexp.MustCompile(`(?:^|\s)Device ([0-9A-Fa-f]{2}(?::[0-9A-Fa-f]{2}){5}) Nintendo RVL-CNT-01(?:-TR)?\s*$`)

func namedWiiRemotes(output string) []string {
	var remotes []string
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		if m := wiimoteAdvertRe.FindStringSubmatch(line); m != nil {
			mac := strings.ToUpper(m[1])
			if !seen[mac] {
				remotes = append(remotes, mac)
				seen[mac] = true
			}
		}
	}
	return remotes
}

// connectWiimote drives bluetoothctl to find and connect any nearby Wii
// Remote, regardless of generation or whether it's been paired before.
//
// A Wii Remote has no power button and never reconnects on its own; the
// user always has to press 1+2 to make it connectable. But bluetoothctl
// only prints a device's name when it's newly discovered - for a Wii
// Remote paired in a previous run, "scan on" gets silent RSSI updates with
// no name, so the name-matching scan below would never see it. "devices"
// lists cached names for already-paired devices, so that's checked first;
// scanning is only needed the first time a given controller is paired.
func connectWiimote() (string, error) { return connectWiimoteContext(context.Background()) }

func connectWiimoteContext(parent context.Context) (mac string, err error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bluetoothctl")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return "", err
	}
	readerDone := make(chan struct{})
	defer func() {
		fmt.Fprintln(stdin, "quit")
		cancel()
		stdin.Close()
		cmd.Wait()
		<-readerDone
	}()

	lines := make(chan string, 256)
	go func() {
		defer close(readerDone)
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	fmt.Fprintln(stdin, "power on")
	fmt.Fprintln(stdin, "agent NoInputNoOutput")
	fmt.Fprintln(stdin, "default-agent")

	fmt.Println("Press 1+2 on the Wii Remote to make it discoverable (blue LEDs will blink).")

	fmt.Fprintln(stdin, "devices")
	mac = collectMacContext(ctx, lines, knownTimeout)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	if mac == "" {
		fmt.Fprintln(stdin, "scan on")
		mac = collectMacContext(ctx, lines, scanTimeout)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		fmt.Fprintln(stdin, "scan off")
	}

	if mac == "" {
		return "", fmt.Errorf("no Wii Remote found")
	}
	fmt.Printf("Found: %s\n", mac)
	fmt.Fprintf(stdin, "trust %s\n", mac)

	for attempt := 1; attempt <= connectRetries; attempt++ {
		fmt.Fprintf(stdin, "connect %s\n", mac)

		connected := false
		deadline := time.After(connectTimeout)
	connect:
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					break connect
				}
				if strings.Contains(line, "Connection successful") {
					connected = true
					break connect
				}
				if strings.Contains(line, "Failed to connect") {
					break connect
				}
			case <-ctx.Done():
				return "", ctx.Err()
			case <-deadline:
				break connect
			}
		}

		if connected {
			return mac, nil
		}
		fmt.Printf("Connect failed (%d/%d), retrying\n", attempt, connectRetries)
	}

	return mac, fmt.Errorf("failed to connect to %s", mac)
}

func collectMac(lines <-chan string, timeout time.Duration) string {
	return collectMacContext(context.Background(), lines, timeout)
}

func collectMacContext(ctx context.Context, lines <-chan string, timeout time.Duration) string {
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return ""
			}
			if m := wiimoteAdvertRe.FindStringSubmatch(line); m != nil {
				return m[1]
			}
		case <-ctx.Done():
			return ""
		case <-deadline:
			return ""
		}
	}
}
