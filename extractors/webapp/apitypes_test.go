package main

import (
	"testing"
	"testing/fstest"
)

// schemaA is the fixture file declaring most of the types TestAPITypeFacts
// reads.
const schemaA = "a.graphql"

// TestAPITypeFacts: a federated entity type is owned where the service
// declares it, a reference where it only extends it; deprecated when every
// field it declares is; a type without @key is no concept seed.
func TestAPITypeFacts(t *testing.T) {
	fsys := fstest.MapFS{
		"gqlgen.yml": {Data: []byte("schema:\n  - \"*.graphql\"\n  - ../../pkg/shared.graphql\n")},
		schemaA: {Data: []byte(`type Assignment @key(fields: "id") {
  id: ID!
}
extend type Classroom @key(fields: "cid") {
  cid: ID! @external
}
type Payload {
  ok: Boolean
}
type Legacy @key(fields: "id") {
  id: ID! @deprecated(reason: "gone")
  name: String @deprecated
}
`)},
		"b.graphql": {Data: []byte(`extend type Assignment @key(fields: "id") {
  title: String
}
`)},
	}
	type want struct{ owned, key, deprecated, path string }
	cases := map[string]want{
		"api_type.Assignment": {owned: goTrue, key: "id", path: schemaA},
		"api_type.Classroom":  {owned: "false", key: "cid", path: schemaA},
		"api_type.Legacy":     {owned: goTrue, key: "id", deprecated: goTrue, path: schemaA},
	}
	facts := apiTypeFacts(fsys)
	if len(facts) != len(cases) {
		t.Fatalf("facts = %+v, want %d", facts, len(cases))
	}
	for _, f := range facts {
		t.Run(f.ID, func(t *testing.T) {
			w, ok := cases[f.ID]
			if !ok {
				t.Fatalf("unexpected fact %s", f.ID)
			}
			got := want{
				f.Attrs[attrOwned],
				f.Attrs[attrKey],
				f.Attrs[attrDeprecated],
				f.Source.Path,
			}
			if got != w {
				t.Errorf("got %+v, want %+v", got, w)
			}
		})
	}
}
