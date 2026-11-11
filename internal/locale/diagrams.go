package locale

// Diagrams is the text of the output README's diagram sections: the
// package dependencies.
type Diagrams struct {
	// The package dependency diagram's section: heading, what it shows,
	// its alternative text, and the label of its unconnected packages.
	PackagesHeading, PackagesIntro, PackagesAlt, PackagesIsolated string
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
		PackagesIsolated: tr("Sans import entre paquets", "No imports between packages"),
		DiagramSource:    tr("Source D2", "D2 source"),
	}
}
