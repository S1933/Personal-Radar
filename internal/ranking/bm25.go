package ranking

import (
	"strings"
	"unicode"
)

// bm25 is a small Okapi-BM25 implementation used to score a single
// item against a fixed topic vocabulary. The full ranking pipeline
// keeps using the LLM stage; the heuristic scorer uses bm25 to give
// the relevance sub-score a real lexical signal instead of a
// substring count that double-weights repeated tokens.
//
// We index the topic vocabulary, not the corpus — the document is
// always one item, and the "corpus" is the set of topic keywords.
// This keeps the implementation in ~80 lines and avoids
// bootstrapping state at scorer call time.
type bm25 struct {
	k1 float64 // term-frequency saturation (BM25 default: 1.5)
	b  float64 // document-length penalty (BM25 default: 0.75)
}

func newBM25() *bm25 { return &bm25{k1: 1.5, b: 0.75} }

// tokenise splits on anything that is not a letter or digit, lowercases.
// It is deliberately tiny — no stopword list, no stemming. The
// topicKeywords in service.go already carry the stopword-equivalent
// information (each topic has 5-15 lemmas).
func tokenise(s string) []string {
	var out []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, strings.ToLower(buf.String()))
			buf.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			buf.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// score computes the BM25 score of a single document (an item's text)
// against one topic's keyword list. Returns 0..1: 0 for "no
// keyword matched", strictly positive otherwise.
//
// "Each keyword is a document" trick: with 1 document and 1 corpus
// the classic BM25 degenerates (idf is undefined). The keyword
// list acts as the vocabulary; the BM25 saturation on TF still
// gives a useful signal — a single occurrence scores ~0.6, repeated
// occurrences saturate towards 1.0. This gives a comparable score
// across topics without bootstrapping any state.
func (m *bm25) score(text string, keywords []string) float64 {
	if len(keywords) == 0 {
		return 0
	}
	tokens := tokenise(text)
	if len(tokens) == 0 {
		return 0
	}
	tf := map[string]int{}
	for _, t := range tokens {
		tf[t]++
	}
	var raw float64
	var matched int
	for _, kw := range keywords {
		kts := tokenise(kw)
		if len(kts) == 0 {
			continue
		}
		// Require all tokens of the keyword to be present
		// (AND semantics). Otherwise a topic like "go" would
		// match any document containing the letter sequence.
		allPresent := true
		for _, kt := range kts {
			if tf[kt] == 0 {
				allPresent = false
				break
			}
		}
		if !allPresent {
			continue
		}
		matched++
		// Sum BM25-saturated TF contributions.
		for _, kt := range kts {
			f := float64(tf[kt])
			numer := f * (m.k1 + 1)
			denom := f + m.k1
			raw += numer / denom
		}
	}
	if matched == 0 {
		return 0
	}
	// Normalise over the matched keyword groups, not over the
	// full vocabulary. A previous version divided by
	// len(keywords), which meant a topic with 12 lemmas could
	// never score higher than a topic with 1 lemma — the rich
	// topic always lost. The intent is the opposite: a topic
	// whose lemmas are densely matched should score high, and a
	// small coverage bonus rewards items that touch several
	// lemmas of the same topic.
	//
	// raw / matched is the average saturation across the
	// groups that fired; (0.6 + 0.4*coverage) shifts that up
	// when the document covers more of the topic vocabulary.
	coverage := float64(matched) / float64(len(keywords))
	score := (raw / float64(matched)) * (0.6 + 0.4*coverage)
	if score > 1 {
		score = 1
	}
	return score
}
