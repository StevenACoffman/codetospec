// Package locale holds every fixed string codetospec writes in the output
// language chosen by --lang: the EARS patterns given to the model, the labels
// of node bodies and the README/llms.txt text. It is the single place to
// translate; writers take a Text for one language, while parsers accept the
// labels of every language (All) so a graph reads back whatever --lang wrote it.
package locale

import "strings"

// Text is the fixed output text for one language. Strings ending in "Fmt"
// are fmt formats; their verbs are listed beside each field.
type Text struct {
	Lang string // --lang code: "fr" | "en"
	Name string // language name substituted into prompts ("French")

	// EARSPatterns lists one template per EARS kind, as given to the model.
	EARSPatterns string

	// DecimalSeparator is how requirements in this language write decimals:
	// ',' in French (1,5 · thousands as 1 000), '.' in English (1.5 ·
	// thousands as 1,000). Grounding reads numbers with it.
	DecimalSeparator byte

	// LLMsPreamble opens the output llms.txt (several lines).
	LLMsPreamble string

	Labels
	Readme
}

// Labels are the labels of node bodies and footers.
type Labels struct {
	// Rule bodies. Requirement and Criteria are also parsed back.
	Requirement string // label preceding the EARS requirement
	Rationale   string
	Criteria    string // label opening the numbered acceptance criteria

	// Entity, endpoint and domain bodies.
	Table, Columns          string
	Method, Path, Handler   string
	DomainTitleFmt          string // %s: domain slug
	ServiceTitleFmt         string // %s: external service slug
	OperationsCalled        string // heading over a service's called operations
	AsServiceAdminNote      string // after a call made as a service-admin query
	RulesHeading            string // markdown heading over a domain's rule list
	SourcesLabel, LinksLead string // node file footer
}

// Readme is the text of the output README.md.
type Readme struct {
	ReadmeCounts
	ReadmeCoverage
	ReadmeReview
}

// ReadmeCounts opens the README: title and node counts.
type ReadmeCounts struct {
	Title, Intro            string
	DomainsFmt, EntitiesFmt string // %d
	ServicesFmt             string // %d external services called
	EndpointsFmt, RulesFmt  string // %d
	NatureFmt               string // %d business, presentation, technical
	OriginFmt               string // %d explicit, implicit
}

// ReadmeCoverage is the README coverage section.
type ReadmeCoverage struct {
	CoverageHeading                         string
	EndpointsCoveredFmt, EntitiesCoveredFmt string // %d/%d (%s percent)
	ChunksFailedFmt, DomainsFailedFmt       string // %d/%d
	NoGrammarFmt, ExtractorsFailedFmt       string // %s: comma-separated list
	GeneratedFmt                            string // %d generated files
	CrosscheckFmt                           string // %d supported, partial, unsupported
	RepairedFmt, UncheckedFmt               string // %d, appended to CrosscheckFmt
	MechanicalFmt                           string // %d, appended to CrosscheckFmt
	RejectedRulesFmt                        string // %d malformed extractor rule facts
	GroundingFmt                            string // %d grounded, ungrounded, unchecked
}

// ReadmeReview lists rules for review and the domain graph heading.
type ReadmeReview struct {
	UngroundedHeading, UngroundedIntroFmt string // %d rules
	DuplicatesHeading, DuplicatesIntroFmt string // %d pairs, intra, cross
	SplitsHeading, SplitsIntroFmt         string // %d split domains
	SplitFmt                              string // %d files, %d communities
	AndMoreFmt                            string // %d omitted entries
	DomainsHeading                        string
}

// newText builds the Text of lang ("fr" or "en"). Each field holds its
// French and English strings side by side, so a translation cannot be
// missing for one language only.
func newText(lang string) *Text {
	tr := func(french, english string) string {
		if lang == "en" {
			return english
		}
		return french
	}
	decimalSeparator := byte(',')
	if lang == "en" {
		decimalSeparator = '.'
	}
	return &Text{
		Lang: lang,
		Name: tr("French", "English"),
		EARSPatterns: tr(
			`  ubiquitous: "Le systeme doit <response>."
  event: "QUAND <trigger>, le systeme doit <response>."
  state: "TANT QUE <state>, le systeme doit <response>."
  unwanted: "SI <condition>, ALORS le systeme doit <response>."
  optional: "LA OU <feature>, le systeme doit <response>."`,
			`  ubiquitous: "The system shall <response>."
  event: "WHEN <trigger>, the system shall <response>."
  state: "WHILE <state>, the system shall <response>."
  unwanted: "IF <condition>, THEN the system shall <response>."
  optional: "WHERE <feature>, the system shall <response>."`,
		),
		DecimalSeparator: decimalSeparator,
		LLMsPreamble: tr(
			`> Spécification des règles métier extraite automatiquement du code source.
> Chaque nœud est un fichier markdown avec un frontmatter YAML portant des edges typés.

Navigation : commencer par nodes/domains/, puis suivre les edges du frontmatter
(belongs_to, touches, exposed_by, depends_on). graph.json contient le graphe complet
au format machine ; les citations `+"`path:lines`"+` pointent dans le dépôt source.`,
			`> Business-rule specification extracted automatically from the source code.
> Each node is a markdown file with YAML frontmatter carrying typed edges.

Navigation: start at nodes/domains/, then follow the frontmatter edges
(belongs_to, touches, exposed_by, depends_on). graph.json holds the full graph
in machine form; `+"`path:lines`"+` citations point into the source repository.`,
		),
		Labels: newLabels(tr),
		Readme: newReadme(tr),
	}
}

// translate picks a language's string from a (French, English) pair.
type translate func(french, english string) string

func newLabels(tr translate) Labels {
	return Labels{
		Requirement: tr(
			"**Exigence (EARS)** : ",
			"**Requirement (EARS)**: ",
		),
		Rationale: tr(
			"**Justification** : ",
			"**Rationale**: ",
		),
		Criteria: tr(
			"**Critères d'acceptation** :",
			"**Acceptance criteria**:",
		),
		Table: tr(
			"**Table** : ",
			"**Table**: ",
		),
		Columns: tr(
			"**Colonnes** :",
			"**Columns**:",
		),
		Method: tr(
			"**Méthode** : ",
			"**Method**: ",
		),
		Path: tr(
			"**Path** : ",
			"**Path**: ",
		),
		Handler: tr(
			"**Handler** : ",
			"**Handler**: ",
		),
		ServiceTitleFmt:  tr("Service %s", "Service %s"),
		OperationsCalled: tr("## Opérations appelées", "## Operations called"),
		AsServiceAdminNote: tr(
			" (requête service-admin)",
			" (service-admin query)",
		),
		DomainTitleFmt: tr(
			"Domaine %s",
			"Domain %s",
		),
		RulesHeading: tr(
			"## Règles",
			"## Rules",
		),
		SourcesLabel: tr(
			"**Sources** : ",
			"**Sources**: ",
		),
		LinksLead: tr(
			"Liens : ",
			"Links: ",
		),
	}
}

func newReadme(tr translate) Readme {
	return Readme{
		ReadmeCounts:   newReadmeCounts(tr),
		ReadmeCoverage: newReadmeCoverage(tr),
		ReadmeReview:   newReadmeReview(tr),
	}
}

func newReadmeCounts(tr translate) ReadmeCounts {
	return ReadmeCounts{
		Title: tr(
			"# Spécification générée",
			"# Generated specification",
		),
		Intro: tr(
			"Graphe de connaissances des règles métier, extrait du code source par codetospec.",
			"Knowledge graph of business rules, extracted from the source code by codetospec.",
		),
		ServicesFmt: tr("- Services externes appelés : %d", "- External services called: %d"),
		DomainsFmt: tr(
			"- Domaines : %d",
			"- Domains: %d",
		),
		EntitiesFmt: tr(
			"- Entités : %d",
			"- Entities: %d",
		),
		EndpointsFmt: tr(
			"- Endpoints : %d",
			"- Endpoints: %d",
		),
		RulesFmt: tr(
			"- Règles : %d",
			"- Rules: %d",
		),
		NatureFmt: tr(
			"  - métier : %d · présentation : %d · technique : %d",
			"  - business: %d · presentation: %d · technical: %d",
		),
		OriginFmt: tr(
			"  - explicites : %d · implicites : %d",
			"  - explicit: %d · implicit: %d",
		),
	}
}

func newReadmeCoverage(tr translate) ReadmeCoverage {
	return ReadmeCoverage{
		CoverageHeading: tr(
			"## Couverture",
			"## Coverage",
		),
		EndpointsCoveredFmt: tr(
			"- Endpoints référencés par au moins une règle : %d/%d (%s)",
			"- Endpoints referenced by at least one rule: %d/%d (%s)",
		),
		EntitiesCoveredFmt: tr(
			"- Entités touchées par au moins une règle : %d/%d (%s)",
			"- Entities touched by at least one rule: %d/%d (%s)",
		),
		ChunksFailedFmt: tr(
			"- Chunks en échec : %d/%d",
			"- Failed chunks: %d/%d",
		),
		DomainsFailedFmt: tr(
			"- Domaines en échec : %d/%d",
			"- Failed domains: %d/%d",
		),
		NoGrammarFmt: tr(
			"- Fichiers sans grammaire (fallback lignes) : %s",
			"- Files without a grammar (line-window fallback): %s",
		),
		GeneratedFmt: tr(
			"- Fichiers générés (non analysés, marqueur « Code generated … DO NOT EDIT. ») : %d",
			"- Generated files (not mapped, \"Code generated … DO NOT EDIT.\" header): %d",
		),
		ExtractorsFailedFmt: tr(
			"- Extracteurs en échec : %s",
			"- Failed extractors: %s",
		),
		CrosscheckFmt: tr(
			"- Contre-vérification adversariale : %d supported, %d partial, %d unsupported",
			"- Adversarial crosscheck: %d supported, %d partial, %d unsupported",
		),
		RepairedFmt: tr(
			", %d réparés",
			", %d repaired",
		),
		UncheckedFmt: tr(
			", %d non vérifiées",
			", %d unchecked",
		),
		RejectedRulesFmt: tr(
			"- Règles d'extracteur rejetées (malformées, voir les logs) : %d",
			"- Extractor rules rejected (malformed, see the logs): %d",
		),
		MechanicalFmt: tr(
			", %d mécaniques (extraites du code, non soumises au LLM)",
			", %d mechanical (extracted from the code, not sent to the LLM)",
		),
		GroundingFmt: tr(
			"- Ancrage mécanique (valeurs citées présentes dans le code) : %d ancrées, %d non ancrées, %d sans valeur vérifiable",
			"- Mechanical grounding (stated values present in the cited code): %d grounded, %d ungrounded, %d with no checkable value",
		),
	}
}

func newReadmeReview(tr translate) ReadmeReview {
	return ReadmeReview{
		UngroundedHeading: tr(
			"## Règles non ancrées",
			"## Ungrounded rules",
		),
		UngroundedIntroFmt: tr(
			"%d règles énoncent des valeurs absentes des lignes citées "+"(valeur inventée ou citation décalée ; `@` indique où la valeur apparaît hors citation) :",
			"%d rules state values missing from their cited lines "+"(invented value or shifted citation; `@` shows where the value appears outside the citation):",
		),
		SplitsHeading: tr(
			"## Domaines dispersés",
			"## Scattered domains",
		),
		SplitsIntroFmt: tr(
			"%d domaines regroupent des fichiers de plusieurs communautés du graphe d'imports "+"(code sans lien entre lui : à scinder, ou startégie de domaine à revoir) :",
			"%d domains group files from several communities of the import graph "+"(code unrelated to itself: split it, or revisit the domain strategy):",
		),
		SplitFmt: tr(
			"%d fichiers dans %d communautés",
			"%d files in %d communities",
		),
		DuplicatesHeading: tr(
			"## Doublons candidates",
			"## Candidate duplicates",
		),
		DuplicatesIntroFmt: tr(
			"%d paires de règles à l'exigence quasi identique (à réconcilier) — "+"%d intra-domaine, %d cross-domaine :",
			"%d rule pairs with near-identical requirements (to reconcile) — "+"%d intra-domain, %d cross-domain:",
		),
		AndMoreFmt: tr(
			"- … et %d autres",
			"- … and %d more",
		),
		DomainsHeading: tr(
			"## Domaines",
			"## Domains",
		),
	}
}

// For returns the text for a --lang code. Config validation restricts --lang
// to "fr" and "en"; any other code gets French, the historical default.
func For(lang string) *Text {
	if lang == "en" {
		return newText("en")
	}
	return newText("fr")
}

// All returns the text of every supported language, for parsers that must
// read output written in any of them.
func All() []*Text {
	return []*Text{newText("fr"), newText("en")}
}

// Prompt fills a prompt template's placeholders for this language: <LANG>
// with the language name and <EARS> with the EARS patterns.
func (t *Text) Prompt(template string) string {
	return strings.NewReplacer("<LANG>", t.Name, "<EARS>", t.EARSPatterns).Replace(template)
}
