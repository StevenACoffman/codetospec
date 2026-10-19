// Shared fact model for the codetospec Go extractor (protocol facts v1).
package main

// Ref pins a fact to a file location, relative to --root, or to the shared
// root named Root (see SharedRoot).
type Ref struct {
	Path  string `json:"path"`
	Lines string `json:"lines"`
	Root  string `json:"root,omitempty"`
}

// Fact is one mechanically provable statement about the codebase.
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

// attrName is the name attribute of symbol, constant and table facts.
const attrName = "name"

const (
	schemaID = "codetospec/facts/v1"
	origin   = "go"
	certain  = "static"
)
