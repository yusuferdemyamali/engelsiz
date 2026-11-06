// HTML plaintext stripping for offline retrieval. The stdlib-only scanner
// below drops tags, comments, script/style payloads and collapses
// whitespace, then decodes entities. No heavyweight parser is introduced:
// article bodies are already bounded by the caller and excerpts are capped
// downstream, so a purpose-built scanner is sufficient and auditable.
package rag

import (
	"html"
	"strings"
)

// StripHTMLText converts bounded HTML/plain input to collapsed plaintext.
func StripHTMLText(data []byte) string {
	s := string(data)
	var b strings.Builder
	b.Grow(len(s))
	i, n := 0, len(s)
	skip := ""
	for i < n {
		if skip != "" {
			rest := s[i:]
			end := indexFold(rest, "</"+skip)
			if end < 0 {
				break
			}
			i += end + len(skip) + 2
			if j := strings.IndexByte(s[i:], '>'); j >= 0 {
				i += j + 1
			}
			skip = ""
			b.WriteByte(' ')
			continue
		}
		if s[i] == '<' {
			if strings.HasPrefix(s[i:], "<!--") {
				if end := strings.Index(s[i+4:], "-->"); end >= 0 {
					i += 4 + end + 3
				} else {
					break
				}
				b.WriteByte(' ')
				continue
			}
			j := strings.IndexByte(s[i:], '>')
			if j < 0 {
				break
			}
			if name := tagName(s[i+1 : i+j]); name == "script" || name == "style" {
				if !strings.HasSuffix(strings.TrimSpace(s[i+1:i+j]), "/") {
					skip = name
				}
			}
			i += j + 1
			b.WriteByte(' ')
			continue
		}
		j := strings.IndexByte(s[i:], '<')
		if j < 0 {
			j = n - i
		}
		b.WriteString(s[i : i+j])
		i += j
	}
	return strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
}

func tagName(tag string) string {
	tag = strings.TrimSpace(tag)
	tag = strings.TrimPrefix(tag, "/")
	for i := 0; i < len(tag); i++ {
		if tag[i] == ' ' || tag[i] == '\t' || tag[i] == '\n' || tag[i] == '\r' || tag[i] == '/' {
			tag = tag[:i]
			break
		}
	}
	return strings.ToLower(tag)
}

func indexFold(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	lower := strings.ToLower(s)
	return strings.Index(lower, sub)
}

// Excerpt returns at most limit runes centered on the first term hit; it
// falls back to the leading run when nothing matches. Terms shorter than 3
// runes never anchor the window.
func Excerpt(text string, terms []string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) <= limit {
		return string(runes)
	}
	lower := strings.ToLower(string(runes))
	best := -1
	for _, t := range terms {
		t = strings.TrimSpace(t)
		if len([]rune(t)) < 3 {
			continue
		}
		if idx := strings.Index(lower, strings.ToLower(t)); idx >= 0 && (best < 0 || idx < best) {
			best = idx
		}
	}
	if best < 0 {
		return string(runes[:limit])
	}
	runeIdx := len([]rune(lower[:best]))
	start := runeIdx - limit/4
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(runes) {
		end = len(runes)
		start = end - limit
		if start < 0 {
			start = 0
		}
	}
	return string(runes[start:end])
}
