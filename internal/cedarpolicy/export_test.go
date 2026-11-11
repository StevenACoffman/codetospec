package cedarpolicy

import (
	"github.com/StevenACoffman/codetospec/internal/policy"
	"github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"
)

// Request exposes the loaded request to the agreement test, which asks
// cedar-go each check's truth on it.
func Request(
	d *Decider,
	action, resourceType string,
	p *policy.Persona,
	facts []Fact,
	mask int,
) (types.EntityMap, cedar.Request) {
	return d.request(action, resourceType, p, facts, mask)
}
