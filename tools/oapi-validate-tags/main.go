// Command oapi-validate-tags preprocesses an OpenAPI v3 YAML spec by
// injecting `x-oapi-codegen-extra-tags: { validate: "..." }` annotations
// onto every property whose JSON Schema constraints map to a
// go-playground/validator.v10 rule.
//
// This is a build-time step that runs immediately before oapi-codegen so
// the generated Go structs carry `validate:` tags. The mapping rules are:
//
//	format: email                          -> email
//	format: uuid                           -> uuid
//	format: uri | format: url              -> url
//	minLength: N        (string)           -> min=N
//	maxLength: N        (string)           -> max=N
//	pattern: ^...$      (string)           -> regexp=^...$
//	minimum: N          (number/integer)   -> min=N
//	maximum: N          (number/integer)   -> max=N
//	enum: [a, b, c]                        -> oneof=a b c
//	required                               -> required (else omitempty)
//
// Optional properties get a leading "omitempty" so validator skips the
// zero-value case (matches the json:"...,omitempty" tag oapi-codegen
// emits for non-required fields).
//
// Before annotation, local external path-item references are inlined into a
// copy of the root document. References from those path files back to the
// input document are rewritten as local #/components references so generators
// resolve every schema against the canonical component root.
//
// The input spec is read from argv[1]; the rewritten spec is written to
// argv[2]. The output is a derived artifact — it is regenerated each
// codegen run and should NOT be committed.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const derivedArtifactPerm = 0o600

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <input.yaml> <output.yaml>\n", os.Args[0])
		os.Exit(2)
	}
	in, out := os.Args[1], os.Args[2]

	if err := preprocess(in, out); err != nil {
		fail("%v", err)
	}
}

func preprocess(in, out string) error {
	root, err := readYAML(in)
	if err != nil {
		return err
	}
	if err := inlinePathItems(root.Content[0], in); err != nil {
		return fmt.Errorf("inline path items: %w", err)
	}
	walk(root.Content[0])

	buf, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	//nolint:gosec // Build tooling intentionally writes to the explicit output path supplied by its caller.
	if err := os.WriteFile(out, buf, derivedArtifactPerm); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	return nil
}

// readYAML reads the caller-selected root spec or a file reached through one
// of that spec's local path-item references.
func readYAML(path string) (*yaml.Node, error) {
	//nolint:gosec // Build-time local spec input is intentionally read from a resolved path.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 {
		return nil, fmt.Errorf("expected single-document YAML in %s", path)
	}
	return &root, nil
}

// inlinePathItems replaces local external references immediately under paths
// with copies of the referenced path items. Backreferences from those files to
// the canonical input document become local references, keeping one component
// root for oapi-codegen and kin-openapi.
func inlinePathItems(root *yaml.Node, inputPath string) error {
	paths := mapValue(root, "paths")
	if paths == nil {
		return nil
	}
	if paths.Kind != yaml.MappingNode {
		return fmt.Errorf("paths must be a mapping")
	}
	canonicalInput, err := filepath.Abs(inputPath)
	if err != nil {
		return fmt.Errorf("resolve input path: %w", err)
	}

	for i := 0; i+1 < len(paths.Content); i += 2 {
		pathName := paths.Content[i].Value
		pathItem := paths.Content[i+1]
		refNode := mapValue(pathItem, "$ref")
		if refNode != nil && refNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("path %q: $ref must be a string", pathName)
		}
		ref := scalarNodeValue(refNode)
		if refNode != nil && ref == "" {
			return fmt.Errorf("path %q: $ref must not be empty", pathName)
		}
		if ref == "" || strings.HasPrefix(ref, "#") {
			continue
		}
		refPath, fragment, err := splitExternalRef(ref)
		if err != nil {
			return fmt.Errorf("path %q: %w", pathName, err)
		}
		externalPath, err := filepath.Abs(filepath.Join(filepath.Dir(canonicalInput), filepath.FromSlash(refPath)))
		if err != nil {
			return fmt.Errorf("path %q: resolve %q: %w", pathName, refPath, err)
		}
		external, err := readYAML(externalPath)
		if err != nil {
			return fmt.Errorf("path %q: %w", pathName, err)
		}
		selected, err := resolveJSONPointer(external.Content[0], fragment)
		if err != nil {
			return fmt.Errorf("path %q ref %q: %w", pathName, ref, err)
		}
		inlined := cloneNode(selected)
		if err := rewriteBackreferences(inlined, filepath.Dir(externalPath), canonicalInput, root); err != nil {
			return fmt.Errorf("path %q ref %q: %w", pathName, ref, err)
		}
		paths.Content[i+1] = inlined
	}
	return nil
}

func scalarNodeValue(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func splitExternalRef(ref string) (path, fragment string, err error) {
	if strings.Contains(ref, "://") {
		return "", "", fmt.Errorf("unsupported remote reference %q", ref)
	}
	path, fragment, ok := strings.Cut(ref, "#")
	if !ok || path == "" || fragment == "" {
		return "", "", fmt.Errorf("external reference must contain a file and JSON pointer: %q", ref)
	}
	return path, fragment, nil
}

func resolveJSONPointer(root *yaml.Node, fragment string) (*yaml.Node, error) {
	if fragment == "" {
		return root, nil
	}
	if !strings.HasPrefix(fragment, "/") {
		return nil, fmt.Errorf("fragment %q is not a JSON pointer", fragment)
	}
	current := root
	for _, raw := range strings.Split(fragment[1:], "/") {
		token, err := unescapeJSONPointerToken(raw)
		if err != nil {
			return nil, err
		}
		switch current.Kind {
		case yaml.MappingNode:
			current = mapValue(current, token)
			if current == nil {
				return nil, fmt.Errorf("JSON pointer %q: key %q not found", fragment, token)
			}
		case yaml.SequenceNode:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(current.Content) {
				return nil, fmt.Errorf("JSON pointer %q: invalid index %q", fragment, token)
			}
			current = current.Content[index]
		default:
			return nil, fmt.Errorf("JSON pointer %q traverses a scalar at %q", fragment, token)
		}
	}
	return current, nil
}

func unescapeJSONPointerToken(token string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(token); i++ {
		if token[i] != '~' {
			out.WriteByte(token[i])
			continue
		}
		if i+1 >= len(token) || (token[i+1] != '0' && token[i+1] != '1') {
			return "", fmt.Errorf("invalid JSON pointer escape in %q", token)
		}
		i++
		if token[i] == '0' {
			out.WriteByte('~')
		} else {
			out.WriteByte('/')
		}
	}
	return out.String(), nil
}

func rewriteBackreferences(n *yaml.Node, sourceDir, canonicalInput string, root *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.MappingNode {
		if refNode := mapValue(n, "$ref"); refNode != nil {
			if refNode.Kind != yaml.ScalarNode {
				return fmt.Errorf("$ref must be a string")
			}
			refPath, fragment, err := splitExternalRef(refNode.Value)
			if err != nil {
				return err
			}
			resolved, err := filepath.Abs(filepath.Join(sourceDir, filepath.FromSlash(refPath)))
			if err != nil {
				return fmt.Errorf("resolve backreference %q: %w", refNode.Value, err)
			}
			if filepath.Clean(resolved) != filepath.Clean(canonicalInput) {
				return fmt.Errorf("unsupported external reference %q inside path item", refNode.Value)
			}
			if _, err := resolveJSONPointer(root, fragment); err != nil {
				return fmt.Errorf("backreference %q: %w", refNode.Value, err)
			}
			refNode.Value = "#" + fragment
		}
	}
	for _, child := range n.Content {
		if err := rewriteBackreferences(child, sourceDir, canonicalInput, root); err != nil {
			return err
		}
	}
	return nil
}

func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	clone := *n
	clone.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		clone.Content[i] = cloneNode(child)
	}
	clone.Alias = cloneNode(n.Alias)
	return &clone
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "oapi-validate-tags: "+format+"\n", args...)
	os.Exit(1)
}

// walk descends into the YAML tree looking for object schemas (nodes
// that have a `properties:` mapping) and annotates each property.
func walk(n *yaml.Node) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.MappingNode:
		annotateSchemaProperties(n)
		for i := 1; i < len(n.Content); i += 2 {
			walk(n.Content[i])
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			walk(c)
		}
	}
}

// annotateSchemaProperties detects an object schema (a mapping with a
// `properties:` key) and injects extra-tags annotations into each
// property's child mapping.
func annotateSchemaProperties(schema *yaml.Node) {
	propsNode := mapValue(schema, "properties")
	if propsNode == nil || propsNode.Kind != yaml.MappingNode {
		return
	}

	required := requiredSet(schema)

	for i := 0; i+1 < len(propsNode.Content); i += 2 {
		nameNode := propsNode.Content[i]
		propNode := propsNode.Content[i+1]
		if propNode.Kind != yaml.MappingNode {
			continue
		}
		isReq := required[nameNode.Value]
		tag := buildValidateTag(propNode, isReq)
		if tag == "" {
			continue
		}
		setExtraValidateTag(propNode, tag)
	}
}

// mapValue returns the value node for `key` in a mapping, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// requiredSet returns the names listed under the schema's `required:`
// sequence, as a set.
func requiredSet(schema *yaml.Node) map[string]bool {
	out := map[string]bool{}
	req := mapValue(schema, "required")
	if req == nil || req.Kind != yaml.SequenceNode {
		return out
	}
	for _, n := range req.Content {
		if n.Kind == yaml.ScalarNode && n.Value != "" {
			out[n.Value] = true
		}
	}
	return out
}

// buildValidateTag derives the value for `validate:"..."` from the
// JSON Schema constraints on `prop`. Returns empty string when there
// are no constraints worth emitting (no `required`, no format, no
// numeric or string bounds, no enum, no pattern).
//
// A `$ref` property is opaque at this level — the referenced type
// emits its own struct with its own field tags — so we only emit
// `required` (or nothing) and skip format/enum/etc.
func buildValidateTag(prop *yaml.Node, isRequired bool) string {
	if prop == nil {
		return ""
	}

	isRef := mapValue(prop, "$ref") != nil
	typeNode := mapValue(prop, "type")
	formatNode := mapValue(prop, "format")
	minLenNode := mapValue(prop, "minLength")
	maxLenNode := mapValue(prop, "maxLength")
	patternNode := mapValue(prop, "pattern")
	minimumNode := mapValue(prop, "minimum")
	maximumNode := mapValue(prop, "maximum")
	enumNode := mapValue(prop, "enum")
	nullableNode := mapValue(prop, "nullable")

	var typ string
	if typeNode != nil {
		typ = typeNode.Value
	}
	nullable := nullableNode != nil && nullableNode.Value == "true"

	var rules []string

	if isRequired && !nullable {
		rules = append(rules, "required")
	} else {
		rules = append(rules, "omitempty")
	}

	if !isRef {
		switch typ {
		case "string":
			if formatNode != nil {
				switch formatNode.Value {
				case "email":
					rules = append(rules, "email")
				case "uuid":
					rules = append(rules, "uuid")
				case "uri", "url":
					rules = append(rules, "url")
				}
			}
			if minLenNode != nil && minLenNode.Value != "" {
				rules = append(rules, "min="+minLenNode.Value)
			}
			if maxLenNode != nil && maxLenNode.Value != "" {
				rules = append(rules, "max="+maxLenNode.Value)
			}
			if patternNode != nil && patternNode.Value != "" {
				rules = append(rules, "regexp="+patternNode.Value)
			}
		case "integer", "number":
			if minimumNode != nil && minimumNode.Value != "" {
				rules = append(rules, "min="+minimumNode.Value)
			}
			if maximumNode != nil && maximumNode.Value != "" {
				rules = append(rules, "max="+maximumNode.Value)
			}
		}
		if enumNode != nil && enumNode.Kind == yaml.SequenceNode && len(enumNode.Content) > 0 {
			if v := enumOneof(enumNode); v != "" {
				rules = append(rules, v)
			}
		}
	}

	if len(rules) == 1 && rules[0] == "omitempty" {
		return ""
	}
	if len(rules) == 0 {
		return ""
	}
	return strings.Join(rules, ",")
}

// enumOneof renders a YAML enum sequence as `oneof=a b c`. Values
// containing whitespace are skipped (validator.v10's oneof tokenizes
// on spaces and offers no way to escape them — better to silently omit
// the rule than to emit a broken tag).
func enumOneof(seq *yaml.Node) string {
	parts := make([]string, 0, len(seq.Content))
	for _, n := range seq.Content {
		if n.Kind != yaml.ScalarNode {
			return ""
		}
		if n.Value == "" {
			continue
		}
		if strings.ContainsAny(n.Value, " \t\r\n") {
			return ""
		}
		parts = append(parts, n.Value)
	}
	if len(parts) == 0 {
		return ""
	}
	return "oneof=" + strings.Join(parts, " ")
}

// setExtraValidateTag adds or merges a `validate:` entry into the
// property node's `x-oapi-codegen-extra-tags` map. Idempotent: if the
// key already exists with the same value, no change is made.
func setExtraValidateTag(prop *yaml.Node, tag string) {
	const extKey = "x-oapi-codegen-extra-tags"
	extras := mapValue(prop, extKey)
	if extras == nil {
		extras = &yaml.Node{Kind: yaml.MappingNode}
		prop.Content = append(prop.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: extKey},
			extras,
		)
	}
	for i := 0; i+1 < len(extras.Content); i += 2 {
		if extras.Content[i].Value == "validate" {
			extras.Content[i+1].Value = tag
			extras.Content[i+1].Style = yaml.DoubleQuotedStyle
			return
		}
	}
	extras.Content = append(extras.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "validate"},
		&yaml.Node{Kind: yaml.ScalarNode, Value: tag, Style: yaml.DoubleQuotedStyle},
	)
}
