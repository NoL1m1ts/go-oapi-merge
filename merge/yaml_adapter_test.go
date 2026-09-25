package merge

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestYAMLScalarPreservation(t *testing.T) {
	for _, value := range []string{"2026-09-24", "2026-09-24T01:02:03+03:00", "2026-09-24 01:02:03"} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
			writeFile(t, input, "openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths: {}\nx-date: "+value+"\n")
			if err := OapiYaml(input, output); err != nil {
				t.Fatal(err)
			}
			if got := mapValueAt(t, unmarshalDoc(t, output), "x-date"); got != value {
				t.Fatalf("date = %#v, want %q", got, value)
			}
		})
	}
}

func TestYAMLScalarAliasKeys(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
	writeFile(t, input, "openapi: 3.2.0\ninfo: {title: &title 'Some API', version: '1'}\npaths: {}\nx-data:\n  <<: {Some API: inherited}\n  ? *title\n  : explicit\n")
	if err := OapiYaml(input, output); err != nil {
		t.Fatal(err)
	}
	if got := mapValueAt(t, unmarshalDoc(t, output), "x-data", "Some API"); got != "explicit" {
		t.Fatalf("aliased key value = %#v", got)
	}
}

func TestYAMLDuplicateAliasKey(t *testing.T) {
	var value any
	err := decodeYAML([]byte("title: &title foo\nx-data: {foo: first, *title : second}\n"), &value)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate key error, got %v", err)
	}
}

func TestYAMLCyclicOverriddenAlias(t *testing.T) {
	var value any
	err := decodeYAML([]byte("x-data:\n  <<: &base\n    a: &self [*self]\n  a: value\n"), &value)
	if err == nil || !strings.Contains(err.Error(), "cyclic YAML alias") {
		t.Fatalf("expected controlled alias cycle error, got %v", err)
	}
}

func TestYAMLOverriddenAliasExpansion(t *testing.T) {
	var input strings.Builder
	input.WriteString("x-data:\n  <<:\n    a: &a [value]\n")
	previous := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g"} {
		input.WriteString("    " + name + ": &" + name + " [" + strings.TrimSuffix(strings.Repeat("*"+previous+", ", 10), ", ") + "]\n")
		previous = name
	}
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		input.WriteString("  " + name + ": overridden\n")
	}
	var value any
	if err := decodeYAML([]byte(input.String()), &value); err == nil || !strings.Contains(err.Error(), "alias") {
		t.Fatalf("expected alias expansion error, got %v", err)
	}
}

func TestYAMLUnicodeLineSeparators(t *testing.T) {
	for _, value := range []string{"a\u2028b", "a\u2029b"} {
		t.Run(strconv.Quote(value), func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "root.yaml"), filepath.Join(dir, "out.yaml")
			writeFile(t, input, "openapi: 3.2.0\ninfo: {title: Test, version: '1'}\npaths: {}\nx-text: "+strconv.Quote(value)+"\n")
			if err := OapiYaml(input, output); err != nil {
				t.Fatal(err)
			}
			if got := mapValueAt(t, unmarshalDoc(t, output), "x-text"); got != value {
				t.Fatalf("value = %#v, want %q", got, value)
			}
		})
	}
}
