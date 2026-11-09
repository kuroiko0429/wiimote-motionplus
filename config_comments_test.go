package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestStripLineCommentsPreservesStrings(t *testing.T) {
	for _, text := range []string{
		`{"url":"https://example.com/a//b","comment":"/* not a comment */"}`,
		`{"quote":"escaped \" // still string", "slash":"\\\\//still string"}`,
		`{"end":"backslash\\"}// comment`,
	} {
		want := text
		if text == `{"end":"backslash\\"}// comment` {
			want = `{"end":"backslash\\"}` + "          "
		}
		if got := string(stripLineComments([]byte(text))); got != want {
			t.Fatalf("strip %q: got %q want %q", text, got, want)
		}
	}
}

func TestStripLineCommentsPreservesPositions(t *testing.T) {
	for _, text := range []string{
		"// 日本語\r\n{\"buttons\":{}}//EOF",
		"// one\r{}//two\n//three\r\n",
		"//", "/", "", `"unterminated\`,
	} {
		input := []byte(text)
		original := append([]byte(nil), input...)
		got := stripLineComments(input)
		if !bytes.Equal(input, original) || len(got) != len(input) {
			t.Fatalf("input mutated or byte positions changed: %q -> %q", input, got)
		}
		for i, b := range input {
			if b == '\r' || b == '\n' {
				if got[i] != b {
					t.Fatalf("line ending at %d changed", i)
				}
			} else if got[i] != b && got[i] != ' ' {
				t.Fatalf("non-whitespace replacement at %d", i)
			}
		}
	}
	text := "// 日本語\r\n{\n  \"buttons\": @\n}"
	var value any
	err := json.Unmarshal(stripLineComments([]byte(text)), &value)
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) || syntax.Offset != int64(strings.Index(text, "@")+1) {
		t.Fatalf("decoder error offset not preserved: %v", err)
	}
}

func TestCommentSupportKeepsInvalidSyntaxStrict(t *testing.T) {
	for _, text := range []string{
		`/* block */ {}`, `{} /* block */`, `{"buttons":{},}`, `/ {}`, `{} /`,
		`{"buttons":{"a":"none",}}`, `{"buttons":{"a":"unterminated // text}}`,
		`{"buttons":{"a":"bad\q//text"}}`, "// comment only", "{} // comment\n{}",
		"// header\n{\"typo\":{}}", "// header\n{\"buttons\":{\"a\":\"https://example.com\"}}",
	} {
		t.Run(text, func(t *testing.T) {
			if _, err := loadButtonConfig(configFile(t, text)); err == nil {
				t.Fatalf("accepted invalid config %q", text)
			}
		})
	}
}

func TestConfigTemplatesDocumentBindings(t *testing.T) {
	for _, path := range []string{"config.default.json", "config.example.json"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for button := range buttonCodes {
			if !strings.Contains(text, "//") || !strings.Contains(text, button) {
				t.Errorf("%s missing documented button %s", path, button)
			}
		}
		for _, action := range []string{"none", "left_click", "right_click", "middle_click", "calibrate", "pause", "scroll_up", "scroll_down", "scroll_left", "scroll_right", "sensitivity_up", "sensitivity_down"} {
			if !strings.Contains(text, action) {
				t.Errorf("%s missing action %s", path, action)
			}
		}
		for _, line := range strings.Split(text, "\n") {
			if strings.Contains(line, `": "`) && !strings.Contains(line, "//") {
				t.Errorf("%s undocumented binding %s", path, line)
			}
		}
		for _, phrase := range []string{"Restart", "Omitted", "disable", "tap", "pause", "/*", "trailing commas"} {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s missing guidance %s", path, phrase)
			}
		}
		got, err := loadButtonConfig(path)
		want := defaultBindings()
		if path == "config.example.json" {
			want[btnA], want[btnB] = "right_click", "left_click"
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s mappings: %v %v", path, got, err)
		}
	}
}

func TestConfigLoadsLineComments(t *testing.T) {
	text := "// ボタン設定\r\n{\r\n  \"buttons\": {\n    \"a\": \"right_click\", // A の操作\n    \"home\": \"none\" // 無効\n  }\n} // EOF comment"
	got, err := loadButtonConfig(configFile(t, text))
	want := defaultBindings()
	want[btnA] = "right_click"
	want[btnMode] = "none"
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("commented config: %v %v", got, err)
	}
}
