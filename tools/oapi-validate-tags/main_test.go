package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// parseProp parses a one-property mapping out of a snippet so tests can
// exercise buildValidateTag without hand-constructing yaml.Node trees.
func parseProp(t *testing.T, snippet string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(snippet), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		t.Fatalf("unexpected doc shape: %d", len(doc.Content))
	}
	return doc.Content[0]
}

func TestBuildValidateTag(t *testing.T) {
	cases := []struct {
		name     string
		snippet  string
		required bool
		want     string
	}{
		{
			name:     "required_string_email",
			snippet:  "type: string\nformat: email\n",
			required: true,
			want:     "required,email",
		},
		{
			name:     "optional_string_email",
			snippet:  "type: string\nformat: email\n",
			required: false,
			want:     "omitempty,email",
		},
		{
			name:     "required_uuid",
			snippet:  "type: string\nformat: uuid\n",
			required: true,
			want:     "required,uuid",
		},
		{
			name:     "uri_optional",
			snippet:  "type: string\nformat: uri\n",
			required: false,
			want:     "omitempty,url",
		},
		{
			name:     "minlen_required",
			snippet:  "type: string\nminLength: 8\n",
			required: true,
			want:     "required,min=8",
		},
		{
			name:     "pattern",
			snippet:  "type: string\npattern: '^\\d{4}-\\d{2}-\\d{2}$'\n",
			required: true,
			want:     "required,regexp=^\\d{4}-\\d{2}-\\d{2}$",
		},
		{
			name:     "numeric_bounds",
			snippet:  "type: integer\nminimum: 1\nmaximum: 100\n",
			required: true,
			want:     "required,min=1,max=100",
		},
		{
			name:     "enum_scalar",
			snippet:  "type: string\nenum: [ru, en]\n",
			required: true,
			want:     "required,oneof=ru en",
		},
		{
			name:     "enum_with_empty_string_drops_empty",
			snippet:  "type: string\nenum: [inherit, all, '']\n",
			required: false,
			want:     "omitempty,oneof=inherit all",
		},
		{
			name:     "enum_with_whitespace_value_skips_rule",
			snippet:  "type: string\nenum: [\"a b\", c]\n",
			required: true,
			want:     "required",
		},
		{
			name:     "ref_required_only",
			snippet:  "$ref: '#/components/schemas/Foo'\n",
			required: true,
			want:     "required",
		},
		{
			name:     "ref_optional_returns_empty",
			snippet:  "$ref: '#/components/schemas/Foo'\n",
			required: false,
			want:     "",
		},
		{
			name:     "plain_string_no_constraints_optional",
			snippet:  "type: string\n",
			required: false,
			want:     "",
		},
		{
			name:     "nullable_required_skips_required",
			snippet:  "type: string\nnullable: true\nformat: email\n",
			required: true,
			want:     "omitempty,email",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := parseProp(t, tc.snippet)
			got := buildValidateTag(n, tc.required)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSetExtraValidateTag_AddsNewMap(t *testing.T) {
	n := parseProp(t, "type: string\nformat: email\n")
	setExtraValidateTag(n, "required,email")
	out, err := yaml.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "x-oapi-codegen-extra-tags:") {
		t.Fatalf("expected extras key, got:\n%s", out)
	}
	if !strings.Contains(string(out), `validate: "required,email"`) {
		t.Fatalf("expected validate value, got:\n%s", out)
	}
}

func TestSetExtraValidateTag_MergesExistingMap(t *testing.T) {
	n := parseProp(t, "type: string\nx-oapi-codegen-extra-tags:\n  custom: \"x\"\n")
	setExtraValidateTag(n, "required")
	out, err := yaml.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `custom: "x"`) {
		t.Fatalf("expected to preserve custom tag, got:\n%s", s)
	}
	if !strings.Contains(s, `validate: "required"`) {
		t.Fatalf("expected validate added, got:\n%s", s)
	}
}

func TestSetExtraValidateTag_OverwritesExistingValidate(t *testing.T) {
	n := parseProp(t, "type: string\nx-oapi-codegen-extra-tags:\n  validate: \"old\"\n")
	setExtraValidateTag(n, "new")
	out, _ := yaml.Marshal(n)
	if strings.Contains(string(out), `"old"`) {
		t.Fatalf("expected old value gone, got:\n%s", out)
	}
	if !strings.Contains(string(out), `validate: "new"`) {
		t.Fatalf("expected new value, got:\n%s", out)
	}
}

func TestWalk_AnnotatesNestedSchemas(t *testing.T) {
	src := `
components:
  schemas:
    Outer:
      type: object
      required: [id]
      properties:
        id:
          type: string
          format: uuid
        nested:
          type: object
          required: [name]
          properties:
            name:
              type: string
              minLength: 3
`
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	walk(doc.Content[0])
	out, _ := yaml.Marshal(&doc)
	s := string(out)
	if !strings.Contains(s, `validate: "required,uuid"`) {
		t.Fatalf("expected outer id tag, got:\n%s", s)
	}
	if !strings.Contains(s, `validate: "required,min=3"`) {
		t.Fatalf("expected nested name tag, got:\n%s", s)
	}
}
