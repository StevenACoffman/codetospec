package locale

// Access is the text of the access views (package policyview): the
// access matrix page and the decision diagrams it links.
type Access struct {
	AccessTitleFmt  string // %s the service
	AccessIntro     string // what the matrix shows, and what decides it
	AccessOperation string // the first column's header
	// Cells: what the policies decide for a column's callers.
	AccessAllowed, AccessDenied, AccessUndecided string
	AccessDependsFmt                             string // %s the request facts, joined by "; "
	// Request facts no caller fixes (cedarpolicy.Fact*); a check's are
	// its statement's words.
	FactKmap, FactCountry, FactLocale string
	// Callers (policy.Personas).
	PersonaAnonymous, PersonaPush, PersonaService, PersonaUser string
	PersonaPhantom, PersonaSiteAdmin                           string
	PersonaRoleFmt                                             string // %s the role, %s its grant scopes
	PersonaHoldsFmt                                            string // %s the capability in words
	// Grant scopes, by policy.Grant*.
	GrantGlobal, GrantUsersAll, GrantUsersNwea, GrantCountry, GrantLocale, GrantContent string
	AccessItemFmt                                                                       string // %s the item kind: an item policy's row
	AccessLimitedFmt                                                                    string // %s the rate limits: an obligated operation's note
	AccessDiagram                                                                       string // the link to an operation's decision diagram
	AccessSkippedTitle                                                                  string
	AccessSkippedIntro                                                                  string
	// The decision diagram.
	DiagramRequestFmt     string // %s the operation
	DiagramAllowed        string
	DiagramAllowedLimits  string // allowed, the enforcement point applying rate limits
	DiagramDenied         string
	DiagramQuestionFmt    string // %s a check's statement
	DiagramYes, DiagramNo string
}

func newAccess(tr translate) Access {
	return Access{
		AccessTitleFmt: tr("Matrice d'accès — %s", "Access matrix — %s"),
		AccessIntro: tr(
			"Chaque ligne est une opération exportée, chaque colonne un genre d'appelant "+
				"(les appelants que les politiques traitent de la même façon partagent une "+
				"colonne). Décidé par les politiques exportées dans le langage de politiques "+
				"Cedar, chargées selon leur contrat.",
			"Each row is an exported operation, each column a kind of caller (callers the "+
				"policies treat alike share a column). Decided by the exported policies in "+
				"the Cedar policy language, loaded as their contract says.",
		),
		AccessOperation: tr("Opération", "Operation"),
		AccessAllowed:   tr("autorisé", "allowed"),
		AccessDenied:    tr("refusé", "denied"),
		AccessUndecided: tr(
			"non décidé (trop de faits sur la requête)",
			"undecided (too many request facts)",
		),
		AccessDependsFmt: tr("selon : %s", "depends on: %s"),
		FactKmap: tr(
			"l'utilisateur concerné est dans un district NWEA MAP",
			"the user concerned is in an NWEA MAP district",
		),
		FactCountry: tr(
			"l'utilisateur concerné est dans le pays de l'attribution",
			"the user concerned is in the grant's country",
		),
		FactLocale: tr(
			"la langue de la requête est celle de l'attribution",
			"the request's locale is the grant's",
		),
		PersonaAnonymous: tr("Appelant anonyme", "Anonymous caller"),
		PersonaPush:      tr("Message push de la file", "Push message from the queue"),
		PersonaService:   tr("Appel administrateur d'un service", "Service administrator call"),
		PersonaUser: tr(
			"Utilisateur connecté, sans attribution",
			"Signed-in user, no grant",
		),
		PersonaPhantom:     tr("Compte fantôme", "Phantom account"),
		PersonaSiteAdmin:   tr("Administrateur du site", "Site administrator"),
		PersonaRoleFmt:     "%s (%s)",
		PersonaHoldsFmt:    tr("détient « %s » en propre", "holds “%s” itself"),
		GrantGlobal:        tr("global", "global"),
		GrantUsersAll:      tr("tous les utilisateurs", "all users"),
		GrantUsersNwea:     tr("utilisateurs NWEA MAP", "NWEA MAP users"),
		GrantCountry:       tr("un pays", "one country"),
		GrantLocale:        tr("une langue", "one locale"),
		GrantContent:       tr("un contenu", "content"),
		AccessItemFmt:      tr("voir un élément %s", "view one %s item"),
		AccessLimitedFmt:   tr("limité en débit : %s", "rate-limited: %s"),
		AccessDiagram:      tr("diagramme", "diagram"),
		AccessSkippedTitle: tr("Opérations non exportées", "Operations not exported"),
		AccessSkippedIntro: tr(
			"Les exports laissent ces opérations de côté : les politiques n'en décident rien.",
			"The exports leave these operations out: the policies decide nothing for them.",
		),
		DiagramRequestFmt: tr("Requête %s", "Request %s"),
		DiagramAllowed:    tr("Autorisé", "Allowed"),
		DiagramAllowedLimits: tr(
			"Autorisé, limites de débit appliquées",
			"Allowed, rate limits applied",
		),
		DiagramDenied:      tr("Refusé", "Denied"),
		DiagramQuestionFmt: tr("%s ?", "%s?"),
		DiagramYes:         tr("oui", "yes"),
		DiagramNo:          tr("non", "no"),
	}
}
