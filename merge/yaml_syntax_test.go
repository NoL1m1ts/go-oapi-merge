package merge

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
)

func TestOapiYamlSyntaxRepresentations(t *testing.T) {
	representations := []struct {
		name string
		doc  string
	}{
		{"block", `openapi: 3.2.0
info:
  title: Syntax API
  version: '1.0'
paths:
  /items:
    get:
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema:
                type: object
              example:
                text: 'value: # literal'
                flags: [true, false, null]
                numbers: [0, -17, 1.25]
                emptyMap: {}
                emptyList: []
`},
		{"flow", `{
openapi: 3.2.0,
info: {title: Syntax API, version: '1.0'},
paths: {/items: {get: {responses: {'200': {
description: OK,
content: {application/json: {schema: {type: object},
example: {text: 'value: # literal', flags: [true, false, null],
numbers: [0, -17, 1.25], emptyMap: {}, emptyList: []}}}
}}}}}
}
`},
		{"json", `{"openapi":"3.2.0","info":{"title":"Syntax API","version":"1.0"},"paths":{"/items":{"get":{"responses":{"200":{"description":"OK","content":{"application/json":{"schema":{"type":"object"},"example":{"text":"value: # literal","flags":[true,false,null],"numbers":[0,-17,1.25],"emptyMap":{},"emptyList":[]}}}}}}}}}` + "\n"},
	}
	transports := []struct {
		name      string
		transform func(string) string
	}{
		{"lf", func(s string) string { return s }},
		{"crlf", func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }},
		{"bom_lf", func(s string) string { return "\ufeff" + s }},
		{"bom_crlf", func(s string) string { return "\ufeff" + strings.ReplaceAll(s, "\n", "\r\n") }},
		{"no_final_newline", func(s string) string { return strings.TrimSuffix(s, "\n") }},
	}
	want := syntaxDecodeDocument(t, []byte(representations[0].doc))
	for _, representation := range representations {
		for _, transport := range transports {
			t.Run(representation.name+"/"+transport.name, func(t *testing.T) {
				doc := transport.transform(representation.doc)
				if got := syntaxDecodeDocument(t, []byte(doc)); !reflect.DeepEqual(got, want) {
					t.Fatalf("fixture differs from canonical representation:\ngot: %#v\nwant: %#v", got, want)
				}
				syntaxAssertRoundTrip(t, doc)
			})
		}
	}
}

func TestOapiYamlSyntaxScalarPreservation(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"single_quotes", "'it''s a value: # and \\\\ remain literal'"},
		{"double_quotes", `"quote: \"; slash: \\; tab: \t; newline: \n"`},
		{"unicode", `"Каталог / 日本語 / café / 😀 / e\u0301"`},
		{"unicode_escapes", `"\u041A\u043E\u0442 \U0001F408"`},
		{"ambiguous_strings", "['null', 'true', 'false', 'yes', 'no', 'on', 'off', '0123', '1e3', '2026-09-24', '']"},
		{"plain_punctuation", "https://example.invalid/a?x=1&y=2#fragment"},
		{"literal_clip", "|\n  first line\n  second line\n"},
		{"literal_strip", "|-\n  first line\n  second line\n\n"},
		{"literal_keep", "|+\n  first line\n  second line\n\n\n"},
		{"literal_indentation", "|2-\n    leading spaces\n      more spaces\n"},
		{"literal_blank_lines", "|-\n\n  first line\n\n  third line\n"},
		{"folded_clip", ">\n  first line\n  second line\n"},
		{"folded_strip", ">-\n  first line\n  second line\n\n"},
		{"folded_keep", ">+\n  first line\n  second line\n\n\n"},
		{"folded_paragraphs", ">-\n  first paragraph\n  continued\n\n  second paragraph\n"},
		{"folded_indented", ">-\n  ordinary\n    indented\n    continued\n  ordinary again\n"},
		{"empty_literal", "|\n"},
		{"empty_folded", ">-\n"},
		{"null_spellings", "{explicit: null, capitalized: Null, uppercase: NULL, tilde: ~, empty: }"},
		{"empty_collections", "{object: {}, array: [], nested: [{}, [], {object: {}, array: [], nil: null}]}"},
		{"numeric_values", "{zero: 0, negative: -1, large: 9007199254740993, fraction: 0.125, exponent: 1.25e+3}"},
		{"tagged_strings", "{boolean: !!str true, number: !!str 123, nilText: !!str null}"},
		{"quoted_keys", "{'': empty, 'a/b': slash, 'a~b': tilde, '200': status, 'true': boolean, 'ключ': unicode}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syntaxAssertRoundTrip(t, "openapi: 3.2.0\ninfo: {title: Syntax API, version: '1.0'}\npaths: {}\nx-data: "+tt.value+"\n")
		})
	}
}

func TestOapiYamlSyntaxAliasesAndMergeKeys(t *testing.T) {
	tests := []struct {
		name string
		doc  string
	}{
		{"scalar_alias", `openapi: 3.2.0
info: {title: &title 'Каталог', version: '1.0', description: *title}
paths: {}
x-data: [*title, *title]
`},
		{"mapping_alias", `openapi: 3.2.0
info: {title: Syntax API, version: '1.0'}
paths: {}
x-source: &source {empty: {}, nil: null, nested: [one, two]}
x-data: {first: *source, second: *source}
`},
		{"sequence_alias", `openapi: 3.2.0
info: {title: Syntax API, version: '1.0'}
paths: {}
x-source: &source [null, [], {}, {text: 'literal $ref'}]
x-data: {first: *source, second: *source}
`},
		{"root_merge", `x-defaults: &defaults
  openapi: 3.2.0
  info: {title: Syntax API, version: '1.0'}
  paths: {}
<<: *defaults
`},
		{"merge_then_override", `openapi: 3.2.0
info: {title: Syntax API, version: '1.0'}
paths: {}
x-defaults: &defaults {one: inherited, two: original}
x-data:
  <<: *defaults
  two: replaced
`},
		{"override_then_merge", `openapi: 3.2.0
info: {title: Syntax API, version: '1.0'}
paths: {}
x-defaults: &defaults {one: inherited, two: original}
x-data:
  two: replaced
  <<: *defaults
`},
		{"merge_sequence_precedence", `openapi: 3.2.0
info: {title: Syntax API, version: '1.0'}
paths: {}
x-first: &first {shared: first, a: one}
x-second: &second {shared: second, b: two}
x-data:
  <<: [*first, *second]
  b: replaced
`},
		{"operation_aliases", `openapi: 3.2.0
info: {title: Syntax API, version: '1.0'}
paths:
  /first:
    get: &operation
      responses:
        '200':
          description: OK
          content:
            application/json:
              schema: &schema {type: object, properties: {text: {type: string}}}
              example: &example {text: 'literal', nested: [null, {}]}
  /second:
    get: *operation
components:
  schemas:
    Item: *schema
x-data: *example
`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syntaxAssertRoundTrip(t, tt.doc)
		})
	}
}

func TestOapiYamlSyntaxRejectsMalformedDocuments(t *testing.T) {
	valid := "openapi: 3.2.0\ninfo: {title: Syntax API, version: '1.0'}\npaths: {}\n"
	tests := []struct {
		name string
		doc  string
	}{
		{"duplicate_root_key", valid + "openapi: 3.1.0\n"},
		{"duplicate_nested_key", valid + "x-data:\n  same: one\n  same: two\n"},
		{"duplicate_flow_key", valid + "x-data: {same: one, same: two}\n"},
		{"duplicate_json_key", `{"openapi":"3.2.0","info":{"title":"Syntax API","version":"1.0"},"paths":{},"x-data":{"same":1,"same":2}}`},
		{"multiple_documents", valid + "---\n" + valid},
		{"trailing_empty_document", valid + "---\n"},
		{"trailing_scalar_document", valid + "---\nignored\n"},
		{"document_after_end", valid + "...\n---\n" + valid},
		{"undefined_alias", valid + "x-data: *undefined\n"},
		{"incomplete_flow_sequence", valid + "x-data: [one, two\n"},
		{"unterminated_string", valid + "x-data: 'unterminated\n"},
		{"tab_indentation", valid + "x-data:\n\tvalue: invalid\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			input, output := filepath.Join(dir, "input.yaml"), filepath.Join(dir, "out.yaml")
			writeFile(t, input, tt.doc)
			writeFile(t, output, "existing output\n")
			if err := OapiYaml(input, output); err == nil {
				t.Fatal("expected an error for malformed or multi-document YAML")
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "existing output\n" {
				t.Errorf("failed merge overwrote existing output: %q", data)
			}
		})
	}
}

func TestOapiYamlSyntaxReferencedDocuments(t *testing.T) {
	root := "openapi: 3.2.0\ninfo: {title: Syntax API, version: '1.0'}\n"
	contexts := []struct {
		name   string
		root   string
		prefix string
		indent string
		path   []string
	}{
		{
			name: "path_item",
			root: root + "paths:\n  /test:\n    $ref: './defs.yaml#/Item'\n",
			path: []string{"paths", "/test"},
		},
		{
			name: "inline_schema",
			root: root + "paths: {}\ncomponents:\n  schemas:\n    Result:\n      $ref: './defs.yaml#/Item'\n",
			path: []string{"components", "schemas", "Result"},
		},
		{
			name:   "component_import",
			root:   root + "paths: {}\ncomponents:\n  schemas:\n    Result:\n      $ref: './defs.yaml#/components/schemas/Item'\n",
			prefix: "components:\n  schemas:\n",
			indent: "    ",
			path:   []string{"components", "schemas", "Item"},
		},
	}
	variants := []struct {
		name      string
		transform func(string) string
		wantError bool
	}{
		{"lf", func(s string) string { return s }, false},
		{"crlf", func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }, false},
		{"bom_lf", func(s string) string { return "\ufeff" + s }, false},
		{"bom_crlf", func(s string) string { return "\ufeff" + strings.ReplaceAll(s, "\n", "\r\n") }, false},
		{"document_markers", func(s string) string { return "---\n" + s + "...\n" }, false},
		{"merge_override", func(s string) string {
			position := strings.Index(s, "Item:")
			indent := position - strings.LastIndex(s[:position], "\n") - 1
			return "x-defaults: &defaults {description: Original, x-data: {text: 'must be replaced'}}\n" +
				strings.Replace(s, "Item:\n", "Item:\n"+strings.Repeat(" ", indent+2)+"<<: *defaults\n", 1)
		}, false},
		{"duplicate_key", func(s string) string { return s + "x-invalid: {same: one, same: two}\n" }, true},
		{"multiple_documents", func(s string) string { return s + "---\n" + s }, true},
		{"trailing_empty_document", func(s string) string { return s + "---\n" }, true},
	}
	value := "Item:\n  description: Final\n  x-data: {text: 'unicode 猫', values: [null, {}, []]}\n"
	want := syntaxDecodeDocument(t, []byte(value))["Item"]
	for _, context := range contexts {
		for _, variant := range variants {
			t.Run(context.name+"/"+variant.name, func(t *testing.T) {
				dir := t.TempDir()
				input, output := filepath.Join(dir, "input.yaml"), filepath.Join(dir, "out.yaml")
				body := strings.TrimSuffix(value, "\n")
				body = context.prefix + context.indent + strings.ReplaceAll(body, "\n", "\n"+context.indent) + "\n"
				defs := variant.transform(body)
				if !variant.wantError {
					syntaxDecodeDocument(t, []byte(defs))
				}
				writeFile(t, input, context.root)
				writeFile(t, filepath.Join(dir, "defs.yaml"), defs)
				err := OapiYaml(input, output)
				if variant.wantError {
					if err == nil {
						t.Fatal("expected an error for malformed or multi-document reference file")
					}
					return
				}
				if err != nil {
					t.Fatalf("merge: %v", err)
				}
				data, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				var got any = syntaxDecodeDocument(t, data)
				for _, key := range context.path {
					mapping, ok := got.(map[string]any)
					if !ok {
						t.Fatalf("expected mapping before key %q, got %#v", key, got)
					}
					got = mapping[key]
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("reference target changed:\ngot: %#v\nwant: %#v", got, want)
				}
				syntaxAssertRoundTrip(t, string(data))
			})
		}
	}
}

func syntaxAssertRoundTrip(t *testing.T, doc string) {
	t.Helper()
	want := syntaxDecodeDocument(t, []byte(doc))
	dir := t.TempDir()
	input := filepath.Join(dir, "input.yaml")
	writeFile(t, input, doc)
	var first []byte
	for pass := 0; pass < 3; pass++ {
		output := filepath.Join(dir, fmt.Sprintf("output-%d.yaml", pass))
		source := input
		if pass == 2 {
			source = filepath.Join(dir, "output-0.yaml")
		}
		if err := OapiYaml(source, output); err != nil {
			t.Fatalf("merge pass %d: %v", pass, err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		got := syntaxDecodeDocument(t, data)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("merge pass %d changed YAML values:\ngot: %#v\nwant: %#v\noutput:\n%s", pass, got, want, data)
		}
		if pass == 0 {
			first = data
		} else if !bytes.Equal(data, first) {
			t.Errorf("merge pass %d changed output bytes:\nfirst:\n%s\ncurrent:\n%s", pass, first, data)
		}
	}
}

func syntaxDecodeDocument(t *testing.T, data []byte) map[string]any {
	t.Helper()
	decoder := yamlv3.NewDecoder(bytes.NewReader(data))
	var doc map[string]any
	if err := decoder.Decode(&doc); err != nil {
		t.Fatalf("YAML decoder rejected document: %v\n%s", err, data)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("expected one YAML document, trailing decode = %v", err)
	}
	return doc
}
