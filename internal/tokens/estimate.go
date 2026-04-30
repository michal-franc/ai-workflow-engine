// Package tokens provides a coarse token-count approximation used by the
// workflow stats view.
//
// Estimate uses len(s)/4, the well-known back-of-envelope proxy for
// Claude/GPT-family BPE tokenization. It is intentionally cheap and tokenizer-
// free; numbers should be read as "approximate tokens", not exact counts for
// any specific model.
package tokens

// Estimate returns an approximate token count for s.
func Estimate(s string) int {
	return len(s) / 4
}
