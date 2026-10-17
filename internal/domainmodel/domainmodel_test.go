package domainmodel_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/domainmodel"
	"github.com/StevenACoffman/codetospec/internal/graph"
)

// Names the fixtures share.
const (
	ticketEntity = "garage.Ticket"
	visitEntity  = "garage.Visit"
	visitID      = "entity.visit"
)

func entity(id, title string) graph.Node {
	return graph.Node{ID: id, Type: graph.TypeEntity, Title: title}
}

func rule(id, requirement string, touches ...string) graph.Node {
	n := graph.Node{ID: id, Type: graph.TypeRule, Body: "**Requirement (EARS)**: " + requirement}
	for _, e := range touches {
		n.Edges = append(n.Edges, graph.Edge{Type: graph.EdgeTouches, To: e})
	}
	return n
}

// load reads testdata/garage.modelith.yaml: entities Visit (two
// invariants), ParkingSpot and Ticket, and one model-level invariant.
func load(t *testing.T) *domainmodel.Set {
	t.Helper()
	set, err := domainmodel.Load([]string{"testdata/garage.modelith.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// garageGraph: Visit and Ticket exist in code (Ticket under another name),
// ParkingSpot does not; Vehicle is in code but not in the model.
func garageGraph() []graph.Node {
	return []graph.Node{
		entity(visitID, "Visit"),
		entity("entity.paperticket", "PaperTicket"),
		entity("entity.vehicle", "Vehicle"),
		rule("rule.visits.exit-after-entry",
			"IF the exit time of a visit is before its entry time, "+
				"THEN the system shall reject the exit.",
			visitID),
		rule("rule.visits.audit-log",
			"WHEN a visit closes, the system shall append an audit log record.",
			visitID),
		rule("rule.tickets.paid-exit",
			"IF a ticket is not paid, THEN the system shall keep the exit barrier closed.",
			"entity.paperticket"),
	}
}

func TestLoad(t *testing.T) {
	cases := map[string]struct {
		path      string
		wantErr   string
		wantScope string
	}{
		"valid model, scope from its file name": {
			path: "testdata/garage.modelith.yaml", wantScope: "garage",
		},
		"lint error fails the load": {
			path: "testdata/invalid.modelith.yaml", wantErr: "lint error",
		},
		"missing file": {
			path: "testdata/absent.modelith.yaml", wantErr: "read domain model",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			set, err := domainmodel.Load([]string{tc.path})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := set.Models[0].Scope; got != tc.wantScope {
				t.Errorf("scope = %q, want %q", got, tc.wantScope)
			}
			if set.Warnings == 0 {
				t.Error("no warnings: the fixture has entities no scenario exercises")
			}
		})
	}
}

// TestCompareEntities: what each model entity is matched to, and which of
// its rules no invariant states.
func TestCompareEntities(t *testing.T) {
	report := domainmodel.Compare(load(t), garageGraph(), domainmodel.Options{
		Entities: map[string]string{ticketEntity: "PaperTicket"},
	})
	got := map[string]domainmodel.Entity{}
	for _, e := range report.Entities {
		got[e.Name] = e
	}
	cases := map[string]struct {
		entity       string
		wantCode     string
		wantUnstated []string
	}{
		"same name": {
			entity: visitEntity, wantCode: visitID,
			wantUnstated: []string{"rule.visits.audit-log"},
		},
		"mapped name, rule stated by a model-level invariant": {
			entity: ticketEntity, wantCode: "entity.paperticket",
		},
		"absent from the code": {
			entity: "garage.ParkingSpot",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e, ok := got[tc.entity]
			if !ok {
				t.Fatalf("no entity %s in %v", tc.entity, report.Entities)
			}
			if e.Code.ID != tc.wantCode {
				t.Errorf("code = %q, want %q", e.Code.ID, tc.wantCode)
			}
			if !reflect.DeepEqual(e.Unstated, tc.wantUnstated) {
				t.Errorf("unstated = %v, want %v", e.Unstated, tc.wantUnstated)
			}
		})
	}
}

// TestCompareInvariants: each invariant is paired with the closest rule
// touching its entity, or with none below the threshold.
func TestCompareInvariants(t *testing.T) {
	report := domainmodel.Compare(load(t), garageGraph(), domainmodel.Options{
		Entities: map[string]string{ticketEntity: "PaperTicket"},
	})
	got := map[string]domainmodel.Invariant{}
	for _, e := range report.Entities {
		for _, inv := range e.Invariants {
			got[inv.ID] = inv
		}
	}
	for _, inv := range report.Invariants {
		got[inv.ID] = inv
	}
	cases := map[string]struct {
		invariant string
		wantRule  string
	}{
		"stated by a rule of its entity": {
			invariant: "exit-after-entry", wantRule: "rule.visits.exit-after-entry",
		},
		"stated by no rule": {
			invariant: "one-open-visit",
		},
		"model-level, compared with every rule": {
			invariant: "payment-before-exit", wantRule: "rule.tickets.paid-exit",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			inv, ok := got[tc.invariant]
			if !ok {
				t.Fatalf("no invariant %s", tc.invariant)
			}
			if inv.Rule != tc.wantRule {
				t.Errorf("rule = %q, want %q (closest %q, %.2f)",
					inv.Rule, tc.wantRule, inv.Closest, inv.Similarity)
			}
		})
	}
}

// TestCompareCodeEntities covers matching that depends on several code
// entities at once: plurals, ambiguity, unmodeled entities and mappings
// that match nothing.
func TestCompareCodeEntities(t *testing.T) {
	cases := map[string]struct {
		nodes         []graph.Node
		mapping       map[string]string
		want          map[string]string // model entity → matched code entity id
		wantAmbiguous string            // model entity with several candidates
		wantUnmodeled []domainmodel.CodeEntity
		wantStale     []string
	}{
		"plural table names, exact name first": {
			nodes: []graph.Node{
				entity("entity.visits", "visits"),
				entity("entity.parking-spots", "parking_spots"),
				entity("entity.ticket", "Ticket"),
				entity("entity.tickets", "tickets"),
			},
			want: map[string]string{
				visitEntity:          "entity.visits",
				"garage.ParkingSpot": "entity.parking-spots",
				ticketEntity:         "entity.ticket",
			},
			wantUnmodeled: []domainmodel.CodeEntity{{ID: "entity.tickets", Title: "tickets"}},
		},
		"two code entities with one normalized name": {
			nodes: []graph.Node{
				entity(visitID, "Visit"),
				entity("entity.visit-2", "visit"),
			},
			want:          map[string]string{},
			wantAmbiguous: visitEntity,
			wantUnmodeled: []domainmodel.CodeEntity{
				{ID: visitID, Title: "Visit"},
				{ID: "entity.visit-2", Title: "visit"},
			},
		},
		"mappings matching nothing": {
			nodes:     []graph.Node{entity(visitID, "Visit")},
			mapping:   map[string]string{"Barrier": "Gate", ticketEntity: "Stub"},
			want:      map[string]string{visitEntity: visitID},
			wantStale: []string{"Barrier → Gate", "garage.Ticket → Stub"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			report := domainmodel.Compare(load(t), tc.nodes, domainmodel.Options{
				Entities: tc.mapping,
			})
			got, ambiguous := matches(&report)
			if want := []string{tc.wantAmbiguous}; tc.wantAmbiguous == "" && len(ambiguous) > 0 ||
				tc.wantAmbiguous != "" && !reflect.DeepEqual(ambiguous, want) {
				t.Errorf("ambiguous = %v, want %q", ambiguous, tc.wantAmbiguous)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("matched = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(report.Unmodeled, tc.wantUnmodeled) {
				t.Errorf("unmodeled = %v, want %v", report.Unmodeled, tc.wantUnmodeled)
			}
			if !reflect.DeepEqual(report.StaleMappings, tc.wantStale) {
				t.Errorf("stale = %v, want %v", report.StaleMappings, tc.wantStale)
			}
		})
	}
}

// matches summarizes a report: each matched model entity's code entity id,
// and the model entities with several candidates.
func matches(report *domainmodel.Report) (map[string]string, []string) {
	matched := map[string]string{}
	var ambiguous []string
	for _, e := range report.Entities {
		if e.Code.ID != "" {
			matched[e.Name] = e.Code.ID
		}
		if len(e.Ambiguous) > 1 {
			ambiguous = append(ambiguous, e.Name)
		}
	}
	return matched, ambiguous
}

// TestCompareIgnoresNodeOrder: reversing the nodes leaves the report
// unchanged (sorted lists, no map order).
func TestCompareIgnoresNodeOrder(t *testing.T) {
	nodes := garageGraph()
	want := domainmodel.Compare(load(t), nodes, domainmodel.Options{})
	for i, j := 0, len(nodes)-1; i < j; i, j = i+1, j-1 {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	}
	got := domainmodel.Compare(load(t), nodes, domainmodel.Options{})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reversed nodes changed the report:\n%+v\n%+v", got, want)
	}
}
