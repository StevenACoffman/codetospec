package ears_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/StevenACoffman/codetospec/internal/ears"
)

// TestRenderParse: components are written in canonical order, in either
// language, and read back as they were; the kind is the one the sentence
// opens with.
func TestRenderParse(t *testing.T) {
	cases := map[string]struct {
		components ears.Components
		lang, want string
		kind       string
	}{
		"ubiquitous": {
			ears.Components{Shall: says("round amounts to cents")},
			"en", "The system shall round amounts to cents.", ears.Ubiquitous,
		},
		"ubiquitous french negative": {
			ears.Components{Shall: says("ne pas stocker les numéros de carte")},
			"fr", "Le système ne doit pas stocker les numéros de carte.", ears.Ubiquitous,
		},
		"event, two responses": {
			ears.Components{
				When:  "a user signs up",
				Shall: says("send a welcome email", "log the sign-up"),
			},
			"en",
			"WHEN a user signs up, the system shall send a welcome email; log the sign-up.",
			ears.Event,
		},
		"state then event": {
			ears.Components{
				While: []string{"the account is locked"},
				When:  "a login arrives",
				Shall: says("reject it"),
			},
			"en",
			"WHILE the account is locked, WHEN a login arrives, the system shall reject it.",
			ears.Complex,
		},
		"every clause, french": {
			ears.Components{
				Where: []string{"le SSO est actif"},
				While: []string{"le compte est bloqué", "la session est ouverte"},
				If:    "le jeton est expiré",
				Shall: says("refuser la connexion"),
			},
			"fr",
			"LÀ OÙ le SSO est actif, TANT QUE le compte est bloqué ET " +
				"la session est ouverte, " +
				"SI le jeton est expiré, ALORS le système doit refuser la connexion.",
			ears.Complex,
		},
		"optional": {
			ears.Components{Where: []string{"SSO is enabled"}, Shall: says("hide the form")},
			"en", "WHERE SSO is enabled, the system shall hide the form.", ears.Optional,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := ears.Render(&tc.components, tc.lang)
			if got != tc.want {
				t.Errorf("Render = %q\nwant     %q", got, tc.want)
			}
			if kind := ears.KindOf(&tc.components); kind != tc.kind {
				t.Errorf("KindOf = %s, want %s", kind, tc.kind)
			}
			if issue := ears.Check(tc.kind, got); issue != "" {
				t.Errorf("Check(%s): %s", tc.kind, issue)
			}
			back, err := ears.Parse(got)
			if err != nil || !reflect.DeepEqual(back, tc.components) {
				t.Errorf("Parse = %+v, %v; want %+v", back, err, tc.components)
			}
		})
	}
}

// noModal opens Parse's error for a sentence without one modal word.
const noModal = `no "shall"`

// TestGenericSyntax: the generic syntax [Trigger] [Precondition] Actor
// Action [Object] and complex requirements with several triggers, read as
// written and rendered canonically: preconditions first, joined by a
// capital AND; a precondition joined by a lowercase "and" stays in the
// trigger, since a parser cannot tell it from a compound trigger.
func TestGenericSyntax(t *testing.T) {
	cases := map[string]struct {
		sentence, render string
		want             ears.Components
		kind             string
	}{
		"two triggers, another actor": {
			sentence: "When the landing gear button is depressed once, if the software " +
				"detects that the landing gear does not lock into position, then the " +
				"software shall sound an alarm.",
			render: "WHEN the landing gear button is depressed once, IF the software " +
				"detects that the landing gear does not lock into position, THEN the " +
				"software shall sound an alarm.",
			want: ears.Components{
				When: "the landing gear button is depressed once",
				If: "the software detects that the landing gear does not lock " +
					"into position",
				System: "the software",
				Shall:  says("sound an alarm"),
			},
			kind: ears.Complex,
		},
		"precondition after the trigger, capital AND": {
			sentence: "WHEN an Order is shipped AND Order Terms are not “Prepaid”, " +
				"the system shall create an Invoice.",
			render: "WHILE Order Terms are not “Prepaid”, WHEN an Order is shipped, " +
				"the system shall create an Invoice.",
			want: ears.Components{
				While: []string{"Order Terms are not “Prepaid”"},
				When:  "an Order is shipped",
				Shall: says("create an Invoice"),
			},
			kind: ears.Complex,
		},
		"precondition after the trigger, lowercase and": {
			sentence: "When an Order is shipped and Order Terms are not “Prepaid”, " +
				"the system shall create an Invoice.",
			render: "WHEN an Order is shipped and Order Terms are not “Prepaid”, " +
				"the system shall create an Invoice.",
			want: ears.Components{
				When:  "an Order is shipped and Order Terms are not “Prepaid”",
				Shall: says("create an Invoice"),
			},
			kind: ears.Event,
		},
		"ubiquitous actor": {
			sentence: "The mobile phone shall have a mass of less than XX grams.",
			render:   "The mobile phone shall have a mass of less than XX grams.",
			want: ears.Components{
				System: "the mobile phone",
				Shall:  says("have a mass of less than XX grams"),
			},
			kind: ears.Ubiquitous,
		},
		"french elided actor, negated": {
			sentence: "QUAND un abonné part, l'application ne doit pas facturer.",
			render:   "QUAND un abonné part, l'application ne doit pas facturer.",
			want: ears.Components{
				When: "un abonné part", System: elidedActor,
				Shall: says("ne pas facturer"),
			},
			kind: ears.Event,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ears.Parse(tc.sentence)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Parse = %+v, %v\nwant    %+v", got, err, tc.want)
			}
			if kind := ears.KindOf(&got); kind != tc.kind {
				t.Errorf("KindOf = %s, want %s", kind, tc.kind)
			}
			if issue := ears.Check(tc.kind, tc.sentence); issue != "" {
				t.Errorf("Check(%s, sentence): %s", tc.kind, issue)
			}
			lang := "en"
			if strings.Contains(tc.sentence, "doit") {
				lang = "fr"
			}
			if r := ears.Render(&got, lang); r != tc.render {
				t.Errorf("Render = %q\nwant     %q", r, tc.render)
			}
		})
	}
}

const (
	issueManager = "the Issue_Manager" // a Kiro glossary actor
	notCreate    = "ne pas créer"      // a French negated action
	noter        = "noter"             // a French action
	elidedActor  = "l'application"     // a French actor, its article elided
)

// TestKiro: Kiro writes the actor's article and the modal in capitals
// ("THE Issue_Manager SHALL"), with or without a comma before them; the
// actor keeps its name, its article lowercase, and RenderCapitals writes
// Kiro's sentence back.
func TestKiro(t *testing.T) {
	cases := map[string]struct {
		sentence, capitals string
		want               ears.Components
	}{
		"glossary actor": {
			sentence: "WHEN a new issue is created, THE Issue_Manager SHALL analyze " +
				"the issue title and body using Bedrock_Classifier",
			capitals: "WHEN a new issue is created, THE Issue_Manager SHALL analyze " +
				"the issue title and body using Bedrock_Classifier.",
			want: ears.Components{
				When: "a new issue is created", System: issueManager,
				Shall: says("analyze the issue title and body using Bedrock_Classifier"),
			},
		},
		"documented form, no comma": {
			sentence: "WHEN a user submits a form with invalid data THE SYSTEM SHALL " +
				"display validation errors next to the relevant fields",
			capitals: "WHEN a user submits a form with invalid data, THE SYSTEM SHALL " +
				"display validation errors next to the relevant fields.",
			want: ears.Components{
				When:  "a user submits a form with invalid data",
				Shall: says("display validation errors next to the relevant fields"),
			},
		},
		"ubiquitous, negated": {
			sentence: "THE Issue_Manager SHALL not fail the entire workflow run if " +
				"individual issue processing fails",
			capitals: "THE Issue_Manager SHALL not fail the entire workflow run if " +
				"individual issue processing fails.",
			want: ears.Components{
				System: issueManager,
				Shall: says(
					"not fail the entire workflow run if individual issue processing fails",
				),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ears.Parse(tc.sentence)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Parse = %+v, %v\nwant    %+v", got, err, tc.want)
			}
			if issue := ears.Check(ears.KindOf(&got), tc.sentence); issue != "" {
				t.Errorf("Check: %s", issue)
			}
			if r := ears.RenderCapitals(&got, "en"); r != tc.capitals {
				t.Errorf("RenderCapitals = %q\nwant           %q", r, tc.capitals)
			}
		})
	}
}

// TestInCapitals: a sentence in either language is rewritten in Kiro's
// style; the actor's name loses its article for a glossary.
func TestInCapitals(t *testing.T) {
	cases := map[string]string{
		"WHEN x, the system shall y.":           "WHEN x, THE SYSTEM SHALL y.",
		"QUAND x, le système ne doit pas y.":    "QUAND x, LE SYSTÈME NE DOIT pas y.",
		"The scheduler shall run nightly.":      "THE scheduler SHALL run nightly.",
		"QUAND x, l'application doit facturer.": "QUAND x, L'application DOIT facturer.",
	}
	for sentence, want := range cases {
		if got, err := ears.InCapitals(sentence); err != nil || got != want {
			t.Errorf("InCapitals(%q) = %q, %v; want %q", sentence, got, err, want)
		}
	}
	if _, err := ears.InCapitals("Users shall wait."); err == nil {
		t.Error("a sentence without an actor was rewritten")
	}
	for system, want := range map[string]string{
		"the scheduler": "scheduler", elidedActor: "application", "la file": "file",
	} {
		if got := ears.ActorName(system); got != want {
			t.Errorf("ActorName(%q) = %q, want %q", system, got, want)
		}
	}
}

// TestWithActions: an action recorded beside a sentence is applied while
// it still opens its response, word for word; an edit that moves it, or
// adds or removes a response, makes it unknown, never wrong.
func TestWithActions(t *testing.T) {
	parsed, err := ears.Parse("QUAND x, le système ne doit pas créer la facture; noter l'échec")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		actions []string
		want    []string
	}{
		"both apply":     {[]string{notCreate, noter}, []string{notCreate, noter}},
		"one edited":     {[]string{"ne pas émettre", noter}, []string{"", noter}},
		"inside a word":  {[]string{"ne pas cré", noter}, []string{"", noter}},
		"count mismatch": {[]string{notCreate}, []string{"", ""}},
		"none recorded":  {nil, []string{"", ""}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := ears.WithActions(&parsed, tc.actions)
			if actions := ears.Actions(&got); !reflect.DeepEqual(actions, tc.want) {
				t.Errorf("actions = %q, want %q", actions, tc.want)
			}
			if parsed.Shall[0].Action != "" {
				t.Error("WithActions changed its argument")
			}
		})
	}
	if obj := ears.WithActions(&parsed, []string{notCreate, noter}).Shall[0].Object(); obj != "la facture" {
		t.Errorf("Object = %q, want %q", obj, "la facture")
	}
}

// TestPatterns: the simple patterns a requirement combines, in clause
// order, and the kind they make.
func TestPatterns(t *testing.T) {
	cases := map[string]struct {
		components ears.Components
		patterns   []string
		kind       string
	}{
		"no keyword": {ears.Components{Shall: says("x")}, nil, ears.Ubiquitous},
		"event": {
			ears.Components{When: "a", Shall: says("x")}, []string{ears.Event}, ears.Event,
		},
		"state and event": {
			ears.Components{While: []string{"a", "b"}, When: "c", Shall: says("x")},
			[]string{ears.State, ears.Event},
			ears.Complex,
		},
		"feature, state, unwanted": {
			ears.Components{
				Where: []string{"a"}, While: []string{"b"}, If: "c", Shall: says("x"),
			},
			[]string{ears.Optional, ears.State, ears.Unwanted},
			ears.Complex,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := ears.Patterns(&tc.components); !reflect.DeepEqual(got, tc.patterns) {
				t.Errorf("Patterns = %v, want %v", got, tc.patterns)
			}
			if got := ears.KindOf(&tc.components); got != tc.kind {
				t.Errorf("KindOf = %s, want %s", got, tc.kind)
			}
		})
	}
}

// TestParse: sentences people and extractors write read into components;
// a sentence with no subject, no keyword, clauses out of order or an IF
// without THEN is refused rather than guessed.
func TestParse(t *testing.T) {
	cases := map[string]struct {
		sentence string
		want     ears.Components
		err      string
	}{
		"extractor restriction": {
			sentence: "The system shall serve `getUser` only when the caller is an admin.",
			want: ears.Components{
				Shall: says("serve `getUser` only when the caller is an admin"),
			},
		},
		"unaccented french, lorsque": {
			sentence: "LORSQUE un abonnement démarre, le systeme doit facturer.",
			want:     ears.Components{When: "un abonnement démarre", Shall: says("facturer")},
		},
		"unwanted without comma": {
			sentence: "IF the amount is negative THEN the system shall reject it",
			want:     ears.Components{If: "the amount is negative", Shall: says("reject it")},
		},
		"no subject": {sentence: "WHEN x, users shall wait.", err: "subject"},
		"no article": {sentence: "Users shall be notified.", err: "subject"},
		"two modals": {
			sentence: "The system shall a and the system shall b.",
			err:      "more than one",
		},
		"no modal":   {sentence: "The system rounds amounts.", err: noModal},
		"no keyword": {sentence: "Once paid, the system shall ship.", err: "no EARS keyword"},
		"trigger, then precondition": {
			sentence: "WHEN x, WHILE y, the system shall z.",
			want:     ears.Components{While: []string{"y"}, When: "x", Shall: says("z")},
		},
		"IF not last": {sentence: "IF x, WHEN y, THEN the system shall z.", err: "last condition"},
		"two WHENs": {
			sentence: "WHEN x, WHEN y, the system shall z.",
			err:      "more than one WHEN",
		},
		"if, no then":  {sentence: "IF x, the system shall z.", err: "THEN"},
		"then, no if":  {sentence: "WHEN x, THEN the system shall z.", err: "THEN"},
		"empty answer": {sentence: "The system shall a; ; b.", err: "empty response"},
		"suffix modal": {sentence: "The system marshall.", err: noModal},
		"prefix modal": {sentence: "The system shallow.", err: noModal},
		"bad clause 2": {
			sentence: "WHEN x, then y, the system shall z.",
			want:     ears.Components{When: "x, then y", Shall: says("z")},
		},
		"capital later": {
			sentence: "When a, If b, Then the system shall c.",
			want:     ears.Components{When: "a", If: "b", Shall: says("c")},
		},
		"spaces around the commas": {
			sentence: "  While   a ,   when b , the system shall c . ",
			want:     ears.Components{While: []string{"a"}, When: "b", Shall: says("c")},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ears.Parse(tc.sentence)
			switch {
			case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
				t.Fatalf("error = %v, want one holding %q", err, tc.err)
			case tc.err == "" && err != nil:
				t.Fatalf("Parse: %v", err)
			case tc.err == "" && !reflect.DeepEqual(got, tc.want):
				t.Errorf("Parse = %+v\nwant    %+v", got, tc.want)
			}
		})
	}
}

// TestNormalizedValidate: the slips a model makes are repaired without a
// correction round; what cannot be written as one sentence that reads back
// is named by component.
func TestNormalizedValidate(t *testing.T) {
	slipped := ears.Components{
		When: " WHEN a user signs up, ",
		If:   "if the email is taken, then",
		Shall: []ears.Response{
			{Text: "the system shall reject the sign-up.", Action: "the system shall reject"},
			{Text: "Le système ne doit pas créer le compte", Action: notCreate},
		},
		System: "THE Issue_Manager",
	}
	want := ears.Components{
		When:   "a user signs up",
		If:     "the email is taken",
		System: issueManager,
		Shall: []ears.Response{
			{Text: "reject the sign-up", Action: "reject"},
			{Text: "ne pas créer le compte", Action: notCreate},
		},
	}
	if got := slipped.Normalized(); !reflect.DeepEqual(got, want) {
		t.Errorf("Normalized = %+v\nwant         %+v", got, want)
	}

	cases := map[string]struct {
		components ears.Components
		err        string
	}{
		"valid":       {ears.Components{When: "x", Shall: does("y")}, ""},
		"no response": {ears.Components{When: "x"}, "ears.shall"},
		"no action": {
			ears.Components{Shall: says("create an Invoice")}, "ears.shall[0]: action is required",
		},
		"action not opening its text": {
			ears.Components{Shall: []ears.Response{{Text: "create an Invoice", Action: "invoice"}}},
			"not the opening words",
		},
		"action ending inside a word": {
			ears.Components{Shall: []ears.Response{{Text: "created it", Action: "create"}}},
			"not the opening words",
		},
		"phrasal action": {
			ears.Components{
				Shall: []ears.Response{{Text: "log out the user", Action: "log out"}},
			},
			"",
		},
		"empty clause": {
			ears.Components{Where: []string{""}, Shall: does("y")},
			"ears.where: empty",
		},
		"semicolon":    {ears.Components{Shall: does("a; b")}, `ears.shall[0]: ";"`},
		"WHEN and IF":  {ears.Components{When: "x", If: "y", Shall: does("z")}, ""},
		"actor":        {ears.Components{System: "the software", Shall: does("z")}, ""},
		"elided actor": {ears.Components{System: elidedActor, Shall: does("z")}, ""},
		"actor without article": {
			ears.Components{System: "software", Shall: does("z")},
			"ears.system: opens with no article",
		},
		"actor with a comma": {
			ears.Components{System: "the engine, left", Shall: does("z")}, "ears.system",
		},
		"default actor spelled out": {
			ears.Components{System: "the system", Shall: does("z")}, "ears.system",
		},
		"capital AND in a clause": {
			ears.Components{When: "x AND y", Shall: does("z")}, "ears.when",
		},
		"lowercase and in a clause": {
			ears.Components{When: "x and y", Shall: does("z")}, "",
		},
		"modal": {
			ears.Components{When: "users shall wait", Shall: does("y")},
			"ears.when",
		},
		"two clauses": {ears.Components{When: "x, if y", Shall: does("z")}, "second clause"},
		"two clauses, spaced": {
			ears.Components{When: "x,  if y", Shall: does("z")}, "second clause",
		},
		"trailing then": {
			ears.Components{While: []string{"x then"}, Shall: does("z")},
			"ears.while",
		},
		"padded":         {ears.Components{If: " x", Shall: does("z")}, "spaces"},
		"comma response": {ears.Components{Shall: does("reject it, when late")}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := ears.Validate(&tc.components)
			switch {
			case tc.err == "" && err != nil:
				t.Fatalf("Validate: %v", err)
			case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
				t.Fatalf("error = %v, want one holding %q", err, tc.err)
			}
		})
	}
}

// FuzzRenderParse: any components Validate accepts after Normalized are
// rendered, in either language, as a sentence Parse reads back to them and
// Check accepts for KindOf: the contract that lets a rule be kept as its
// sentence alone.
func FuzzRenderParse(f *testing.F) {
	f.Add("SSO is on", "locked|open", "a login arrives", "", "", "reject it|log it")
	f.Add("", "", "", "le jeton est expiré", "", "ne pas refuser")
	f.Add("", "", "x, then", "", "", "ne  x")
	f.Add("", "", "the landing gear button is depressed once",
		"the software detects that the landing gear does not lock into position",
		"the software", "sound an alarm")
	f.Add("", "Order Terms are not “Prepaid”", "an Order is shipped", "", "", "create an Invoice")
	f.Add("", "", "", "", "l’application", "ne pas facturer")
	f.Fuzz(func(t *testing.T, where, while, when, ifs, system, shall string) {
		raw := ears.Components{
			Where: split(where), While: split(while), When: when, If: ifs,
			System: system, Shall: does(split(shall)...),
		}
		c := raw.Normalized()
		if ears.Validate(&c) != nil {
			return
		}
		for _, lang := range []string{"en", "fr"} {
			for _, sentence := range []string{ears.Render(&c, lang), ears.RenderCapitals(&c, lang)} {
				parsed, err := ears.Parse(sentence)
				back := ears.WithActions(&parsed, ears.Actions(&c))
				if err != nil || !reflect.DeepEqual(back, c) {
					t.Fatalf("%s: Parse(%q) = %+v, %v; want %+v", lang, sentence, back, err, c)
				}
				if issue := ears.Check(ears.KindOf(&c), sentence); issue != "" {
					t.Fatalf("%s: Check(%s, %q): %s", lang, ears.KindOf(&c), sentence, issue)
				}
			}
		}
	})
}

// says are responses as a sentence states them: text, action unknown.
func says(texts ...string) []ears.Response {
	out := make([]ears.Response, len(texts))
	for i, text := range texts {
		out[i] = ears.Response{Text: text}
	}
	return out
}

// does are responses as the model writes them, each text's first word
// its action.
func does(texts ...string) []ears.Response {
	out := says(texts...)
	for i, text := range texts {
		if words := strings.Fields(text); len(words) > 0 {
			out[i].Action = words[0]
		}
	}
	return out
}

// split is a fuzzed list, "|"-separated; none for "".
func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "|")
}
