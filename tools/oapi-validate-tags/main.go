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
// The input spec is read from argv[1]; the rewritten spec is written to
// argv[2]. The output is a derived artifact — it is regenerated each
// codegen run and should NOT be committed.
package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <input.yaml> <output.yaml>\n", os.Args[0])
		os.Exit(2)
	}
	in, out := os.Args[1], os.Args[2]

	data, err := os.ReadFile(in)
	if err != nil {
		fail("read %s: %v", in, err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		fail("parse %s: %v", in, err)
	}

	if root.Kind != yaml.DocumentNode || len(root.Content) != 1 {
		fail("expected single-document YAML, got %d documents", len(root.Content))
	}
	walk(root.Content[0])

	buf, err := yaml.Marshal(&root)
	if err != nil {
		fail("marshal: %v", err)
	}
	if err := os.WriteFile(out, buf, 0o644); err != nil {
		fail("write %s: %v", out, err)
	}
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
		// A schema is recognised by having a `properties` map. We do not
		// rely on the JSON pointer path because schemas appear under many
		// locations: components.schemas.*, requestBody.content.*.schema,
		// nested allOf entries, items, etc.
		annotateSchemaProperties(n)
		// Recurse into all values.
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

	// validator.v10 ordering convention: required/omitempty first, then
	// content rules. omitempty means "skip if zero", which matches the
	// json:",omitempty" tag oapi-codegen emits for optional fields.
	//
	// nullable: true means the field accepts JSON null. We treat
	// nullable as "optional for validation purposes" — even if the
	// spec lists the field as required, an explicit null should not
	// fail content rules like email/uuid. The handler can still
	// enforce presence-of-pointer separately if needed.
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
		// enum applies to any scalar type.
		if enumNode != nil && enumNode.Kind == yaml.SequenceNode && len(enumNode.Content) > 0 {
			if v := enumOneof(enumNode); v != "" {
				rules = append(rules, v)
			}
		}
	}

	// Drop a lone "omitempty" — there is no point telling the
	// validator to skip a zero value when there is nothing to check.
	if len(rules) == 1 && rules[0] == "omitempty" {
		return ""
	}
	// Likewise drop a lone "required" only if the field has nothing
	// further to enforce AND it is a ref. For scalar required fields
	// we keep `required` so the handler can rely on presence.
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
			return "" // non-scalar enum (object literals) — skip rule entirely.
		}
		if n.Value == "" {
			// Empty-string enum value (e.g. tri-state "inherit|all|...|''")
			// cannot be expressed in `oneof=` because validator splits on
			// whitespace with no escape. Skip the empty entry; the
			// matching field is already optional (omitempty) so the empty
			// string passes through without a tag rule.
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
	// Stable ordering — independent of YAML node ordering churn would
	// hide spec changes from CI; keep spec order so a reordered enum
	// in the spec surfaces as a generator diff.
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
	// Replace existing validate key if present.
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

