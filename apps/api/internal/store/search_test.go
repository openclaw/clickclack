package store

import "testing"

func TestParseSearchSnippetUsesUnicodeCodePointOffsets(t *testing.T) {
	t.Parallel()
	marked := "before 🦞 " + string(SearchHighlightStart) + "needles" + string(SearchHighlightEnd) + " after"
	snippet, highlights := ParseSearchSnippet(marked)
	if snippet != "before 🦞 needles after" {
		t.Fatalf("unexpected snippet %q", snippet)
	}
	if len(highlights) != 1 || highlights[0].Start != 9 || highlights[0].End != 16 {
		t.Fatalf("unexpected highlights %#v", highlights)
	}
}

func TestParseSearchSnippetClosesUnterminatedHighlight(t *testing.T) {
	t.Parallel()
	marked := "start " + string(SearchHighlightStart) + "match"
	snippet, highlights := ParseSearchSnippet(marked)
	if snippet != "start match" || len(highlights) != 1 || highlights[0] != (SearchHighlight{Start: 6, End: 11}) {
		t.Fatalf("unexpected parsed snippet %q %#v", snippet, highlights)
	}
}
