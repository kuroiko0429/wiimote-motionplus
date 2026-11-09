package main

import (
	"bytes"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"
)

func TestConfigCLIRejectsExplicitEmptyPath(t *testing.T) {
	if _, err := parseOptions([]string{"--config", ""}, &bytes.Buffer{}); err == nil {
		t.Fatal("explicit empty --config accepted")
	}
}

func TestConfigCLIValidationBeforeHardwareOrConnect(t *testing.T) {
	path := configFile(t, `{"buttons":{"a":"execute_command"}}`)
	var output bytes.Buffer
	o, err := parseOptions([]string{"--config", path, "--connect"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if o.configPath != path {
		t.Fatalf("path %q", o.configPath)
	}
	start := time.Now()
	if err := run(o); err == nil || !strings.Contains(err.Error(), "invalid action") {
		t.Fatalf("early validation: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("config validation waited for hardware")
	}
	output.Reset()
	_, err = parseOptions([]string{"--help"}, &output)
	if !errors.Is(err, flag.ErrHelp) || !strings.Contains(output.String(), "-config") {
		t.Fatalf("help: %v %s", err, &output)
	}
}
