package anarch_test

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/anarch"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	models = "example.com/svc/models"
	emails = "example.com/svc/emails"
	jobs   = "example.com/svc/jobs"
	status = "models.KADEmailRun.Status"
	// sendFile writes the state from the emails package.
	sendFile = "emails/send.go"
)

func pkg(path, file string) extract.Fact {
	return extract.Fact{
		Kind: extract.KindPackage, ID: "package." + path,
		Attrs: map[string]string{extract.AttrName: path}, Source: extract.Ref{Path: file},
	}
}

// write is a writes fact of a field of a models struct.
func write(file, lines, field string) extract.Fact {
	return extract.Fact{
		Kind: extract.KindWrites, ID: "writes." + file + ":" + lines + "#" + field,
		Attrs:  map[string]string{extract.AttrField: field, extract.AttrFieldPackage: models},
		Source: extract.Ref{Path: file, Lines: lines},
	}
}

// svcFacts is a service storing two kinds, one under another Go type's
// name, and writing their state from two packages.
func svcFacts() []extract.Fact {
	run := extract.Fact{
		Kind: extract.KindTable,
		ID:   "table.webapp.KADEmailRun",
		Attrs: map[string]string{
			extract.AttrName: "KADEmailRun",
		},
		Source: extract.Ref{Path: "models/run.go"},
	}
	coach := extract.Fact{
		Kind: extract.KindTable,
		ID:   "table.webapp.CoachRequest",
		Attrs: map[string]string{
			extract.AttrName:     "CoachRequest",
			extract.AttrStoredBy: "TeacherRequest",
		},
		Source: extract.Ref{Path: "models/request.go"},
	}
	return []extract.Fact{
		pkg(models, "models/run.go"), pkg(emails, sendFile), pkg(jobs, "jobs/run.go"),
		run, coach,
		write("jobs/run.go", "30-40", status),
		write(sendFile, "10-20", status),
		write(sendFile, "50-60", status),
		write(sendFile, "10-20", "models.TeacherRequest.State"),
		write(sendFile, "10-20", "models.Params.Kind"),              // not stored
		write("unknown/x.go", "1-2", "models.TeacherRequest.State"), // no package
	}
}

func svcNodes() []graph.Node {
	return []graph.Node{
		{
			ID: "rule.emails.send", Type: graph.TypeRule,
			Sources: []extract.Ref{{Path: sendFile, Lines: "12-14"}},
		},
		{
			ID: "rule.jobs.retry", Type: graph.TypeRule,
			Sources: []extract.Ref{{Path: "jobs/run.go", Lines: "35-35"}},
		},
		{ID: "rule.models.run", Type: graph.TypeRule, // cites no writing code
			Sources: []extract.Ref{{Path: "models/run.go", Lines: "1-5"}}},
		{ID: "entity.kademailrun", Type: graph.TypeEntity},
	}
}

// One writers rule per written enum field of a stored struct, from the
// packages writing it today, with the writes and the rules touching the
// writing code as evidence.
func TestPropose(t *testing.T) {
	t.Parallel()
	f := anarch.Propose(svcFacts(), svcNodes(), nil)
	if f.Schema != "anarch-lint.proposals.v1" || f.Source != "codetospec" {
		t.Errorf("header = %q %q", f.Schema, f.Source)
	}
	var names []string
	for _, p := range f.Proposals {
		names = append(names, p.Fitness.Fields[0].Name)
	}
	want := []string{status + " writers", "models.TeacherRequest.State writers"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("rules = %v, want %v", names, want)
	}

	run := f.Proposals[0]
	rule := run.Fitness.Fields[0]
	wantRule := anarch.FieldRule{
		Name: status + " writers", Optional: true, QualityAttribute: "maintainability",
		Fields: []string{models + ".KADEmailRun.Status"}, Writes: true, Only: true,
		From: []string{emails, jobs},
		Reason: "Draft, from the code: only these packages write " + status +
			" today (3 write(s); 2 rule(s) of the spec cite the writing code).",
	}
	if !reflect.DeepEqual(rule, wantRule) {
		t.Errorf("rule =\n%+v\nwant\n%+v", rule, wantRule)
	}
	wantEvidence := []anarch.Evidence{
		{Kind: anarch.EvidenceWrite, Path: sendFile, Lines: "10-20"},
		{Kind: anarch.EvidenceWrite, Path: sendFile, Lines: "50-60"},
		{Kind: anarch.EvidenceWrite, Path: "jobs/run.go", Lines: "30-40"},
		{Kind: anarch.EvidenceRule, ID: "rule.emails.send"},
		{Kind: anarch.EvidenceRule, ID: "rule.jobs.retry"},
	}
	if !reflect.DeepEqual(run.Evidence, wantEvidence) {
		t.Errorf("evidence =\n%+v\nwant\n%+v", run.Evidence, wantEvidence)
	}
	if len(run.ID) != 16 {
		t.Errorf("id = %q, want 16 hex digits", run.ID)
	}
	if again := anarch.Propose(svcFacts(), svcNodes(), nil); again.Proposals[0].ID != run.ID {
		t.Error("the same writers should give the same id")
	}
	if got := f.Proposals[1].Fitness.Fields[0].From; !reflect.DeepEqual(got, []string{emails}) {
		t.Errorf("stored_by kind: from = %v, want only the resolved writer", got)
	}
}

// A rule the model declares, accepted or rejected, is not proposed again.
func TestProposeSkipsDeclared(t *testing.T) {
	t.Parallel()
	model := []byte(`{"version": 1, "fitness": {"requireAssigned": true,
		"fields": [{"name": "` + status + ` writers", "gate": "off"}],
		"onion": {"name": "rings"}, "calls": [{"callees": ["f"]}]}}`)
	declared, err := anarch.Declared(model)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]bool{
		status + " writers": true,
		"rings":             true,
	}; !reflect.DeepEqual(
		declared,
		want,
	) {
		t.Errorf("declared = %v, want %v", declared, want)
	}
	f := anarch.Propose(svcFacts(), svcNodes(), declared)
	if len(f.Proposals) != 1 || f.Proposals[0].Fitness.Fields[0].Name == status+" writers" {
		t.Errorf("proposals = %+v, want only the undeclared one", f.Proposals)
	}
	if _, err = anarch.Declared([]byte("{")); err == nil {
		t.Error("a malformed model should be an error")
	}
}

// The file is what anarch-lint's proposals schema accepts, even empty.
func TestMarshalMatchesSchema(t *testing.T) {
	t.Parallel()
	schema := compileSchema(t)
	for name, f := range map[string]anarch.File{
		"proposals": anarch.Propose(svcFacts(), svcNodes(), nil),
		"none":      anarch.Propose(nil, nil, nil),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(anarch.Marshal(&f)))
			if err != nil {
				t.Fatal(err)
			}
			if err = schema.Validate(doc); err != nil {
				t.Errorf("the schema rejects the file: %v", err)
			}
		})
	}
}

// compileSchema loads the copy of anarch-lint's proposals schema.
func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	const path = "testdata/proposals.schema.json"
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err = c.AddResource(path, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
