package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func configFile(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConfigAutomaticLookup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	bindings, err := loadButtonConfig("")
	if err != nil || bindings[btnA] != "left_click" {
		t.Fatalf("missing automatic: %v %v", bindings, err)
	}
	dir := filepath.Join(root, "wiimote-motionplus")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"buttons":{"a":"none"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	bindings, err = loadButtonConfig("")
	if err != nil || bindings[btnA] != "none" {
		t.Fatalf("XDG lookup: %v %v", bindings, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", root)
	if err := os.MkdirAll(filepath.Join(root, ".config"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, filepath.Join(root, ".config", "wiimote-motionplus")); err != nil {
		t.Fatal(err)
	}
	bindings, err = loadButtonConfig("")
	if err != nil || bindings[btnA] != "none" {
		t.Fatalf("HOME lookup: %v %v", bindings, err)
	}
	if _, err := loadButtonConfig(filepath.Join(root, "missing")); err == nil {
		t.Fatal("explicit missing accepted")
	}
	for _, text := range []string{`{}`, `{"buttons":{}}`} {
		bindings, err = loadButtonConfig(configFile(t, text))
		if err != nil || len(bindings) != 11 || bindings[btnA] != "left_click" {
			t.Fatalf("empty overrides: %v %v", bindings, err)
		}
	}
}

func TestConfigRejectsInvalidDocuments(t *testing.T) {
	for _, text := range []string{
		`{"typo":{}}`, `{"Buttons":{}}`, `{"buttons":{"unknown":"pause"}}`,
		`{"buttons":{"a":"shell"}}`, `{"buttons":{"a":true}}`, `{"buttons":{"a":null}}`,
		`{"buttons":null}`, `null`, `[]`, `{"buttons":[]}`, `{} {}`, `{} garbage`, ``,
	} {
		t.Run(text, func(t *testing.T) {
			if _, err := loadButtonConfig(configFile(t, text)); err == nil {
				t.Fatalf("accepted invalid config %s", text)
			}
		})
	}
}

func TestExampleSwapsOnlyAB(t *testing.T) {
	bindings, err := loadButtonConfig("config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	want := defaultBindings()
	want[btnA] = "right_click"
	want[btnB] = "left_click"
	if !reflect.DeepEqual(bindings, want) {
		t.Fatalf("example: %v", bindings)
	}
}

func TestConfigPartialOverridePreservesDefaults(t *testing.T) {
	bindings, err := loadButtonConfig(configFile(t, `{"buttons":{"a":"right_click","b":"none"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if bindings[btnA] != "right_click" || bindings[btnB] != "none" || bindings[btn1] != "calibrate" || bindings[keyUp] != "scroll_up" {
		t.Fatalf("bindings: %v", bindings)
	}
}
