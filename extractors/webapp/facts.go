// Shared fact model for the codetospec webapp extractor (protocol facts v1).
// The kind and attribute names mirror codetospec's internal/extract contract.
package main

// Ref pins a fact to a file location, relative to --root.
type Ref struct {
	Path  string `json:"path"`
	Lines string `json:"lines"`
}

// Fact is one mechanically derived statement about the codebase.
type Fact struct {
	Kind      string            `json:"kind"`
	ID        string            `json:"id"`
	Attrs     map[string]string `json:"attrs"`
	Source    Ref               `json:"source"`
	Origin    string            `json:"origin"`
	Certainty string            `json:"certainty"`
}

// Envelope is the JSON payload written to stdout.
type Envelope struct {
	Schema string `json:"schema"`
	Facts  []Fact `json:"facts"`
}

const (
	schemaID = "codetospec/facts/v1"
	origin   = "webapp"
	certain  = "static"

	kindRule   = "rule"
	kindDomain = "domain"

	attrSlug        = "slug"
	attrTitle       = "title"
	attrRequirement = "requirement"
	attrEARS        = "ears"
	attrNature      = "nature"
	attrDomain      = "domain"
)
