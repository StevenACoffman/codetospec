package locale

// Diagrams is the text of the output README's diagram sections: the
// package dependencies and the concept map.
type Diagrams struct {
	// The package dependency diagram's section: heading, what it shows,
	// its alternative text, and the label of its unconnected packages.
	PackagesHeading, PackagesIntro, PackagesAlt, PackagesIsolated string
	// The concept map's section: heading, what it shows (of confirmed
	// models, or of the proposal), its alternative text, and the words of
	// its edges and labels.
	ConceptMapHeading, ConceptMapIntro, ConceptMapProposedIntro, ConceptMapAlt string
	ConceptOwns, ConceptReferences, ConceptIsA                                 string
	ConceptProposalFmt                                                         string // %s the model's title
	ConceptRulesFmt                                                            string // %d rules placed in a concept
	// DiagramSource labels the link to a drawn diagram's D2 source.
	DiagramSource string
}

func newDiagrams(tr translate) Diagrams {
	return Diagrams{
		PackagesHeading: tr("## Dépendances des paquets", "## Package dependencies"),
		PackagesIntro: tr(
			"Les paquets du code et lesquels en importent d'autres, d'après les imports, sans rien inférer : la structure du code, pas ses domaines métier. Les paquets d'un arbre partagé, quand le code les importe, sont dessinés à part.",
			"The code's packages and which import which, from its imports, nothing inferred: the code's structure, not its business domains. Packages of a shared tree the code imports are drawn apart.",
		),
		PackagesAlt: tr(
			"Paquets du code et leurs imports",
			"The code's packages and their imports",
		),
		PackagesIsolated:  tr("Sans import entre paquets", "No imports between packages"),
		ConceptMapHeading: tr("## Carte des concepts", "## Concept map"),
		ConceptMapIntro: tr(
			"Les concepts des modèles confirmés (`--domain-model`), un cadre par contexte, et leurs relations ; chaque concept dit combien de règles y sont placées.",
			"The concepts of the confirmed models (`--domain-model`), one frame per context, and their relationships; each concept says how many rules are placed in it.",
		),
		ConceptMapProposedIntro: tr(
			"**Proposition, non confirmée** : les concepts que codetospec tire du code (tables, types d'API, propriété par clés étrangères). Corrigez-la (`--write-domain-model` l'écrit dans `models/generated/`), puis passez-la avec `--domain-model` : alors seulement elle regroupe les règles.",
			"**Proposal, not confirmed**: the concepts codetospec draws from the code (tables, API types, ownership from foreign keys). Correct it (`--write-domain-model` writes it under `models/generated/`), then pass it with `--domain-model`: only then does it regroup the rules.",
		),
		ConceptMapAlt:      tr("Concepts et leurs relations", "Concepts and their relationships"),
		ConceptOwns:        tr("possède", "owns"),
		ConceptReferences:  tr("référence", "references"),
		ConceptIsA:         tr("est un", "is a"),
		ConceptProposalFmt: tr("%s (proposition)", "%s (proposal)"),
		ConceptRulesFmt:    tr("%d règles", "%d rules"),
		DiagramSource:      tr("Source D2", "D2 source"),
	}
}
