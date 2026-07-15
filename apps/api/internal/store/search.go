package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	SearchHighlightStart = '\ufdd0'
	SearchHighlightEnd   = '\ufdd1'
)

type SearchMarkers struct {
	Start string
	End   string
}

func NewSearchMarkers() (SearchMarkers, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return SearchMarkers{}, err
	}
	suffix := hex.EncodeToString(nonce[:])
	return SearchMarkers{
		Start: string(SearchHighlightStart) + "start-" + suffix + string(SearchHighlightEnd),
		End:   string(SearchHighlightStart) + "end-" + suffix + string(SearchHighlightEnd),
	}, nil
}

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

func ParseSearchSnippetWithMarkers(marked string, markers SearchMarkers) (string, []SearchHighlight, error) {
	if markers.Start == "" || markers.End == "" || markers.Start == markers.End {
		return "", nil, errors.New("invalid search snippet markers")
	}

	var snippet strings.Builder
	snippet.Grow(len(marked))
	highlights := make([]SearchHighlight, 0, 2)
	position := 0
	remaining := marked

	for {
		startIndex := strings.Index(remaining, markers.Start)
		endIndex := strings.Index(remaining, markers.End)
		if startIndex < 0 {
			if endIndex >= 0 {
				return "", nil, errors.New("malformed search snippet markers")
			}
			snippet.WriteString(remaining)
			break
		}
		if endIndex >= 0 && endIndex < startIndex {
			return "", nil, errors.New("malformed search snippet markers")
		}

		prefix := remaining[:startIndex]
		snippet.WriteString(prefix)
		position += utf8.RuneCountInString(prefix)
		remaining = remaining[startIndex+len(markers.Start):]

		endIndex = strings.Index(remaining, markers.End)
		if endIndex < 0 {
			return "", nil, errors.New("malformed search snippet markers")
		}
		if nestedStart := strings.Index(remaining, markers.Start); nestedStart >= 0 && nestedStart < endIndex {
			return "", nil, errors.New("malformed search snippet markers")
		}

		match := remaining[:endIndex]
		startPosition := position
		snippet.WriteString(match)
		position += utf8.RuneCountInString(match)
		if position > startPosition {
			highlights = append(highlights, SearchHighlight{Start: startPosition, End: position})
		}
		remaining = remaining[endIndex+len(markers.End):]
	}

	return snippet.String(), highlights, nil
}
