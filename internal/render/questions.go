package render

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/StevenACoffman/codetospec/internal/atomicfs"
	"github.com/StevenACoffman/codetospec/internal/locale"
	"github.com/StevenACoffman/codetospec/internal/questions"
)

// QuestionsFile lists every question of the run, with its id: the id a
// person writes in questions.ResolutionsFile to resolve it.
const QuestionsFile = "questions.json"

// writeQuestions writes the ledger to QuestionsFile.
func writeQuestions(outDir string, ledger *questions.Ledger) error {
	data, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", QuestionsFile, err)
	}
	if err = atomicfs.WriteFile(
		filepath.Join(outDir, QuestionsFile),
		append(data, '\n'),
	); err != nil {
		return fmt.Errorf("write %s: %w", QuestionsFile, err)
	}
	return nil
}

// resolutionsLine counts the questions and those a person resolved, the
// resolutions that no longer match a question, and the targets
// (unusedTargets) that no longer match a non-functional rule; empty when
// the run raised no question and nothing is unused.
func resolutionsLine(ledger *questions.Ledger, unusedTargets []string, text *locale.Text) string {
	if len(ledger.Questions) == 0 && len(ledger.Unused) == 0 && len(unusedTargets) == 0 {
		return ""
	}
	line := fmt.Sprintf(text.QuestionsFmt, len(ledger.Questions), ledger.Count())
	if len(ledger.Unused) > 0 {
		line += fmt.Sprintf(text.UnusedResolutionsFmt, len(ledger.Unused))
	}
	if len(unusedTargets) > 0 {
		line += fmt.Sprintf(text.UnusedTargetsFmt, len(unusedTargets))
	}
	return line + "\n"
}
