// Package locale holds every fixed string codetospec writes in the output
// language chosen by --lang: the EARS sentence template given to the
// model, the labels of node bodies and the README/llms.txt text. It is the
// single place to translate (EARS keywords excepted: package ears owns
// them, for Render, Parse and Check alike); writers take a Text for one
// language, while parsers accept the labels of every language (All) so a
// graph reads back whatever --lang wrote it.
package locale

import (
	"strings"

	"github.com/StevenACoffman/codetospec/internal/ears"
)

// Text is the fixed output text for one language. Strings ending in "Fmt"
// are fmt formats; their verbs are listed beside each field.
type Text struct {
	Lang string // --lang code: "fr" | "en"
	Name string // language name substituted into prompts ("French")

	// EARSTemplate is the sentence EARS components make, with placeholders
	// (ears.Template), as given to the model.
	EARSTemplate string

	// DecimalSeparator is how requirements in this language write decimals:
	// ',' in French (1,5 · thousands as 1 000), '.' in English (1.5 ·
	// thousands as 1,000). Grounding reads numbers with it.
	DecimalSeparator byte

	// LLMsPreamble opens the output llms.txt (several lines).
	LLMsPreamble string

	Labels
	Readme
	ModelFile
	Policy
}

// ModelFile is the text codetospec writes into modelith model files.
type ModelFile struct {
	// PlaceholderPrefix opens every definition codetospec writes for an
	// entity nobody described: a later run recognizes it by this prefix
	// (in any language) and replaces it once a description exists.
	PlaceholderPrefix string
	PlaceholderFmt    string // %s prefix, %s table name, %s source path:lines
	GeneratedDescFmt  string // %s the code the model covers
	// PlaceholderTypeFmt defines an API type nobody described (%s prefix,
	// %s type name, %s source path:lines); ReferencedTypesFmt and
	// DeprecatedTypesFmt list, in a proposed model's description, the
	// types owned elsewhere and the deprecated ones (%s names).
	PlaceholderTypeFmt                     string
	ReferencedTypesFmt, DeprecatedTypesFmt string
	CodeTypeFmt                            string // %s the column's type in the code
	// GeneratedHeaderFmt is the YAML comment heading a generated model,
	// after its schema line (%s the code the model covers).
	GeneratedHeaderFmt string
}

// Labels are the labels of node bodies and footers.
type Labels struct {
	// Rule bodies. Requirement and Criteria are also parsed back.
	Requirement string // label preceding the EARS requirement
	Rationale   string
	Criteria    string // label opening the numbered acceptance criteria
	// The acceptance facts (see package acceptance); Observable and
	// NonClaims are parsed back.
	Observable    string // label before the observable: (channel) check
	NonClaims     string // label opening the "- " list of non-claims
	Examples      string // label opening the "- " list of examples
	ExampleFmt    string // %s the given values (JSON), %s ExpectMatches or ExpectNoMatch
	ExpectMatches string // the condition holds
	ExpectNoMatch string // the condition does not hold
	ExampleBroken string // after an example the condition contradicts

	// Entity, endpoint and domain bodies.
	Table, Columns          string
	Method, Path, Handler   string
	DomainTitleFmt          string // %s: domain slug
	ServiceTitleFmt         string // %s: external service slug
	OperationsCalled        string // heading over a service's called operations
	Journey                 string // label over an endpoint's journey
	EnumValues              string // label over an enumeration's values
	EnumTransitions         string // label over its transitions
	EnumTransitionHeader    string // their table's header and separator rows
	AsServiceAdminNote      string // after a call made as a service-admin query
	RulesHeading            string // markdown heading over a domain's rule list
	SourcesLabel, LinksLead string // node file footer
}

// Readme is the text of the output README.md.
type Readme struct {
	ReadmeCounts
	ReadmeCoverage
	ReadmeReview
	ReadmeModel
}

// ReadmeCounts opens the README: title and node counts.
type ReadmeCounts struct {
	Title, Intro            string
	DomainsFmt, EntitiesFmt string // %d
	ServicesFmt             string // %d external services called
	EnumsFmt                string // %d enumerations, %d with transitions
	EndpointsFmt, RulesFmt  string // %d
	NatureFmt               string // %d business, presentation, technical
	QualityFmt              string // %s "attribute count" list, %d asked rules without one
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
	ExcludedFactsFmt                        string // %d extractor facts citing --exclude'd code
	GroundingFmt                            string // %d grounded, ungrounded, unchecked
	EARSIssuesFmt                           string // %d rules whose EARS form is off
	UnusedActorsFmt                         string // %s: actors no rule names
	TestedFmt                               string // %d rules a test reaches, of %d known, within %d calls
	TestedViaInterfaceFmt                   string // %d more reached only through an interface
	ExecutedFmt                             string // %d rules with executed cited code, of %d recorded
	QuestionsFmt                            string // %d questions, %d resolved
	UnusedResolutionsFmt                    string // %d resolutions matching no question
	UnusedTargetsFmt                        string // %d targets matching no non-functional rule
}

// ReadmeReview lists rules for review and the domain graph heading.
type ReadmeReview struct {
	UngroundedHeading, UngroundedIntroFmt string // %d rules
	CoverageGapsHeading, GapsIntroFmt     string // %d gaps, %d domains
	DuplicatesHeading, DuplicatesIntroFmt string // %d pairs, intra, cross
	SplitsHeading, SplitsIntroFmt         string // %d split domains
	SplitFmt                              string // %d files, %d communities
	SharedHeading, SharedIntroFmt         string // %d shared entities
	LayersHeading, LayersIntro            string
	LayersColumns                         string // the table's header rows
	EvidenceHeading, EvidenceIntro        string
	EvidenceColumns                       string // the table's header rows
	ConceptsHeading, ConceptsIntroFmt     string // %d conflicts, %d unplaced
	ConceptsColumns                       string // the table's header rows
	AndMoreFmt                            string // %d omitted entries
	DomainsHeading                        string
}

// ReadmeModel is the README comparison with human domain models.
type ReadmeModel struct {
	ModelHeading          string
	ModelIntroFmt         string // %d model files, %d lint warnings, %d threshold percent
	ModelEntitiesFmt      string // %d/%d model entities found in the code
	ModelInvariantsFmt    string // %d/%d invariants a rule states
	ModelMissingFmt       string // %d model entities missing from the code
	ModelAmbiguousFmt     string // %d model entities matching several
	ModelStaleMappingsFmt string // %d mappings matching nothing
	ModelUnmodeledFmt     string // %d code entities in no model
	ModelUnmatchedFmt     string // %d invariants no rule states
	ModelClosestFmt       string // %s rule id, %d similarity percent
	ModelUnstatedRulesFmt string // %d rules stating no invariant
	ModelActionsFmt       string // %d/%d model actions a rule performs
	ModelUnperformedFmt   string // %d model actions no rule performs
	ModelRuleActionsFmt   string // %d rule verbs no model action names
	ModelFilesHeading     string
	ModelFilesIntroFmt    string // %d files written
	ModelUndefinedFmt     string // %d entities without a definition
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
		Lang:             lang,
		Name:             tr("French", "English"),
		EARSTemplate:     ears.Template(lang),
		DecimalSeparator: decimalSeparator,
		LLMsPreamble: tr(
			`> Spécification des règles métier extraite automatiquement du code source.
> Chaque nœud est un fichier markdown avec un frontmatter YAML portant des edges typés.

Navigation : commencer par nodes/domains/, puis suivre les edges du frontmatter
(belongs_to, touches, exposed_by, depends_on). graph.json contient le graphe complet
au format machine ; les citations `+"`path:lines`"+` pointent dans le dépôt source.

Chaque règle ci-dessous porte sa citation, son verdict de revue (crosscheck) et son
ancrage (grounding). Ne s'y fier que si elle est grounded et supported, repaired ou
mechanical : unchecked n'est jamais un succès. Un domaine partial contient du code
qui n'a produit aucune règle : l'absence d'une règle n'y prouve rien. Le champ
confidence est la note que le modèle se donne, pas une vérification.`,
			`> Business-rule specification extracted automatically from the source code.
> Each node is a markdown file with YAML frontmatter carrying typed edges.

Navigation: start at nodes/domains/, then follow the frontmatter edges
(belongs_to, touches, exposed_by, depends_on). graph.json holds the full graph
in machine form; `+"`path:lines`"+` citations point into the source repository.

Each rule below carries its citation, its review verdict (crosscheck) and its
grounding. Rely on it only when it is grounded and supported, repaired or
mechanical: unchecked is never a pass. A partial domain holds code that produced
no rule: a rule missing there proves nothing. The confidence field is the
model's own rating, not a check.`,
		),
		Labels:    newLabels(tr),
		Readme:    newReadme(tr),
		ModelFile: newModelFile(tr),
		Policy:    newPolicy(tr),
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
		Observable: tr(
			"**Observable** (comment distinguer le succès d'une imitation) : ",
			"**Observable** (how to tell success from an imitation): ",
		),
		NonClaims: tr(
			"**Ne promet pas** :",
			"**Does not claim**:",
		),
		Examples: tr(
			"**Exemples de la condition** :",
			"**Condition examples**:",
		),
		ExampleFmt:    tr("- avec `%s` : %s", "- given `%s`: %s"),
		ExpectMatches: tr("la condition est vraie", "the condition holds"),
		ExpectNoMatch: tr("la condition est fausse", "the condition does not hold"),
		ExampleBroken: tr(" (la condition dit le contraire)", " (the condition says otherwise)"),
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
		Journey: tr(
			"**Parcours** (règles que le handler applique, puis celles de ce qu'il appelle) :",
			"**Journey** (the rules the handler applies, then those of what it calls):",
		),
		ServiceTitleFmt:  tr("Service %s", "Service %s"),
		OperationsCalled: tr("## Opérations appelées", "## Operations called"),
		EnumValues:       tr("**Valeurs** :", "**Values**:"),
		EnumTransitions: tr(
			"**Transitions** (le code, tant que la valeur est « De », la passe à « Vers ») :",
			"**Transitions** (code that, while the value is From, sets it to To):",
		),
		EnumTransitionHeader: tr(
			"| De | Vers | Code |\n|----|------|------|",
			"| From | To | Code |\n|------|----|------|",
		),
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

func newModelFile(tr translate) ModelFile {
	return ModelFile{
		PlaceholderPrefix: tr("Pas encore défini.", "Not yet defined."),
		PlaceholderFmt: tr(
			"%s codetospec l'a trouvée comme la table %s (%s) : dire ce qu'elle est, et ce qu'elle n'est pas.",
			"%s codetospec found it as the table %s (%s): say what it is, and what it is not.",
		),
		GeneratedDescFmt: tr(
			"Concepts que codetospec propose pour %s à partir de son code (tables et types d'entité de son API qu'aucun modèle humain ne nomme, possession par les clés étrangères NOT NULL) : une proposition, à confirmer ou corriger avant qu'elle ne regroupe les règles.",
			"Concepts codetospec proposes for %s from its code (tables and API entity types no human model names, ownership from NOT NULL foreign keys): a proposal, to confirm or correct before it groups rules.",
		),
		PlaceholderTypeFmt: tr(
			"%s codetospec l'a trouvé comme le type d'API %s (%s) : dire ce qu'il est, et ce qu'il n'est pas.",
			"%s codetospec found it as the API type %s (%s): say what it is, and what it is not.",
		),
		ReferencedTypesFmt: tr(
			"Concepts référencés, possédés ailleurs : %s.",
			"Concepts it references, owned elsewhere: %s.",
		),
		DeprecatedTypesFmt: tr(
			"Types d'API dépréciés, laissés de côté (encore utiles ?) : %s.",
			"Deprecated API types, left out (still needed?): %s.",
		),
		CodeTypeFmt: tr("type dans le code : %s", "code type: %s"),
		GeneratedHeaderFmt: tr(
			"# Généré par codetospec à partir du code de %s.\n"+
				"# Modifiez-le comme votre propre modèle : une définition que vous écrivez est conservée.",
			"# Generated by codetospec from the code of %s.\n"+
				"# Edit it as a model of your own: a definition you write is kept.",
		),
	}
}

func newReadme(tr translate) Readme {
	return Readme{
		ReadmeCounts:   newReadmeCounts(tr),
		ReadmeCoverage: newReadmeCoverage(tr),
		ReadmeReview:   newReadmeReview(tr),
		ReadmeModel:    newReadmeModel(tr),
	}
}

func newReadmeModel(tr translate) ReadmeModel {
	return ReadmeModel{
		ModelHeading: tr(
			"## Modèle de domaine",
			"## Domain model",
		),
		ModelIntroFmt: tr(
			"Comparaison sans LLM avec %d fichier(s) de modèle modelith (%d avertissements de lint, détaillés par `modelith lint`). "+"Invariants et règles sont appariés par similarité de mots (seuil %d %%) : une piste à vérifier, pas un verdict.",
			"Compared without an LLM with %d modelith model file(s) (%d lint warnings, listed by `modelith lint`). "+"Invariants and rules are paired by word similarity (threshold %d%%): a lead to check, not a verdict.",
		),
		ModelEntitiesFmt: tr(
			"- Entités du modèle trouvées dans le code : %d/%d",
			"- Model entities found in the code: %d/%d",
		),
		ModelInvariantsFmt: tr(
			"- Invariants énoncés par une règle : %d/%d",
			"- Invariants a rule states: %d/%d",
		),
		ModelMissingFmt: tr(
			"%d entités du modèle n'ont pas d'entité de code du même nom : un autre nom (à déclarer sous `domain_model_entities`), ou pas encore dans le code ?",
			"%d model entities have no code entity of the same name: named differently (map them under `domain_model_entities`), or not in the code yet?",
		),
		ModelAmbiguousFmt: tr(
			"%d entités du modèle correspondent à plusieurs entités de code : laquelle est-ce (à déclarer sous `domain_model_entities`) ?",
			"%d model entities match several code entities: which one is it (map it under `domain_model_entities`)?",
		),
		ModelStaleMappingsFmt: tr(
			"%d correspondances de `domain_model_entities` ne trouvent rien : entité renommée, ou faute de frappe ?",
			"%d `domain_model_entities` mappings match nothing: a renamed entity, or a typo?",
		),
		ModelUnmodeledFmt: tr(
			"%d entités de code ne figurent dans aucun modèle : à ajouter au modèle, ou hors de son périmètre ?",
			"%d code entities appear in no model: worth adding to the model, or outside its scope?",
		),
		ModelUnmatchedFmt: tr(
			"%d invariants ne correspondent à aucune règle : pas implémentés, ou manqués par l'extraction ?",
			"%d invariants match no rule: not implemented, or missed by the extraction?",
		),
		ModelClosestFmt: tr(
			"plus proche : `%s`, %d %%",
			"closest: `%s`, %d%%",
		),
		ModelFilesHeading: tr(
			"## Fichiers du modèle de domaine",
			"## Domain model files",
		),
		ModelFilesIntroFmt: tr(
			"%d fichiers de modèle modelith écrits sous `models/` (provenance de chaque champ : `models/provenance.json`) :",
			"%d modelith model files written under `models/` (where each field comes from: `models/provenance.json`):",
		),
		ModelUndefinedFmt: tr(
			"%d entités n'ont pas encore de définition : que sont-elles, et que ne sont-elles pas ?",
			"%d entities have no definition yet: what are they, and what are they not?",
		),
		ModelUnstatedRulesFmt: tr(
			"%d règles sur des entités modélisées n'énoncent aucun invariant du modèle : à ajouter au modèle ?",
			"%d rules about modeled entities state no invariant of the model: worth adding to it?",
		),
		ModelActionsFmt: tr(
			"- Actions du modèle qu'une règle accomplit (même verbe) : %d/%d",
			"- Model actions a rule performs (same verb): %d/%d",
		),
		ModelUnperformedFmt: tr(
			"%d actions du modèle ne sont accomplies par aucune règle de leur entité : pas implémentées, ou décrites avec un autre verbe ?",
			"%d model actions are performed by no rule of their entity: not implemented, or described with another verb?",
		),
		ModelRuleActionsFmt: tr(
			"%d verbes de règles sur des entités modélisées ne sont nommés par aucune action du modèle : actions à ajouter au modèle ?",
			"%d verbs of rules about modeled entities are named by no model action: actions worth adding to the model?",
		),
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
		EnumsFmt: tr(
			"- Énumérations : %d (dont %d avec transitions)",
			"- Enumerations: %d (%d with transitions)",
		),
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
		QualityFmt: tr(
			"  - qualité ISO 25010 (hors règles de présentation) : %s ; %d sans attribut",
			"  - ISO 25010 quality (rules but presentation): %s; %d without one",
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
		ExcludedFactsFmt: tr(
			"- Faits d'extracteur écartés (ils citent du code exclu par --exclude, voir les logs) : %d",
			"- Extractor facts dropped (they cite code --exclude leaves out, see the logs): %d",
		),
		MechanicalFmt: tr(
			", %d mécaniques (extraites du code, non soumises au LLM)",
			", %d mechanical (extracted from the code, not sent to the LLM)",
		),
		GroundingFmt: tr(
			"- Ancrage mécanique (valeurs citées présentes dans le code) : %d ancrées, %d non ancrées, %d sans valeur vérifiable",
			"- Mechanical grounding (stated values present in the cited code): %d grounded, %d ungrounded, %d with no checkable value",
		),
		EARSIssuesFmt: tr(
			"- Forme EARS : %d règles n'ont pas la forme de leur type (`ears_issue`)",
			"- EARS form: %d rules do not have the form their kind names (`ears_issue`)",
		),
		UnusedActorsFmt: tr(
			"- Acteurs du glossaire qu'aucune règle ne nomme : %s",
			"- Glossary actors no rule names: %s",
		),
		TestedFmt: tr(
			"- Tests : %d règles sur %d ont leur code cité atteint par un test, en %d appels au plus (`tests`, `tested_by`)",
			"- Tests: %d of %d rules have their cited code reached by a test, within %d calls (`tests`, `tested_by`)",
		),
		TestedViaInterfaceFmt: tr(
			" ; %d de plus seulement par un appel d'interface (`tests_via_interface` : peut-être exécuté)",
			"; %d more only through an interface call (`tests_via_interface`: possibly run)",
		),
		QuestionsFmt: tr(
			"- Questions : %d constats à revoir (`questions.json`), dont %d résolus dans `resolutions.yaml` (non listés ci-dessous)",
			"- Questions: %d findings to review (`questions.json`), %d of them resolved in `resolutions.yaml` (not listed below)",
		),
		UnusedResolutionsFmt: tr(
			" ; %d résolutions ne correspondent plus à aucune question (code cité modifié, ou constat disparu) : à retirer",
			"; %d resolutions match no question any more (cited code changed, or the finding is gone): remove them",
		),
		UnusedTargetsFmt: tr(
			" ; %d cibles de `targets.yaml` ne correspondent à aucune règle non fonctionnelle (règle disparue, renommée ou devenue fonctionnelle)",
			"; %d targets in `targets.yaml` match no non-functional rule (rule gone, renamed, or now functional)",
		),
		ExecutedFmt: tr(
			"- Exécution (profil de couverture) : %d règles sur %d ont du code cité exécuté par les tests (`executed`)",
			"- Execution (coverage profile): %d of %d rules have cited code the tests executed (`executed`)",
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
		CoverageGapsHeading: tr(
			"## Code sans règle",
			"## Code without rules",
		),
		GapsIntroFmt: tr(
			"%d morceaux de code de %d domaines n'ont produit aucune règle "+"(fichier généré, appel map ou reduce en échec) : ces domaines sont `coverage: partial`, l'absence d'une règle n'y prouve rien :",
			"%d pieces of code in %d domains produced no rule "+"(generated file, failed map or reduce call): those domains are `coverage: partial`, and a rule missing there proves nothing:",
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
		SharedHeading: tr(
			"## Entités partagées",
			"## Shared entities",
		),
		SharedIntroFmt: tr(
			"%d entités sont touchées par des règles de plusieurs domaines : un noyau partagé à nommer comme tel, ou une frontière de domaine qui traverse un concept ?",
			"%d entities are touched by rules of several domains: a shared kernel to name as such, or a domain boundary running through one concept?",
		),
		LayersHeading: tr(
			"## Couches",
			"## Layers",
		),
		LayersIntro: tr(
			"Les règles de chaque couche de `nature_by_path` ou `nature_by_layer` dont le texte donne une autre nature que la couche : une part élevée dit que la correspondance est fausse, pas les règles. Une règle n'est une question que si ses propres indices (une entité touchée, un endpoint, une condition sur une entité, son attribut de qualité) appuient la couche.",
			"The rules of each layer of `nature_by_path` or `nature_by_layer` whose text gives another nature than the layer: a high share says the mapping is wrong, not the rules. A rule is a question only when its own evidence (an entity it touches, an endpoint, a condition on an entity, its quality attribute) backs the layer.",
		),
		LayersColumns: tr(
			"| couche | nature | règles | en désaccord | questions |\n|---|---|---:|---:|---:|",
			"| layer | nature | rules | disagree | questions |\n|---|---|---:|---:|---:|",
		),
		EvidenceHeading: tr(
			"## Nature par les indices",
			"## Nature by evidence",
		),
		EvidenceIntro: tr(
			"La nature que les seuls indices structures de chaque règle donnent (`nature_by_evidence`, sans LLM ni mots du texte) : métier par une entité touchée, un endpoint ou une condition sur une entité ; technique par un attribut de qualité technique ou du code de plateforme (règle héritée, ou citant seulement une autre racine) ; ouverte quand les deux. Le regroupement par concept ne porte que sur les règles métier ; « en désaccord » compte celles dont la `nature` du LLM diffère.",
			"The nature each rule's structural evidence alone gives (`nature_by_evidence`, no LLM, no words of its text): business by an entity it touches, an endpoint or a condition on an entity; technical by a technical quality attribute or platform code (an inherited rule, or one citing only another root); open when both. Grouping by concept bears on business rules only; \"disagree\" counts those whose LLM `nature` differs.",
		),
		EvidenceColumns: tr(
			"| domaine | règles | métier | technique | ouverte | sans indice | en désaccord |\n|---|---:|---:|---:|---:|---:|---:|",
			"| domain | rules | business | technical | open | no evidence | disagree |\n|---|---:|---:|---:|---:|---:|---:|",
		),
		ConceptsHeading: tr(
			"## Concepts",
			"## Concepts",
		),
		ConceptsIntroFmt: tr(
			"Les règles métier que leurs indices (entités touchées, conditions sur leurs tables) placent dans un concept des modèles confirmés (`concept`, `concept_evidence`), plusieurs concepts allant à leur propriétaire commun le plus proche. Le regroupement n'est pas appliqué : le tableau dit sa taille. %d règles nomment des concepts sans propriétaire commun (`concept_conflict`, une question chacune) ; %d règles métier ne nomment aucun concept.",
			"The business rules their evidence (entities touched, conditions on their tables) places in a concept of the confirmed models (`concept`, `concept_evidence`), several concepts going to their nearest common owner. The regrouping is not applied: the table states its size. %d rules name concepts with no common owner (`concept_conflict`, a question each); %d business rules name no concept.",
		),
		ConceptsColumns: tr(
			"| concept | règles | domaines actuels |\n|---|---:|---|",
			"| concept | rules | current domains |\n|---|---:|---|",
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
// to "en" and "fr"; any other code gets English, the default.
func For(lang string) *Text {
	if lang == "fr" {
		return newText("fr")
	}
	return newText("en")
}

// All returns the text of every supported language, for parsers that must
// read output written in any of them.
func All() []*Text {
	return []*Text{newText("fr"), newText("en")}
}

// Prompt fills a prompt template's placeholders for this language: <LANG>
// with the language name and <EARS> with the EARS sentence template.
func (t *Text) Prompt(template string) string {
	return strings.NewReplacer("<LANG>", t.Name, "<EARS>", t.EARSTemplate).Replace(template)
}
