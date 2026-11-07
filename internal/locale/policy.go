package locale

// Policy is the text of a check's business statement (policy.Statement):
// what its pin allows, in words a person who doesn't read code follows.
type Policy struct {
	AllowedWhenFmt string // %s the condition
	ItemVisibleFmt string // %s the item kind, %s the condition
	NotStatedFmt   string // %s the person's reason
	ApproxNote     string // appended to an approximate pin's statement
	CasesFmt       string // %s the cases (CaseFmt), joined by "; "
	CaseFmt        string // %s the arguments (ArgFmt, joined by And), %s the case's condition
	ArgFmt         string // %s the argument index, %s its text as written
	And, Or        string // joining parts, with their spaces
	ListSep        string // between all but the last two parts of an and
	NotFmt         string // %s the negated part
	// Leaves, by meaning.
	Login, LoginStrict, ServiceAdmin, Task, Okta, Push, Self, Coach string
	CapabilityFmt                                                   string // %s the capability, %s its scope phrase
	CapabilityFromCallFmt                                           string // %s its scope phrase: a capability the call names
	RelationFmt                                                     string // %s the relation's phrase
	RelationNamedFmt                                                string // %s a relation no phrase words
	AttributeNamedFmt                                               string // %s the holder, %s an attribute no phrase words
	HolderPhraseFmt                                                 string // %s the holder, %s the attribute's phrase
	Caller, Resource, Request                                       string // attribute holders
	// Scopes of a capability, by scope (policy.Scope*): each a phrase
	// following the capability, with its leading space; "" for none.
	ScopeGlobal, ScopeUser, ScopeCountry, ScopeUserAll, ScopeUserNWEA string
	ScopeLocale, ScopeExplicit, ScopeAny                              string
	ScopeContentFmt                                                   string // %s the content kind
}

func newPolicy(tr translate) Policy {
	return Policy{
		AllowedWhenFmt: tr("Autorisé si %s.", "Allowed when %s."),
		ItemVisibleFmt: tr(
			"Un élément %s est visible si %s.",
			"An item of kind %s is visible when %s.",
		),
		NotStatedFmt: tr("Non énoncé : %s.", "Not stated: %s."),
		ApproxNote: tr(
			" (Approximation : le code autorise aussi des requêtes que cet énoncé exclut.)",
			" (Approximate: the code also allows some requests this statement leaves out.)",
		),
		CasesFmt: tr(
			"Selon les arguments de l'appel : %s.",
			"Depending on the call's arguments: %s.",
		),
		CaseFmt: tr("avec %s, autorisé si %s", "with %s, allowed when %s"),
		ArgFmt:  tr("l'argument %s égal à %s", "argument %s equal to %s"),
		And:     tr(" et ", " and "),
		ListSep: ", ",
		Or:      tr(", ou ", ", or "),
		NotFmt:  tr("il est faux que %s", "it is not the case that %s"),
		Login:   tr("l'appelant est connecté", "the caller is signed in"),
		LoginStrict: tr(
			"l'appelant est connecté avec un compte complet (pas un compte fantôme)",
			"the caller is signed in with a full account (not a phantom one)",
		),
		ServiceAdmin: tr(
			"la requête est un appel administrateur d'un autre service",
			"the request is an administrator call from another service",
		),
		Task: tr(
			"la requête vient d'une tâche planifiée",
			"the request comes from a scheduled task",
		),
		Okta: tr(
			"l'appelant s'est connecté par l'authentification unique Okta",
			"the caller signed in through Okta single sign-on",
		),
		Push: tr(
			"la requête est un message push de la file",
			"the request is a push message from the queue",
		),
		Self: tr("l'appelant est l'utilisateur concerné", "the caller is the user concerned"),
		Coach: tr(
			"l'appelant est coach de l'utilisateur concerné",
			"the caller coaches the user concerned",
		),
		CapabilityFmt: tr(
			"l'appelant détient la capacité « %s »%s",
			"the caller holds the capability \"%s\"%s",
		),
		CapabilityFromCallFmt: tr(
			"l'appelant détient la capacité que la requête nomme%s",
			"the caller holds the capability the request names%s",
		),
		RelationFmt: tr("l'appelant %s", "the caller %s"),
		RelationNamedFmt: tr(
			"l'appelant a la relation « %s »",
			"the caller has the relation \"%s\"",
		),
		AttributeNamedFmt: tr("%s a l'attribut « %s »", "%s has the attribute \"%s\""),
		HolderPhraseFmt:   tr("%s %s", "%s %s"),
		Caller:            tr("l'appelant", "the caller"),
		Resource:          tr("la ressource", "the resource"),
		Request:           tr("la requête", "the request"),
		ScopeGlobal:       tr(" partout", " everywhere"),
		ScopeUser:         tr(" pour l'utilisateur concerné", " for the user concerned"),
		ScopeCountry: tr(
			" pour le pays de l'utilisateur concerné",
			" for the country of the user concerned",
		),
		ScopeUserAll:  tr(" pour tous les utilisateurs", " for all users"),
		ScopeUserNWEA: tr(" pour les utilisateurs NWEA", " for NWEA users"),
		ScopeLocale: tr(
			" pour la langue de la requête (ou pour toutes)",
			" for the request's language (or for every language)",
		),
		ScopeExplicit: tr(
			" par un octroi explicite (pas un que donne un rôle)",
			" through an explicit grant (not one a role gives)",
		),
		ScopeAny: tr(" sur une portée quelconque", " on any scope"),
		ScopeContentFmt: tr(
			" pour la langue de la requête et le contenu de type « %s »",
			" for the request's language and content of kind \"%s\"",
		),
	}
}
