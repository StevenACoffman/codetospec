package policy_test

import (
	"reflect"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/policy"
)

// TestJoin: a check calling one operation of another service takes the
// pin of the one check that operation answers there; it takes none, with
// the reason, when the service has no --join graph, the answered check is
// about a user, or it calls several operations.
func TestJoin(t *testing.T) {
	check := func(name, lines string) extract.Fact {
		return fact(extract.KindAccessCheck, "access_check.webapp."+name, at(checkFile, lines),
			map[string]string{extract.AttrName: name, extract.AttrForm: formMarked})
	}
	dep := func(id, lines, service, op string) extract.Fact {
		return fact(extract.KindDependency, id, at(checkFile, lines),
			map[string]string{extract.AttrService: service, extract.AttrOperation: op})
	}
	facts := []extract.Fact{
		check("permissions.IsStaff", "10-15"),
		dep("d.1", "11-11", "admin", "Query.isStaff"),
		check("permissions.IsCoach", "20-25"),
		dep("d.2", "21-21", "coaches", "Query.isCoach"),
		check("permissions.Both", "30-35"),
		dep(
			"d.3",
			"31-31",
			"admin",
			"Query.isStaff",
		),
		dep("d.4", "32-32", "coaches", "Query.isCoach"),
		check("permissions.Elsewhere", "40-45"),
		dep("d.5", "41-41", "users", "Query.x"),
		check("permissions.Local", "50-55"),
	}
	answering := func(op, answers string) graph.Node {
		n := guardRule("rule."+op, op, "", "")
		n.Extra[graph.ExtraAnswers] = answers
		return n
	}
	targets := []policy.JoinTarget{
		{
			Service: "admin",
			Rules:   []graph.Node{answering("Query.isStaff", "acl.IsStaff")},
			Checkers: policy.Checkers{
				"acl.IsStaff": {
					Means:     policy.MeansAttribute,
					Of:        policy.HolderCaller,
					Attribute: "staff",
				},
			},
		},
		{
			Service: "coaches",
			Rules:   []graph.Node{answering("Query.isCoach", "permissions.CoachOf")},
			Checkers: policy.Checkers{
				"permissions.CoachOf": {Means: policy.MeansCoach, SubjectArg: new(int)},
			},
		},
	}
	pins, report := policy.Join(facts, nil, targets)
	if got := pins["permissions.IsStaff"]; len(pins) != 1 || got.Attribute != "staff" ||
		got.Note != "joined from admin:Query.isStaff (acl.IsStaff)" {
		t.Errorf("pins = %+v, want IsStaff's from admin", pins)
	}
	want := []policy.Joined{
		{
			Check:  "permissions.Both",
			Via:    "admin:Query.isStaff, coaches:Query.isCoach",
			Reason: policy.JoinSeveral,
		},
		{Check: "permissions.Elsewhere", Via: "users:Query.x", Reason: policy.JoinNoTarget},
		{
			Check:  "permissions.IsCoach",
			Via:    "coaches:Query.isCoach",
			Answer: "permissions.CoachOf",
			Reason: policy.JoinSubject,
		},
		{Check: "permissions.IsStaff", Via: "admin:Query.isStaff", Answer: "acl.IsStaff"},
	}
	if !reflect.DeepEqual(report, want) {
		t.Errorf("report = %+v\nwant %+v", report, want)
	}
}
