# go-oapi-merge

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/NoL1m1ts/go-oapi-merge)](https://goreportcard.com/report/github.com/NoL1m1ts/go-oapi-merge)
[![Go Reference](https://pkg.go.dev/badge/github.com/NoL1m1ts/go-oapi-merge.svg)](https://pkg.go.dev/github.com/NoL1m1ts/go-oapi-merge)

`go-oapi-merge` is a CLI tool for merging OpenAPI YAML files. It resolves `$ref` references across multiple files and combines them into a single, unified OpenAPI specification. Perfect for managing large or modular OpenAPI projects.

---

## Features

- **Resolves `$ref` References**: Resolves references in OpenAPI and Schema Objects while preserving literal example/default data and vendor extensions.
- **Deterministic Output**: Repeated merges of the same input files produce identical output, while preserving property order within each source file.
- **OpenAPI 3.0, 3.1, and 3.2 Support**: Merges documents on any of these versions, including native OpenAPI 3.2 hierarchical tags (`tags[].parent`).
- **`webhooks` Support (OpenAPI 3.1+)**: `webhooks` entries are resolved and merged the same way as `paths` — a whole-item `$ref` to another file is fetched, inlined, and any refs nested inside it are followed and merged into `components` too.
- **Preserves Other Root-Level Fields**: `jsonSchemaDialect` (3.1), `$self`/`summary` (3.2), `externalDocs`, and vendor `x-*` extensions are carried through to the output unchanged, even though none of them get `$ref` resolution.
- **Simple CLI Interface**: Easy-to-use command-line tool for quick integration into your workflow.
- **Customizable Input/Output**: Specify input and output file paths for flexible usage.
- **Cross-Platform**: Built in Go, it works seamlessly on Windows, macOS, and Linux.

---

## OpenAPI Version Support

`go-oapi-merge` does not validate or restrict the `openapi` version field, whatever string is in the input document (e.g. `3.0.0`, `3.0.3`, `3.1.0`, `3.2.0`) is carried through unchanged to the output.

In practice this means:

- **OpenAPI 3.0.x**: Fully supported. This is the format the tool was originally built around.
- **OpenAPI 3.1.x**: Supported for the fields the merger models (`info`, `servers`, `paths`, `webhooks`, `components`, `security`, `tags`). `webhooks` gets the same `$ref` resolution as `paths` (see above).
- **OpenAPI 3.2.x**: Supported, including native hierarchical tags (see below).

Because the tool merges YAML structurally rather than validating against a JSON Schema for a specific OpenAPI version, it does not by itself guarantee the *output* is spec-valid — it guarantees it faithfully reflects the *input*. Run the result through an OpenAPI validator (e.g. [Redocly CLI](https://redocly.com/docs/cli/) or [Spectral](https://github.com/stoplightio/spectral)) as part of your pipeline if you need that guarantee.

### Root fields the merger preserves without processing

The merger models a fixed set of root fields that need active processing (`openapi`, `info`, `servers`, `paths`, `webhooks`, `components`, `security`, `tags`). Beyond those, it also recognizes and preserves `jsonSchemaDialect` (3.1), `$self`/`summary`/`externalDocs` (3.2, though `externalDocs` predates 3.1), and any root-level vendor `x-*` extension — copied through verbatim, in their original relative order, positioned after the modeled fields in the output. These additional fields are passed through without interpreting their contents as references.

Any *other* unrecognized root-level field (a hypothetical future OpenAPI addition not listed above) is still dropped, since it isn't in this preserved set either.

---

## Hierarchical Tags (OpenAPI 3.2)

OpenAPI 3.2 introduces native support for organizing tags into a hierarchy via the `parent` field on each tag object. `go-oapi-merge` preserves `tags[].parent` — along with `description`, `externalDocs`, and any other tag field — exactly as written. It is **not** converted into a vendor extension like `x-tagGroups`; `parent` is emitted as a native OpenAPI 3.2 field.

```yaml
openapi: 3.2.0

info:
  title: Example API
  version: 1.0.0

tags:
  - name: Catalog

  - name: Categories
    parent: Catalog

  - name: Items
    parent: Catalog

paths:
  /categories:
    get:
      tags:
        - Categories
      responses:
        "200":
          description: OK
```

Tags can also live in a modular project and be merged the same way as paths and components. See [`example/openapi3.2`](example/openapi3.2) for a complete multi-file example (`root.yaml` referencing `categories.yaml` and `items.yaml`) where the root document declares the tag hierarchy and the referenced files supply the tagged paths — run it with:

```bash
go-oapi-merge -input example/openapi3.2/root.yaml -output merged.yaml
```

### Limitations

- The merger does not validate that a tag's `parent` refers to a tag that actually exists elsewhere in the document, or that the hierarchy is free of cycles — it passes the `tags` array through as-is. Validate the merged output with an OpenAPI 3.2-aware validator if that guarantee matters to you.

---

## Installation

Install `go-oapi-merge` using `go install`:

```bash
go install github.com/NoL1m1ts/go-oapi-merge@latest
```

---

## Usage

### CLI Command

```bash
go-oapi-merge -input <input_file> -output <output_file>
```

#### Options

| Flag      | Description                              | Required | Default Value   |
|-----------|------------------------------------------|----------|-----------------|
| `-input`  | Path to the main OpenAPI file.           | No       | `api.yaml`      |
| `-output` | Path to save the merged OpenAPI file.    | No       | `merged_api.yaml` |

#### Examples

```bash
# Basic usage with default values
go-oapi-merge

# Specify custom input and output files
go-oapi-merge -input specs/main.yaml -output dist/merged.yaml

# Using relative paths
go-oapi-merge -input ./api/openapi.yaml -output ./dist/final.yaml
```

#### Error Handling

The tool will display error messages in the following cases:
- Input file doesn't exist or is not accessible
- Input file is not a valid YAML
- Output directory doesn't exist or is not writable
- Referenced files (`$ref`) are missing or invalid
- Invalid OpenAPI specification format

Example error output:
```bash
Error: open api.yaml: no such file or directory
```

---

## How It Works

1. **Reads the Main File**: The tool starts by reading the main OpenAPI file specified with the `-input` flag.
2. **Resolves References**: It identifies `$ref` references, reads the linked files, and merges their content into the main file.
3. **Saves the Result**: The final merged OpenAPI specification is saved to the file specified with the `-output` flag.

### YAML input and output

Each input file must contain one YAML document. Block and flow styles, JSON input, UTF-8 BOM, LF/CRLF, YAML aliases, and merge keys (`<<`) are supported. Explicit keys override merged keys; duplicate explicit keys, cyclic aliases, and excessive alias expansion are rejected. Dates retain their original text. Output normalizes formatting, expands aliases, and may change quoting or multiline style while preserving values.

Local file references may omit the fragment to select the entire document. Percent-encoded filenames and JSON Pointer fragments are decoded when resolving references.

### A note on `$ref` detection

Reference handling follows the OpenAPI/JSON Schema object structure. Literal `example` data, Example Object `value` fields, schema `default`/`const`/`enum`/`examples`, and vendor extensions are preserved, including literal `$ref` properties and YAML aliases. Real references in properties or named examples still resolve even when their names are `example`, `default`, `$ref`, or start with `x-`.

External component references are merged into `components`. Targets outside a component container, such as `definitions.yaml#/Pet`, are inlined with their nested references resolved relative to their source file. Cycles through these inlined targets are rejected with an explicit error; put recursive definitions under `components` to use local component references. Unresolved local JSON Pointer references produce an error before output is written.

Each batch of discovered dependency files is processed in lexical filename order. Within a file, category, component, and property order is preserved. Root components take precedence over dependencies; when dependency files define the same component, the first processed definition wins. This makes both component selection and output order repeatable without alphabetically sorting the original properties.

---

## Example Project Structure

Here’s an example of a modular OpenAPI project:

```
├── api.yaml
├── paths
│   ├── user.yaml
│   └── pet.yaml
└── components
    └── schemas.yaml
```

The `api.yaml` file contains references to other files:

```yaml
paths:
  /user:
    $ref: "./paths/user.yaml#/paths/~1user"
  /pet:
    $ref: "./paths/pet.yaml#/paths/~1pet"
```

After running `go-oapi-merge`, the tool will merge all referenced files into a single `merged_api.yaml`.

---

## Example Project Structure with Example Files

The repository includes example files in the `examples` directory that demonstrate the recommended structure and usage:

```
examples/
├── api.yaml                 # Main OpenAPI file
├── paths/
│   ├── users.yaml          # User endpoints
│   └── pets.yaml           # Pet endpoints
└── components/
    └── schemas.yaml        # Shared schemas
```

### Main File (api.yaml)
```yaml
openapi: 3.0.0
info:
  title: Example API
  version: 1.0.0
paths:
  /users:
    $ref: './paths/users.yaml#/paths/users'
  /pets:
    $ref: './paths/pets.yaml#/paths/pets'
components:
  schemas:
    $ref: './common/schemas.yaml#/common/schemas'
```

You can use these examples as a starting point:
```bash
# Copy examples to your project
cp -r examples/* your-project/

# Run merge on example files
go-oapi-merge -input examples/api.yaml -output merged_api.yaml
```

---

## Development

### Building from Source

1. Clone the repository:

   ```bash
   git clone https://github.com/NoL1m1ts/go-oapi-merge.git
   cd go-oapi-merge
   ```

2. Build the project:

   ```bash
   go build -o go-oapi-merge
   ```

3. Run the tool locally:

   ```bash
   ./go-oapi-merge -input api.yaml -output merged_api.yaml
   ```

### Contributing

We welcome contributions! Here’s how you can help:

1. Fork the repository.
2. Create a new branch for your feature or bugfix.
3. Submit a pull request with a detailed description of your changes.

Please ensure your code follows the project’s coding standards and includes appropriate tests.

---

### License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

---

### Author

Developed and maintained by [NoL1m1ts](https://github.com/NoL1m1ts).

---

### Support

If you encounter any issues or have questions, please [open an issue](https://github.com/NoL1m1ts/go-oapi-merge/issues) on GitHub.

---

**Happy merging!** 🚀
