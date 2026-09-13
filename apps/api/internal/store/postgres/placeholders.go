package postgres

import (
	"fmt"
	"strings"
)

func pgPlaceholders(n, start int) string {
	var b strings.Builder
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "$%d", start+i)
	}
	return b.String()
}
