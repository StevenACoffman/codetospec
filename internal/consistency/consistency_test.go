package consistency

import (
	"testing"

	"codetospec/internal/graph"
)

func rule(id, requirement string) graph.Node {
	return graph.Node{
		ID:   id,
		Type: "rule",
		Body: "**Exigence (EARS)** : " + requirement,
	}
}

func TestFindDuplicatesFlagsNearIdentical(t *testing.T) {
	nodes := []graph.Node{
		rule(
			"rule.root.note-gol",
			"Le système doit afficher la note indiquant que les tonnages du Gol incluent Grand Pourpier.",
		),
		rule(
			"rule.usine.note-gol",
			"Le système doit afficher une note indiquant que les tonnages du Gol incluent Grand Pourpier.",
		),
		rule(
			"rule.billing.prorata",
			"QUAND un abonné active en cours de mois, le système doit facturer au prorata des jours restants.",
		),
		{ID: "domain.x", Type: "domain", Body: "ignore me"},
	}
	pairs := FindDuplicates(nodes, DefaultThreshold)
	if len(pairs) != 1 {
		t.Fatalf("pairs = %d, want 1 (the two Gol notes)", len(pairs))
	}
	p := pairs[0]
	if p.A != "rule.root.note-gol" || p.B != "rule.usine.note-gol" {
		t.Errorf("pair = %s ↔ %s", p.A, p.B)
	}
	if p.Similarity < DefaultThreshold {
		t.Errorf("similarity = %.2f, want ≥ %.2f", p.Similarity, DefaultThreshold)
	}
}

func TestFindDuplicatesIgnoresDistinctRules(t *testing.T) {
	nodes := []graph.Node{
		rule(
			"rule.a.x",
			"Le système doit calculer la TVA à vingt pour cent sur le montant hors taxe.",
		),
		rule(
			"rule.b.y",
			"QUAND un utilisateur se connecte, le système doit vérifier son mot de passe.",
		),
	}
	if pairs := FindDuplicates(nodes, DefaultThreshold); len(pairs) != 0 {
		t.Fatalf("distinct rules should not pair, got %+v", pairs)
	}
}

func TestFindDuplicatesDeterministicOrder(t *testing.T) {
	nodes := []graph.Node{
		rule(
			"rule.z.b",
			"Le système doit afficher le cumul de la semaine pour Bois Rouge et Le Gol.",
		),
		rule(
			"rule.a.a",
			"Le système doit afficher le cumul de la semaine pour Bois Rouge et Le Gol.",
		),
	}
	p1 := FindDuplicates(nodes, DefaultThreshold)
	p2 := FindDuplicates(nodes, DefaultThreshold)
	if len(p1) != 1 || len(p2) != 1 || p1[0] != p2[0] {
		t.Fatalf("non-deterministic or wrong: %+v vs %+v", p1, p2)
	}
	// A comes before B lexically.
	if p1[0].A != "rule.a.a" || p1[0].B != "rule.z.b" {
		t.Errorf("pair not ordered by id: %+v", p1[0])
	}
}

func TestFindDuplicatesNegationAware(t *testing.T) {
	cases := map[string]struct {
		a, b string
		want int
	}{
		"opposite french conditions are not duplicates": {
			"SI un utilisateur n'a pas de date de naissance et est marqué sous l'âge limite, ALORS le systeme doit le traiter comme mineur.",
			"SI un utilisateur n'a pas de date de naissance et n'est pas marqué sous l'âge limite, ALORS le systeme doit le traiter comme mineur.",
			0,
		},
		"opposite english conditions are not duplicates": {
			"IF a user has not set a birthdate and is marked as potentially under the age gate, THEN the system shall determine that the user is under the age gate.",
			"IF a user has not set a birthdate and is not marked as potentially under the age gate, THEN the system shall determine that the user is under the age gate.",
			0,
		},
		"same negation count still pairs": {
			"IF the request is not a service-admin query, THEN the system shall return an unauthorized error.",
			"IF the request isn't a service-admin query, THEN the system shall return an unauthorized error.",
			1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pairs := FindDuplicates(
				[]graph.Node{rule("rule.d.a", tc.a), rule("rule.d.b", tc.b)},
				0.7,
			)
			if len(pairs) != tc.want {
				t.Fatalf("got %d pairs %v, want %d", len(pairs), pairs, tc.want)
			}
		})
	}
}

func TestFindDuplicatesReadsEnglishRules(t *testing.T) {
	en := func(id, requirement string) graph.Node {
		return graph.Node{
			ID:   id,
			Type: graph.TypeRule,
			Body: "**Requirement (EARS)**: " + requirement,
		}
	}
	pairs := FindDuplicates([]graph.Node{
		en("rule.d.a", "WHEN an order is paid, the system shall send a receipt."),
		en("rule.d.b", "WHEN an invoice is overdue, the system shall lock the account."),
	}, 0.5)
	if len(pairs) != 0 {
		t.Fatalf("distinct English rules paired through their shared label: %v", pairs)
	}
}

func TestFindDuplicatesSkipsTwoExtractorRules(t *testing.T) {
	mechanical := func(id, op string) graph.Node {
		n := rule(
			id,
			"IF the request is not a service-admin query, THEN the system shall reject "+op+".",
		)
		n.Extra = map[string]string{graph.ExtraExtractedBy: "webapp"}
		return n
	}
	llm := rule(
		"rule.d.llm",
		"IF the request is not a service-admin query, THEN the system shall reject Mutation.a.",
	)
	cases := map[string]struct {
		nodes []graph.Node
		want  int
	}{
		"two extractor rules": {
			[]graph.Node{
				mechanical("rule.d.a", "Mutation.a"),
				mechanical("rule.d.b", "Mutation.b"),
			},
			0,
		},
		"extractor rule and llm": {[]graph.Node{mechanical("rule.d.a", "Mutation.a"), llm}, 1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := FindDuplicates(tc.nodes, DefaultThreshold); len(got) != tc.want {
				t.Fatalf("got %d pairs %v, want %d", len(got), got, tc.want)
			}
		})
	}
}
