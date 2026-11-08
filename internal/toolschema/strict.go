package toolschema

import (
	"strconv"
	"strings"
)

// probes is the corpus a strict string's pattern must reject: option
// injection (-x, --a=b), path traversal (.., /), whitespace and line breaks,
// and shell metacharacters ($(, ;, |, backtick).
var probes = []string{"-x", "--a=b", "..", "/", " ", "\n", "$(", ";", "|", "`"}

// Probes returns a copy of the probe corpus.
func Probes() []string {
	return append([]string(nil), probes...)
}

// probeList renders the corpus for messages.
func probeList() string {
	parts := make([]string, len(probes))
	for i, p := range probes {
		parts[i] = strconv.Quote(p)
	}
	return strings.Join(parts, ", ")
}
