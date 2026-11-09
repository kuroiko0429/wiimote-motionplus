//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var buttonCodes = map[string]uint16{
	"a": btnA, "b": btnB, "one": btn1, "two": btn2, "home": btnMode,
	"up": keyUp, "down": keyDown, "left": keyLeft, "right": keyRight,
	"plus": keyNext, "minus": keyPrevious,
}

func defaultBindings() map[uint16]string {
	return map[uint16]string{
		btnA: "left_click", btnB: "right_click", btn1: "calibrate", btn2: "middle_click", btnMode: "pause",
		keyUp: "scroll_up", keyDown: "scroll_down", keyLeft: "scroll_left", keyRight: "scroll_right",
		keyNext: "sensitivity_up", keyPrevious: "sensitivity_down",
	}
}

func loadButtonConfig(path string) (map[uint16]string, error) {
	bindings := defaultBindings()
	automatic := path == ""
	if automatic {
		root, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("config directory: %w", err)
		}
		path = filepath.Join(root, "wiimote-motionplus", "config.json")
	}
	data, err := os.ReadFile(path)
	if automatic && errors.Is(err, os.ErrNotExist) {
		return bindings, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(stripLineComments(data), &config); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	if config == nil {
		return nil, fmt.Errorf("config %s: expected JSON object", path)
	}
	for field, raw := range config {
		if field != "buttons" {
			return nil, fmt.Errorf("config %s: unknown field %q", path, field)
		}
		var buttons map[string]json.RawMessage
		if err := json.Unmarshal(raw, &buttons); err != nil || buttons == nil {
			return nil, fmt.Errorf("config %s: buttons must be an object", path)
		}
		for button, value := range buttons {
			code, ok := buttonCodes[button]
			if !ok {
				return nil, fmt.Errorf("config %s: unknown button %q", path, button)
			}
			var action string
			if err := json.Unmarshal(value, &action); err != nil || !validAction(action) {
				return nil, fmt.Errorf("config %s: invalid action for button %q: %s", path, button, value)
			}
			bindings[code] = action
		}
	}
	return bindings, nil
}

// stripLineComments replaces // comments with spaces, preserving byte offsets
// and line endings for the JSON decoder. All other syntax is validated by JSON.
func stripLineComments(data []byte) []byte {
	out := append([]byte(nil), data...)
	inString := false
	for i := 0; i < len(out); i++ {
		if inString {
			switch out[i] {
			case '\\':
				i++ // A JSON escape consumes the next byte, even an escaped quote.
			case '"':
				inString = false
			}
			continue
		}
		if out[i] == '"' {
			inString = true
			continue
		}
		if out[i] == '/' && i+1 < len(out) && out[i+1] == '/' {
			for i < len(out) && out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
				i++
			}
		}
	}
	return out
}

func validAction(action string) bool {
	switch action {
	case "none", "left_click", "right_click", "middle_click", "calibrate", "pause", "scroll_up", "scroll_down", "scroll_left", "scroll_right", "sensitivity_up", "sensitivity_down":
		return true
	}
	return false
}
