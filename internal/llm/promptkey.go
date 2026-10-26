package llm

import (
	"crypto/sha256"
	"encoding/hex"
)

// promptKeyLen is the number of hex characters kept from the digest, matching
// the 16-character ids used by every other content hash in the pipeline.
const promptKeyLen = 16

// PromptKey identifies everything besides the input content that determines
// an LLM reply: the model and the prompt templates (and any other knob, such
// as the output language). Phases fold it into their cache keys so editing a
// prompt or switching models invalidates cached replies automatically — no
// hand-bumped version constant to forget.
//
// Requires: nothing; parts may be empty.
// Ensures:  a 16-char hex digest; equal inputs give equal keys, and parts
// are NUL-separated so ("ab", "c") and ("a", "bc") give different keys.
func PromptKey(model string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(model))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:promptKeyLen]
}
