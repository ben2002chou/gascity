package formula

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGraphV2RejectsLegacyReservedReferences(t *testing.T) {
	prev := IsFormulaV2Enabled()
	SetFormulaV2Enabled(true)
	defer SetFormulaV2Enabled(prev)

	dir := t.TempDir()
	writeFormula(t, dir, "bad.formula.toml", `
formula = "bad"
version = 1
contract = "graph.v2"
type = "workflow"

[[steps]]
id = "direct"
title = "Direct {{issue}}"

[[steps]]
id = "spaced"
title = "Spaced {{ issue }}"

[[steps]]
id = "trimmed"
title = "Trimmed {{- issue -}}"

[[steps]]
id = "dotted"
title = "Dotted {{.bead_id}}"

[[steps]]
id = "indexed"
title = "Indexed {{ index . \"issue\" }}"
`)

	_, err := Compile(context.Background(), "bad", []string{dir}, map[string]string{"convoy_id": "convoy-1"})
	if err == nil {
		t.Fatal("Compile succeeded, want reserved-variable error")
	}
	msg := err.Error()
	for _, want := range []string{"issue is not available", "bead_id is not available"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestGraphV2RejectsLegacyReservedReferencesInExpansionBeforeConditionFiltering(t *testing.T) {
	prev := IsFormulaV2Enabled()
	SetFormulaV2Enabled(true)
	defer SetFormulaV2Enabled(prev)

	dir := t.TempDir()
	writeFormula(t, dir, "parent.formula.toml", `
formula = "parent"
version = 2
contract = "graph.v2"
type = "workflow"

[[steps]]
id = "work"
title = "Work"
expand = "hidden-legacy"
`)
	writeFormula(t, dir, "hidden-legacy.formula.toml", `
formula = "hidden-legacy"
version = 2
type = "expansion"

[[template]]
id = "{target}.hidden"
title = "Hidden {{bead_id}}"
condition = "!{{convoy_id}}"
`)

	_, err := Compile(context.Background(), "parent", []string{dir}, map[string]string{"convoy_id": "convoy-1"})
	if err == nil {
		t.Fatal("Compile succeeded, want transitive reserved-variable error")
	}
	if !strings.Contains(err.Error(), "bead_id is not available") {
		t.Fatalf("error = %q, want bead_id reserved-variable error", err)
	}
}

func TestGraphV2RejectsReservedVariableDeclarations(t *testing.T) {
	f := &Formula{
		Formula:  "bad-vars",
		Version:  1,
		Contract: "graph.v2",
		Type:     TypeWorkflow,
		Vars: map[string]*VarDef{
			"convoy_id": {},
			"issue":     {},
			"bead_id":   {},
		},
	}

	err := ValidateGraphV2ReservedSymbols(f, true)
	if err == nil {
		t.Fatal("ValidateGraphV2ReservedSymbols succeeded, want error")
	}
	msg := err.Error()
	for _, want := range []string{"vars.convoy_id", "vars.issue", "vars.bead_id"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestGraphV2TargetlessRejectsConvoyReferencesAndDrain(t *testing.T) {
	f := &Formula{
		Formula:     "needs-target",
		Version:     1,
		Contract:    "graph.v2",
		Type:        TypeWorkflow,
		Description: "Work on {{convoy_id}}",
		Steps: []*Step{{
			ID:    "drain",
			Title: "Drain",
			Drain: &DrainSpec{Context: "separate", Formula: "item"},
		}},
	}

	err := ValidateGraphV2ReservedSymbols(f, false)
	if err == nil {
		t.Fatal("ValidateGraphV2ReservedSymbols succeeded, want targetless error")
	}
	if !strings.Contains(err.Error(), "convoy_id requires a targeted graph.v2 invocation") {
		t.Fatalf("error = %q, want convoy target message", err)
	}
	if !GraphV2FormulaReferencesInputConvoy(f) {
		t.Fatal("GraphV2FormulaReferencesInputConvoy = false, want true")
	}
}

func TestGraphV2DrainV0AcceptsSharedAndExclusive(t *testing.T) {
	f := &Formula{
		Formula:  "shared-drain",
		Version:  1,
		Contract: "graph.v2",
		Type:     TypeWorkflow,
		Steps: []*Step{{
			ID:    "drain",
			Title: "Drain",
			Drain: &DrainSpec{
				Context:       "shared",
				Formula:       "item",
				MemberAccess:  "exclusive",
				OnItemFailure: "skip_remaining",
				Item:          &DrainItemSpec{SingleLane: true},
			},
		}},
	}

	if err := ValidateGraphV2ReservedSymbols(f, true); err != nil {
		t.Fatalf("ValidateGraphV2ReservedSymbols(shared exclusive drain): %v", err)
	}
}

func TestGraphV2DrainV0RejectsInvalidModes(t *testing.T) {
	cases := []struct {
		name string
		spec DrainSpec
		want string
	}{
		{
			name: "too many units",
			spec: DrainSpec{Context: "separate", Formula: "item", MaxUnits: 101},
			want: "max_units must be <= 100",
		},
		{
			name: "templated item formula",
			spec: DrainSpec{Context: "separate", Formula: "{{item_formula}}"},
			want: "templated item formula names are not supported in v0",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &Formula{
				Formula:  "bad-drain",
				Version:  1,
				Contract: "graph.v2",
				Type:     TypeWorkflow,
				Steps: []*Step{{
					ID:    "drain",
					Title: "Drain",
					Drain: &tc.spec,
				}},
			}

			err := f.Validate()
			if err == nil {
				t.Fatal("Validate succeeded, want drain validation error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want %q", err, tc.want)
			}
		})
	}
}

func TestParseGraphV2DrainStep(t *testing.T) {
	dir := t.TempDir()
	writeFormula(t, dir, "drain-demo.formula.toml", `
formula = "drain-demo"
version = 1
contract = "graph.v2"
type = "workflow"

[[steps]]
id = "review-members"
title = "Review members"

[steps.drain]
context = "separate"
formula = "review-one"
member_access = "read"
max_units = 50
on_item_failure = "continue"
`)

	parsed, err := NewParser(dir).LoadByName("drain-demo")
	if err != nil {
		t.Fatalf("LoadByName: %v", err)
	}
	resolved, err := NewParser(dir).Resolve(parsed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := resolved.Steps[0].Drain; got == nil || got.Context != "separate" || got.Formula != "review-one" {
		t.Fatalf("parsed drain = %#v", got)
	}
}

func TestParseFileReturnsDescriptionFileErrors(t *testing.T) {
	dir := t.TempDir()
	writeFormula(t, dir, "missing-desc.formula.toml", `
formula = "missing-desc"
version = 1
contract = "graph.v2"
type = "workflow"

[[steps]]
id = "work"
title = "Work"
description_file = "does-not-exist.md"
`)

	_, err := NewParser(dir).LoadByName("missing-desc")
	if err == nil {
		t.Fatal("LoadByName succeeded, want description_file error")
	}
	if !strings.Contains(err.Error(), "does-not-exist.md") {
		t.Fatalf("error = %q, want missing path", err)
	}
}

func TestParseFileKeepsLegacyDescriptionFileTolerance(t *testing.T) {
	dir := t.TempDir()
	writeFormula(t, dir, "missing-desc.formula.toml", `
formula = "missing-desc"
version = 1
type = "workflow"

[[steps]]
id = "work"
title = "Work"
description_file = "does-not-exist.md"
`)

	loaded, err := NewParser(dir).LoadByName("missing-desc")
	if err != nil {
		t.Fatalf("LoadByName: %v", err)
	}
	if got := loaded.Steps[0].DescriptionFile; got != "does-not-exist.md" {
		t.Fatalf("DescriptionFile = %q, want unresolved legacy value", got)
	}
}

func writeFormula(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.TrimSpace(contents)+"\n"), 0o644); err != nil {
		t.Fatalf("write formula: %v", err)
	}
}
