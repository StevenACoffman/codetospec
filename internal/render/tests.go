package render

import (
	"fmt"
	"strings"

	"github.com/StevenACoffman/codetospec/internal/graph"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/testreach"
)

// testedLine counts the rules a test reaches among those whose test reach
// is known; empty when none is (the run had no test facts). The rules a
// test reaches only through an interface are counted apart.
func testedLine(nodes []graph.Node, text *locale.Text) string {
	known, reached, viaInterface := 0, 0, 0
	for i := range nodes {
		tests := nodes[i].Extra[graph.ExtraTests]
		if nodes[i].Type != graph.TypeRule || tests == "" {
			continue
		}
		known++
		switch {
		case tests != "0":
			reached++
		case nodes[i].Extra[graph.ExtraTestsViaInterface] != "":
			viaInterface++
		}
	}
	if known == 0 {
		return ""
	}
	line := fmt.Sprintf(text.TestedFmt, reached, known, testreach.MaxDepth)
	if viaInterface > 0 {
		line += fmt.Sprintf(text.TestedViaInterfaceFmt, viaInterface)
	}
	return line + "\n"
}

// executedLine counts the rules whose cited code a profiled test run
// executed, among those it has a record of; empty when it has none (no
// coverage profile).
func executedLine(nodes []graph.Node, text *locale.Text) string {
	known, executed := 0, 0
	for i := range nodes {
		ran := nodes[i].Extra[graph.ExtraExecuted]
		if nodes[i].Type != graph.TypeRule || ran == "" {
			continue
		}
		known++
		if !strings.HasPrefix(ran, "0/") {
			executed++
		}
	}
	if known == 0 {
		return ""
	}
	return fmt.Sprintf(text.ExecutedFmt+"\n", executed, known)
}
