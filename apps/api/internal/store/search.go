package store

import "strings"

const (
	SearchHighlightStart = '\ufdd0'
	SearchHighlightEnd   = '\ufdd1'
)

func ParseSearchSnippet(marked string) (string, []SearchHighlight) {
	var snippet strings.Builder
	highlights := []SearchHighlight{}
	highlightStart := -1
	position := 0

	for _, char := range marked {
		switch char {
		case SearchHighlightStart:
			if highlightStart < 0 {
				highlightStart = position
			}
		case SearchHighlightEnd:
			if highlightStart >= 0 && highlightStart < position {
				highlights = append(highlights, SearchHighlight{Start: highlightStart, End: position})
			}
			highlightStart = -1
		default:
			snippet.WriteRune(char)
			position++
		}
	}
	if highlightStart >= 0 && highlightStart < position {
		highlights = append(highlights, SearchHighlight{Start: highlightStart, End: position})
	}
	return snippet.String(), highlights
}
