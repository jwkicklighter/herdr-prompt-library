package placeholders

import (
	"strings"
	"time"
)

type Values struct {
	HerdrTabID             string
	HerdrPluginContextJSON string
	Directory              string
}

type Expander struct {
	Values Values
	Now    func() time.Time
}

// Expand replaces supported placeholders using one clock snapshot.
func (e Expander) Expand(text string) string {
	now := e.Now()
	replacements := map[string]string{
		"herdr_tab_id":              e.Values.HerdrTabID,
		"herdr_plugin_context_json": e.Values.HerdrPluginContextJSON,
		"today":                     now.Format("2006-01-02"),
		"now":                       now.Format(time.RFC3339),
		"directory":                 e.Values.Directory,
	}

	var expanded strings.Builder
	for offset := 0; ; {
		relativeStart := strings.Index(text[offset:], "{{")
		if relativeStart < 0 {
			expanded.WriteString(text[offset:])
			break
		}
		start := offset + relativeStart
		expanded.WriteString(text[offset:start])

		relativeEnd := strings.Index(text[start+2:], "}}")
		if relativeEnd < 0 {
			expanded.WriteString(text[start:])
			break
		}
		end := start + 2 + relativeEnd
		token := text[start : end+2]
		name := strings.Trim(text[start+2:end], " \t\n\r\v\f")
		replacement, supported := replacements[name]
		malformedBoundary := (start > 0 && text[start-1] == '{') ||
			(end+2 < len(text) && text[end+2] == '}')
		if supported && !malformedBoundary {
			expanded.WriteString(replacement)
		} else {
			expanded.WriteString(token)
		}
		offset = end + 2
	}
	return expanded.String()
}
