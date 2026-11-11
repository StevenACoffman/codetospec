package locale

// Alignment is the text of the output README's comparison of the
// confirmed models with the code: the questions that settle the concept
// conflicts, and the "Concepts and code" section.
type Alignment struct {
	// ConceptsUnreviewedFmt warns that the models given are a proposal
	// (%d entities still defined by codetospec, %d entities in all).
	ConceptsUnreviewedFmt               string
	ConceptSetsLeadFmt                  string // %d questions, %d conflicts
	ConceptSetFmt                       string // %d conflicts a question settles
	AlignmentHeading, AlignmentIntroFmt string // %d converge, diverge, absent, unobservable
	AlignmentDivergentLead              string // before the divergent pairs
	AlignmentAbsentLead                 string // before the absent pairs
	AlignmentRecoveryFmt                string // %s index, %d files, concepts, communities
	AlignmentNoRecoveryFmt              string // %d files
	AlignmentScatteringColumns          string // the table's header rows
	AlignmentTanglingColumns            string // the table's header rows
}

func newAlignment(tr translate) Alignment {
	return Alignment{
		ConceptsUnreviewedFmt: tr(
			"**Modèles non relus** : %d des %d entités des modèles donnés gardent une définition écrite par codetospec (un emplacement à remplir, ou un brouillon tiré de la documentation du code). Lisez ces concepts comme une proposition : les règles bougent comme un modèle confirmé les ferait bouger, et les relations tirées des clés étrangères concordent par construction avec ces mêmes clés.",
			"**Unreviewed models**: %d of the %d entities of the models given keep a definition codetospec wrote (a placeholder, or a draft from the code's documentation). Read these concepts as a proposal: the rules move as a confirmed model would move them, and relationships drawn from foreign keys converge with those same keys by construction.",
		),
		ConceptSetsLeadFmt: tr(
			"Ces %d questions, posées dans l'ordre, règlent les %d conflits : chacune demande si ses concepts ont un propriétaire commun, ce qui règle aussi tout conflict dont elle contient les concepts.",
			"These %d questions, asked in order, settle the %d conflicts: each asks whether its concepts share an owner, which also settles every conflict whose concepts it contains.",
		),
		ConceptSetFmt: tr(
			"conflits réglés : %d",
			"conflicts settled: %d",
		),
		AlignmentHeading: tr(
			"## Concepts et code",
			"## Concepts and code",
		),
		AlignmentIntroFmt: tr(
			"Les relations des modèles confirmés face à ce que le code montre seul : clés étrangères entre les tables des concepts, appels directs du code que citent les règles d'un concept vers celui que citent les règles d'un autre. Une description, pas une note de qualité. Paires : %d concordent (déclarées et montrées), %d divergent (montrées, non déclarées : `concept_divergence`, une question chacune), %d absentes (déclarées, non montrées), %d inobservables (déclarées, un concept sans règle placée ni table : rien ne pouvait les montrer).",
			"The confirmed models' relationships against what the code alone shows: foreign keys between the concepts' tables, direct calls from code one concept's rules cite into code another's cite. A description, not a quality score. Pairs: %d converge (declared and shown), %d diverge (shown, not declared: `concept_divergence`, a question each), %d absent (declared, not shown), %d unobservable (declared, one concept with neither placed rules nor a table: nothing could show them).",
		),
		AlignmentDivergentLead: tr(
			"Paires divergentes :",
			"Divergent pairs:",
		),
		AlignmentAbsentLead: tr(
			"Paires absentes :",
			"Absent pairs:",
		),
		AlignmentRecoveryFmt: tr(
			"Les communautés du graphe des imports retrouvent les concepts avec un indice de Rand ajusté de %s (1 : la même partition, 0 : le hasard) sur %d fichiers (%d concepts, %d communautés ; le concept d'un fichier est celui de la plupart de ses règles placées).",
			"The import graph's communities recover the concepts with an adjusted Rand index of %s (1: the same partition, 0: chance) over %d files (%d concepts, %d communities; a file's concept is that of most of its placed rules).",
		),
		AlignmentNoRecoveryFmt: tr(
			"L'indice de Rand ajusté des communautés face aux concepts n'est pas défini sur %d fichiers (trop peu, ou un seul groupe de chaque côté).",
			"The adjusted Rand index of the communities against the concepts is undefined over %d files (too few, or one group on each side).",
		),
		AlignmentScatteringColumns: tr(
			"| concept | règles | répertoires | le plus dans |\n|---|---:|---:|---|",
			"| concept | rules | directories | most in |\n|---|---:|---:|---|",
		),
		AlignmentTanglingColumns: tr(
			"| répertoire | règles | concepts | le plus pour |\n|---|---:|---:|---|",
			"| directory | rules | concepts | most for |\n|---|---:|---:|---|",
		),
	}
}
