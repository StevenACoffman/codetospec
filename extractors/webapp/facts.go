// Shared fact model for the codetospec webapp extractor (protocol facts v1).
// The kind and attribute names mirror codetospec's internal/extract contract.
package main

// Ref pins a fact to a file location, relative to --root.
type Ref struct {
	Path  string `json:"path"`
	Lines string `json:"lines"`
	// Root names the tree Path is in when it is not the service: pkgRoot
	// for webapp's shared pkg/ (codetospec --ref-root pkg=<checkout>/pkg).
	Root string `json:"root,omitempty"`
}

// pkgRoot is the citation root of webapp's shared code: paths under it
// are relative to the checkout's pkg/ (sharedDir).
const pkgRoot = "pkg"

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

	kindRule       = "rule"
	kindDomain     = "domain"
	kindConstant   = "constant"
	kindRoute      = "route"
	kindTable      = "table"
	kindDoc        = "doc"
	kindReach      = "reach" // shared code the service reaches (codetospec's extract.KindReach)
	kindDependency = "dependency"

	attrSlug        = "slug"
	attrTitle       = "title"
	attrRequirement = "requirement"
	attrEARS        = "ears"
	attrNature      = "nature"
	attrDomain      = "domain"
	attrName        = "name"
	attrValue       = "value"
	attrRoute       = "route"
	attrMethod      = "method"
	attrPath        = "path"
	attrHandler     = "handler"
	attrHandlerPath = "handler_path"
	attrColumns     = "columns"
	attrSymbol      = "symbol" // the reached declaration, package-qualified
	// attrDescription, on a table fact, is the entity's authored
	// description: its struct's doc comment (codetospec's
	// extract.AttrDescription).
	attrDescription = "description"
	attrText        = "text"
	attrService     = "service"
	attrOperation   = "operation"
	attrAs          = "as" // a dependency's authority: asServiceAdmin, asUser
)

// Values of attrAs (codetospec's extract.AsServiceAdmin, extract.AsUser).
const (
	asServiceAdmin = "service-admin"
	asUser         = "user"
)
