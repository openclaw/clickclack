package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

const searchHighlightBoundary = "\ufdd0"

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
		Start: searchHighlightBoundary + "start-" + suffix + searchHighlightBoundary,
		End:   searchHighlightBoundary + "end-" + suffix + searchHighlightBoundary,
	}, nil
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
