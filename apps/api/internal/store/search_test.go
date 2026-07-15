package store

import "testing"

func TestParseSearchSnippetWithMarkersPreservesLiteralLegacyMarkers(t *testing.T) {
	t.Parallel()
	markers := SearchMarkers{Start: "<search-start-123>", End: "<search-end-123>"}
	literal := "\ufdd0 user text \ufdd1"
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
