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

func TestParseSearchSnippetWithMarkersPreservesLiteralLegacyMarkers(t *testing.T) {
	t.Parallel()
	markers := SearchMarkers{Start: "<search-start-123>", End: "<search-end-123>"}
	literal := string(SearchHighlightStart) + " user text " + string(SearchHighlightEnd)
	marked := literal + " before " + markers.Start + "needle" + markers.End + " after"

	snippet, highlights, err := ParseSearchSnippetWithMarkers(marked, markers)
	if err != nil {
		t.Fatal(err)
	}
	if snippet != literal+" before needle after" {
		t.Fatalf("unexpected snippet %q", snippet)
	}
	if len(highlights) != 1 || highlights[0] != (SearchHighlight{Start: 21, End: 27}) {
		t.Fatalf("unexpected highlights %#v", highlights)
	}
}

func TestParseSearchSnippetWithMarkersRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	markers := SearchMarkers{Start: "<start>", End: "<end>"}
	for _, marked := range []string{
		"orphan <end>",
		"missing <start>end",
		"<start>nested <start>match<end><end>",
	} {
		if _, _, err := ParseSearchSnippetWithMarkers(marked, markers); err == nil {
			t.Fatalf("expected malformed marker error for %q", marked)
		}
	}
}

func TestNewSearchMarkersAreDistinct(t *testing.T) {
	t.Parallel()
	first, err := NewSearchMarkers()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewSearchMarkers()
	if err != nil {
		t.Fatal(err)
	}
	if first.Start == first.End || second.Start == second.End || first == second {
		t.Fatalf("unexpected markers: first=%#v second=%#v", first, second)
	}
}
