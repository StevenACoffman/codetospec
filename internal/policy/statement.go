package policy

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/StevenACoffman/codetospec/internal/locale"
)

// Statement renders pin as a business statement in t's language: what it
// allows, in words a person who doesn't read code follows, never written
// by hand. Relations and attributes take their registry phrase (the bare
// name when the registry gives none); capabilities their Go name in words
// (CanViewUserIdentity → "View user identity"). A filter states when one
// item is visible; an approximate pin carries its caveat; an unpinnable
// one says why nothing is stated.
//
// Ensures: deterministic; nested groups parenthesized, so the statement
// reads one way.
func Statement(pin *Checker, reg Registry, t *locale.Text) string {
	if pin.Unpinnable != "" {
		return fmt.Sprintf(t.NotStatedFmt, pin.Unpinnable)
	}
	s := statement{reg: reg, t: t}
	var out string
	switch {
	case pin.Cases != nil:
		cases := make([]string, len(pin.Cases))
		for i := range pin.Cases {
			k := &pin.Cases[i]
			args := make([]string, 0, len(k.WhenArg))
			for _, index := range slices.Sorted(maps.Keys(k.WhenArg)) {
				args = append(args, fmt.Sprintf(t.ArgFmt, index, k.WhenArg[index]))
			}
			cases[i] = fmt.Sprintf(t.CaseFmt, strings.Join(args, t.And), s.part(&k.Checker, false))
		}
		out = fmt.Sprintf(t.CasesFmt, strings.Join(cases, "; "))
	case pin.Item != "":
		out = fmt.Sprintf(t.ItemVisibleFmt, pin.Item, s.part(pin, false))
		if pin.Call != nil {
			out += fmt.Sprintf(t.CallFmt, s.part(pin.Call, false))
		}
	default:
		out = fmt.Sprintf(t.AllowedWhenFmt, s.part(pin, false))
	}
	if pin.Approx {
		out += t.ApproxNote
	}
	return out
}

// statement renders the parts of one pin.
type statement struct {
	reg Registry
	t   *locale.Text
}

// part renders c; nested says it sits inside another group, which
// parenthesizes a group of several parts.
func (s statement) part(c *Checker, nested bool) string {
	var parts []string
	var join string
	switch {
	case c.Means != "":
		return s.leaf(c)
	case c.Not != nil:
		return fmt.Sprintf(s.t.NotFmt, s.part(c.Not, true))
	case c.Any != nil:
		join = s.t.Or
		for i := range c.Any {
			parts = append(parts, s.part(&c.Any[i], true))
		}
	default:
		join = s.t.And
		for i := range c.All {
			parts = append(parts, s.part(&c.All[i], true))
		}
	}
	out := strings.Join(parts, join)
	if join == s.t.And && len(parts) > 2 {
		// A, B and C: a list reads better than a chain of ands.
		out = strings.Join(parts[:len(parts)-1], s.t.ListSep) + s.t.And + parts[len(parts)-1]
	}
	if nested && len(parts) > 1 {
		return "(" + out + ")"
	}
	return out
}

// leaf renders one leaf in words.
func (s statement) leaf(c *Checker) string {
	t := &s.t.Policy
	switch c.Means {
	case MeansLogin:
		return t.Login
	case MeansLoginStrict:
		return t.LoginStrict
	case MeansServiceAdmin:
		return t.ServiceAdmin
	case MeansTask:
		return t.Task
	case MeansOkta:
		return t.Okta
	case MeansPush:
		return t.Push
	case MeansSelf:
		return t.Self
	case MeansCoach:
		return t.Coach
	case MeansCapability:
		if c.Capability == "" {
			return fmt.Sprintf(t.CapabilityFromCallFmt, s.scope(c))
		}
		return fmt.Sprintf(t.CapabilityFmt, capabilityWords(goName(c.Capability)), s.scope(c))
	case MeansRelation:
		if phrase, ok := s.reg.phrase(c.Relation, s.t.Lang); ok {
			return fmt.Sprintf(t.RelationFmt, phrase)
		}
		return fmt.Sprintf(t.RelationNamedFmt, c.Relation)
	case MeansAttribute:
		holder := map[string]string{
			HolderCaller: t.Caller, HolderResource: t.Resource, HolderContext: t.Request,
		}[c.Of]
		if phrase, ok := s.reg.phrase(c.Attribute, s.t.Lang); ok {
			return fmt.Sprintf(t.HolderPhraseFmt, holder, phrase)
		}
		return fmt.Sprintf(t.AttributeNamedFmt, holder, c.Attribute)
	}
	return c.Means
}

// scope is the phrase following capability check c: its scope's, a
// content scope's naming its kind ("…" when the call names it).
func (s statement) scope(c *Checker) string {
	t := &s.t.Policy
	if c.Scope == ScopeContent {
		return fmt.Sprintf(t.ScopeContentFmt, cmp.Or(c.ContentKind, "…"))
	}
	return map[string]string{
		ScopeAny: t.ScopeAny, ScopeGlobal: t.ScopeGlobal, ScopeUser: t.ScopeUser,
		ScopeCountry: t.ScopeCountry, ScopeUserAll: t.ScopeUserAll,
		ScopeUserNwea: t.ScopeUserNWEA, ScopeLocale: t.ScopeLocale,
		ScopeExplicit: t.ScopeExplicit,
	}[c.Scope]
}

// capabilityWords spells a capability's Go name as words: its "Can"
// prefix dropped, camel case split, every word but the first and the
// acronyms lowercased (CanViewCDIReports → "View CDI reports").
func capabilityWords(name string) string {
	name = strings.TrimPrefix(name, "Can")
	var words []string
	runes := []rune(name)
	start := 0
	for i := 1; i <= len(runes); i++ {
		boundary := i == len(runes) ||
			unicode.IsUpper(runes[i]) && (unicode.IsLower(runes[i-1]) ||
				i+1 < len(runes) && unicode.IsLower(runes[i+1]))
		if boundary {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	for i, w := range words {
		if i > 0 && strings.ToUpper(w) != w {
			words[i] = strings.ToLower(w)
		}
	}
	return strings.Join(words, " ")
}
