package graph

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/condition"
	"github.com/StevenACoffman/codetospec/internal/ears"
	"github.com/StevenACoffman/codetospec/internal/extract"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/mapper"
)

// ExtraExtractedBy is the Extra key naming the extractor that produced a
// mechanical rule (see extract.KindRule); absent on LLM rules.
const ExtraExtractedBy = "extracted_by"

// ExtraInheritedFrom is the Extra key naming the spec an inherited rule
// comes from (see extract.AttrInheritedFrom); absent on other rules.
const ExtraInheritedFrom = "inherited_from"

// ExtraStaleVerdict is the Extra key of an inherited rule's verdict given
// on shared code that changed since (see extract.AttrStaleVerdict).
const ExtraStaleVerdict = "stale_verdict"

// ExtraEARSIssue is the Extra key holding why a rule's requirement does not
// have the EARS form its kind names (ears.Check); absent when it does.
const ExtraEARSIssue = "ears_issue"

// ExtraEARSActions is the Extra key keeping, beside a rule's sentence,
// the action of each of its responses ("create; sound", ears.Actions): a
// sentence does not say where a verb ends, so the components the model
// wrote are read back whole (RuleComponents). Set only when every action
// is known.
const ExtraEARSActions = "ears_actions"

// actionsSeparator joins ExtraEARSActions, as Render joins responses: an
// action opens its response, so it holds no ";".
const actionsSeparator = "; "

// ExtraEARSPatterns is the Extra key listing the simple EARS patterns a
// rule's sentence combines ("state, event", ears.Patterns), derived from
// the sentence; absent unless it combines two or more.
const ExtraEARSPatterns = "ears_patterns"

// Reviewable reports whether a node is a rule of this spec's LLM output,
// which the cross-check reviewer may judge: not an extractor's mechanical
// rule, nor one inherited from another spec.
func Reviewable(n *Node) bool {
	return n.Type == TypeRule && n.Extra[ExtraExtractedBy] == "" &&
		n.Extra[ExtraInheritedFrom] == ""
}

// ExtraGroundingWidened is the Extra key listing the lines grounding added to
// a rule's citations ("path:a-b, …"), because the value it states sat just
// outside them (see grounding.Annotate).
const ExtraGroundingWidened = "grounding_widened"

// ExtraCalleesDigest is the frontmatter key of the digest of a rule's
// callees (Node.Callees), which drift re-hashes.
const ExtraCalleesDigest = "callees_digest"

// ExtraDigest is the frontmatter key of the digest of a rule's cited code
// (drift.Digest), which drift re-hashes.
const ExtraDigest = "digest"

// ExtraSourceHashes is the frontmatter key of the hashes of each cited
// span's text alone, comma-separated in Sources order: drift finds code
// that moved by them (see drift.Check).
const ExtraSourceHashes = "source_hashes"

// ExtraQualityAttribute is the frontmatter key of the ISO 25010 quality
// characteristic a rule serves (see package quality).
const ExtraQualityAttribute = "quality_attribute"

// ExtraJourney is the frontmatter key listing, on an endpoint, the rules
// its journey applies, in order (see withJourneys).
const ExtraJourney = "journey"

// ExtraNatureByPosition is the frontmatter key of the nature the cited
// code's layer gives a rule, set only when it differs from the rule's
// nature (see package position).
const ExtraNatureByPosition = "nature_by_position"

// ExtraNatureEvidence is the frontmatter key of the structural evidence
// for a rule's nature (entities it touches, endpoints, conditions on an
// entity, its quality attribute), set beside ExtraNatureByPosition (see
// package position).
const ExtraNatureEvidence = "nature_evidence"

// ExtraNatureByEvidence is the frontmatter key of the nature a rule's
// structural evidence alone gives (business, technical, or open when it
// holds both), and ExtraFacets that of its facets (access): see package
// position.
const (
	ExtraNatureByEvidence = "nature_by_evidence"
	ExtraFacets           = "facets"
)

// ExtraCondition is the frontmatter key of a rule's condition, in the
// canonical compact notation (condition.Condition.Canonical), and
// ExtraConditionHash that of its hash: two rules checking the same thing
// the same way carry the same hash, whatever their wording.
const (
	ExtraCondition     = "condition"
	ExtraConditionHash = "condition_hash"
)

// ExtraChecks and ExtraCheckArgs are the frontmatter keys of what an
// extractor's access rule checks (extract.AttrChecks, extract.AttrCheckArgs):
// the checks' names and, as JSON, the operands its condition can't state.
// ExtraDelegatesTo is the handler an alias rule is checked as;
// ExtraCheckScope says a guard drops one item, not the call
// (extract.AttrCheckScope). Copied from
// the rule fact as the extractor wrote them; the policy inventory reads
// them, and grounding counts a value they carry as stated.
const (
	ExtraChecks      = "checks"
	ExtraCheckArgs   = "check_args"
	ExtraDelegatesTo = "delegates_to"
	ExtraCheckScope  = "scope_of_check"
	// ExtraAnswers names the checks whose result the guarded handler
	// returns as its value (extract.AttrAnswers).
	ExtraAnswers = "answers"
	// ExtraDelegatesToOperation is the operation an alias rule's handler
	// serves (extract.AttrDelegatesToOperation).
	ExtraDelegatesToOperation = "delegates_to_operation"
	// ExtraOperation is what an extractor rule guards (extract.AttrOperation,
	// "User.emails"): the enforcement point when it has no endpoint.
	ExtraOperation = "operation"
)

// ExtraConditionDropped is the frontmatter key of the values a rule's
// condition compared with that its cited lines do not hold: the condition
// was removed. ExtraConditionDisagreement is that of how a kept condition
// and the rule's EARS conditions differ (see grounding.Annotate).
const (
	ExtraConditionDropped      = "condition_dropped"
	ExtraConditionDisagreement = "condition_disagreement"
)

// Frontmatter keys of a rule's acceptance facts (see package acceptance):
// where the observable is seen (response, state, event, log), what it was
// read from (test, code), whether its values occur there (a grounding
// Status), how many condition examples hold ("k/n"), and the numbers of
// those the condition contradicts ("2, 4").
const (
	ExtraObservable           = "observable"
	ExtraObservableSource     = "observable_source"
	ExtraObservableGrounding  = "observable_grounding"
	ExtraExamples             = "examples"
	ExtraExamplesContradicted = "examples_contradicted"
)

// ExtraObservableCrosscheck is the frontmatter key of the verdict of the
// adversarial review of a rule's observable (acceptance.Writer.Review),
// and ExtraObservableRefuted that of the review's reason when it found
// the observable unsupported.
const (
	ExtraObservableCrosscheck = "observable_crosscheck"
	ExtraObservableRefuted    = "observable_refuted"
)

// ExtraThreshold is the frontmatter key of the threshold a
// non-functional rule's code states: the quantities of its requirement,
// each found in the cited code (see quality.AnnotateThresholds); or the
// target a person set (see package nfrtarget).
const ExtraThreshold = "threshold"

// ExtraQuantity is the frontmatter key saying a non-functional rule's
// requirement states a quantity (QuantityStated): only such a rule without
// a threshold asks for its target (see quality.AnnotateThresholds).
const ExtraQuantity = "quantity"

// QuantityStated is ExtraQuantity's value.
const QuantityStated = "stated"

// ExtraTarget is the frontmatter key of the kind of target a person set
// for a non-functional rule (measure, policy or example), and
// ExtraTargetStale that of the digest of the code it was set for, when the
// rule's cited code has changed since (see package nfrtarget).
const (
	ExtraTarget      = "target"
	ExtraTargetStale = "target_stale"
)

// Frontmatter keys of the tests reaching a rule's cited code (see
// testreach.Annotate): how many, the depth of the nearest (1 when it uses
// the cited definition itself) and the nearest few, as "path#TestName".
// Absent when the run had no test facts: unknown, not untested.
// ExtraTestsViaInterface counts the further tests reaching the cited code
// only through a call of an interface it implements (set when any).
const (
	ExtraTests             = "tests"
	ExtraTestDepth         = "test_depth"
	ExtraTestedBy          = "tested_by"
	ExtraTestsViaInterface = "tests_via_interface"
)

// ExtraExecuted is the frontmatter key of how many statement lines of a
// rule's cited code a profiled test run executed, "k/n" (see
// testreach.Annotate). Absent when the run had no coverage profile for
// every cited file: unknown, not unexecuted.
const ExtraExecuted = "executed"

// ValidateRuleFacts drops extract.KindRule facts that do not satisfy the rule
// fact contract, so an extractor bug cannot put a malformed rule in the
// graph. Facts of other kinds pass through unchanged.
//
// Requires: nothing.
// Ensures:  kept preserves the order of facts minus the invalid rule facts;
// rejected holds one "<fact id>: <reason>" per dropped fact.
func ValidateRuleFacts(facts []extract.Fact) ([]extract.Fact, []string) {
	var (
		kept     []extract.Fact
		rejected []string
	)
	routes := routesByID(facts)
	for _, f := range facts {
		if f.Kind != extract.KindRule {
			kept = append(kept, f)
			continue
		}
		reason := invalidRuleFact(&f)
		if route := f.Attrs[extract.AttrRoute]; reason == "" && route != "" &&
			routes[route] == nil {
			reason = fmt.Sprintf("unknown route %q", route)
		}
		if reason != "" {
			rejected = append(rejected, f.ID+": "+reason)
			continue
		}
		kept = append(kept, f)
	}
	return kept, rejected
}

// invalidRuleFact returns why f breaks the rule fact contract, or "".
func invalidRuleFact(f *extract.Fact) string {
	a := f.Attrs
	switch {
	case strings.TrimSpace(a[extract.AttrRequirement]) == "":
		return "empty requirement"
	case strings.TrimSpace(a[extract.AttrTitle]) == "":
		return "empty title"
	case a[extract.AttrSlug] == "" || extract.Slugify(a[extract.AttrSlug]) != a[extract.AttrSlug]:
		return fmt.Sprintf("slug %q is not a slug", a[extract.AttrSlug])
	case a[extract.AttrDomain] == "" || extract.Slugify(a[extract.AttrDomain]) != a[extract.AttrDomain]:
		return fmt.Sprintf("domain %q is not a slug", a[extract.AttrDomain])
	case !ears.IsKind(a[extract.AttrEARS]):
		return fmt.Sprintf("unknown EARS kind %q", a[extract.AttrEARS])
	case !mapper.IsNature(a[extract.AttrNature]):
		return fmt.Sprintf("unknown nature %q", a[extract.AttrNature])
	case f.Source.Path == "":
		return "no citation path"
	}
	if _, _, err := extract.ParseLines(f.Source.Lines); err != nil {
		return err.Error()
	}
	if _, err := condition.Parse([]byte(a[extract.AttrCondition])); err != nil {
		return err.Error()
	}
	// An extractor derives its rules deterministically: a malformed one is
	// its bug to fix. An inherited rule was written by another spec's model
	// and keeps its form, flagged like that spec's own rules.
	if a[extract.AttrInheritedFrom] == "" {
		if issue := ears.Check(a[extract.AttrEARS], a[extract.AttrRequirement]); issue != "" {
			return "requirement: " + issue
		}
	}
	return ""
}

// routesByID indexes the route facts by fact ID.
func routesByID(facts []extract.Fact) map[string]*extract.Fact {
	routes := make(map[string]*extract.Fact)
	for i := range facts {
		if facts[i].Kind == extract.KindRoute {
			routes[facts[i].ID] = &facts[i]
		}
	}
	return routes
}

// mechanicalRuleNode turns a valid rule fact into a rule node; id is its
// final, collision-free node id, and routes resolves its optional route.
func mechanicalRuleNode(
	f *extract.Fact,
	id string,
	routes map[string]*extract.Fact,
	text *locale.Text,
) Node {
	edges := []Edge{{Type: EdgeBelongsTo, To: "domain." + f.Attrs[extract.AttrDomain]}}
	if route := routes[f.Attrs[extract.AttrRoute]]; route != nil {
		edges = append(edges, Edge{Type: EdgeExposedBy, To: EndpointID(route)})
		sortEdges(edges)
	}
	return Node{
		ID:      id,
		Type:    TypeRule,
		Status:  StatusGenerated,
		Sources: []extract.Ref{f.Source},
		Edges:   edges,
		Title:   f.Attrs[extract.AttrTitle],
		Body:    text.Requirement + f.Attrs[extract.AttrRequirement],
		Extra: withCondition(
			withEARS(ruleFactExtra(f), f.Attrs[extract.AttrRequirement]),
			f.Attrs[extract.AttrCondition],
		),
	}
}

// withCondition sets, on extra, a map its caller has just built, the
// condition raw states (ExtraCondition, ExtraConditionHash) when it is one;
// and returns extra. A raw condition that does not parse sets nothing:
// every producer validates its own before building a node.
func withCondition(extra map[string]string, raw string) map[string]string {
	c, err := condition.Parse([]byte(raw))
	if err != nil || c.IsZero() {
		return extra
	}
	extra[ExtraCondition] = c.Canonical()
	extra[ExtraConditionHash] = c.Hash()
	return extra
}

// RuleCondition is the condition a rule node carries; the zero Condition
// when it has none, or when a person's edit left one that does not parse.
func RuleCondition(n *Node) condition.Condition {
	c, err := condition.Parse([]byte(n.Extra[ExtraCondition]))
	if err != nil {
		return condition.Condition{}
	}
	return c
}

// withEARS sets, on extra, a map its caller has just built, what the
// sentence requirement says of its EARS form: ExtraEARSIssue when it does
// not have the form of the kind extra declares, ExtraEARSPatterns when it
// combines two or more patterns; and returns extra. Every rule node goes
// through it, whoever wrote the sentence.
func withEARS(extra map[string]string, requirement string) map[string]string {
	if issue := ears.Check(extra["ears"], requirement); issue != "" {
		extra[ExtraEARSIssue] = issue
	}
	if c, err := ears.Parse(requirement); err == nil {
		if patterns := ears.Patterns(&c); len(patterns) > 1 {
			extra[ExtraEARSPatterns] = strings.Join(patterns, ", ")
		}
	}
	return extra
}

// withActions sets ExtraEARSActions on extra from the components a rule
// was written from, when each response's action is known; and returns
// extra.
func withActions(extra map[string]string, c *ears.Components) map[string]string {
	actions := ears.Actions(c)
	if len(actions) > 0 && !slices.Contains(actions, "") {
		extra[ExtraEARSActions] = strings.Join(actions, actionsSeparator)
	}
	return extra
}

// RuleComponents are the EARS components of a rule node: its sentence read
// back (ears.Parse), with the actions kept beside it where they still
// open their responses; false when the sentence does not parse.
func RuleComponents(n *Node) (ears.Components, bool) {
	c, err := ears.Parse(RuleRequirement(n.Body))
	if err != nil {
		return ears.Components{}, false
	}
	var actions []string
	if recorded := n.Extra[ExtraEARSActions]; recorded != "" {
		actions = strings.Split(recorded, actionsSeparator)
	}
	return ears.WithActions(&c, actions), true
}

// WithSubject returns rule with system as the actor of its EARS
// requirement: its components read back (RuleComponents), the actor set,
// the sentence written again in the language of its label; false, and
// rule as is, when the sentence does not parse or already names system.
//
// Ensures: rule is not modified; the kind, conditions and responses stay.
func WithSubject(rule *Node, system string) (Node, bool) {
	c, ok := RuleComponents(rule)
	if !ok || c.System == system {
		return *rule, false
	}
	c.System = system
	lines := strings.Split(rule.Body, "\n")
	for i, line := range lines {
		for _, text := range locale.All() {
			if !strings.HasPrefix(line, text.Requirement) {
				continue
			}
			lines[i] = text.Requirement + ears.Render(&c, text.Lang)
			out := *rule
			out.Body = strings.Join(lines, "\n")
			out.Extra = maps.Clone(rule.Extra)
			delete(out.Extra, ExtraEARSIssue)
			out.Extra = withEARS(out.Extra, RuleRequirement(out.Body))
			return out, true
		}
	}
	return *rule, false
}

// ruleFactExtra is the frontmatter of a rule fact's node: a mechanical
// rule names its extractor; an inherited one its spec, with that spec's
// verdict (unchecked when it had none), origin and confidence.
func ruleFactExtra(f *extract.Fact) map[string]string {
	extra := map[string]string{
		"ears":   f.Attrs[extract.AttrEARS],
		"nature": f.Attrs[extract.AttrNature],
		"origin": "explicit",
	}
	for _, carried := range [][2]string{
		{extract.AttrChecks, ExtraChecks},
		{extract.AttrCheckArgs, ExtraCheckArgs},
		{extract.AttrDelegatesTo, ExtraDelegatesTo},
		{extract.AttrAnswers, ExtraAnswers},
		{extract.AttrCheckScope, ExtraCheckScope},
		{extract.AttrDelegatesToOperation, ExtraDelegatesToOperation},
		{extract.AttrOperation, ExtraOperation},
	} {
		if v := f.Attrs[carried[0]]; v != "" {
			extra[carried[1]] = v
		}
	}
	from := f.Attrs[extract.AttrInheritedFrom]
	if from == "" {
		extra[ExtraExtractedBy] = f.Origin
		return extra
	}
	extra[ExtraInheritedFrom] = from
	extra["crosscheck"] = f.Attrs[extract.AttrCrosscheck]
	if extra["crosscheck"] == "" {
		extra["crosscheck"] = "unchecked"
	}
	if stale := f.Attrs[extract.AttrStaleVerdict]; stale != "" {
		extra[ExtraStaleVerdict] = stale
	}
	if origin := f.Attrs[extract.AttrOrigin]; origin != "" {
		extra["origin"] = origin
	}
	if confidence := f.Attrs[extract.AttrConfidence]; confidence != "" {
		extra["confidence"] = confidence
	}
	return extra
}
