package d2

import (
	"fmt"
	"math"
	"strconv"
)

// Palette is the colours of one rendering, each an opaque "#rrggbb".
// Light and Dark are the only ones: the diagram-design skill's default
// skin (style-guide.md's semantic roles, light and dark columns), its
// translucent roles composited over Paper. Functions, not variables:
// the package keeps no state.
type Palette struct {
	Paper, Ink, Muted, Soft, Rule, Accent, AccentTint string
	// Node is a step's fill, Group a container's, Store and Input their
	// roles' (ink and muted at low opacity), External and ExternalStroke
	// an external node's.
	Node, Group, Store, Input, External, ExternalStroke string
}

// The opacities the skill gives each translucent role (style-guide.md,
// SKILL.md's node treatments): hairlines, containers and external nodes,
// stores, input nodes, external strokes, and accent-tint in each skin.
const (
	opacityRule      = 0.12
	opacityGroup     = 0.03
	opacityStore     = 0.05
	opacityInput     = 0.10
	opacityExtStroke = 0.30
	opacityTintLight = 0.08
	opacityTintDark  = 0.10
)

// Light is the light rendering's palette.
func Light() Palette {
	return skin(&skinTokens{
		paper: "#f5f5f5", ink: "#2d3142", muted: "#4f5d75", soft: "#7a8399",
		accent: "#eb6c36", node: "#ffffff", accentTint: opacityTintLight,
	})
}

// Dark is the dark rendering's palette.
func Dark() Palette {
	return skin(&skinTokens{
		paper: "#2d3142", ink: "#f5f5f5", muted: "#bfc0c0", soft: "#8e98ac",
		accent: "#f08a59", node: "#393e53", accentTint: opacityTintDark,
	})
}

// skinTokens are one column of the style guide's semantic roles; node is
// a step's fill (white on light paper, paper-2 on dark).
type skinTokens struct {
	paper, ink, muted, soft, accent, node string
	accentTint                            float64 // accent-tint's opacity
}

// skin composites t's translucent roles over its paper.
func skin(t *skinTokens) Palette {
	return Palette{
		Paper: t.paper, Ink: t.ink, Muted: t.muted, Soft: t.soft, Accent: t.accent,
		Rule:           over(t.ink, opacityRule, t.paper),
		AccentTint:     over(t.accent, t.accentTint, t.paper),
		Node:           t.node,
		Group:          over(t.ink, opacityGroup, t.paper),
		Store:          over(t.ink, opacityStore, t.paper),
		Input:          over(t.muted, opacityInput, t.paper),
		External:       over(t.ink, opacityGroup, t.paper),
		ExternalStroke: over(t.ink, opacityExtStroke, t.paper),
	}
}

// over is colour fg at opacity alpha painted on bg, both "#rrggbb".
func over(fg string, alpha float64, bg string) string {
	f, g := rgb(fg), rgb(bg)
	var out [3]int
	for i := range out {
		out[i] = int(math.Round(float64(f[i])*alpha + float64(g[i])*(1-alpha)))
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2])
}

// rgb is a "#rrggbb" colour's channels; the palette's literals are all
// well formed, so a malformed one is a programming error.
func rgb(hex string) [3]int {
	var out [3]int
	for i := range out {
		v, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			panic(fmt.Sprintf("d2: bad colour %q", hex))
		}
		out[i] = int(v)
	}
	return out
}
