package main

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// isOrgPath reports whether path is an Emacs Org-mode file, by its extension.
func isOrgPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".org")
}

// isOrg reports whether d is an Org-mode document.
func isOrg(d *document) bool { return d.path != "" && isOrgPath(d.path) }

// orgToMarkdown converts Org-mode source into Markdown for the Reading view,
// keeping a 1:1 line mapping so scroll sync and passage restoration land on
// the matching lines of the Org source.
func orgToMarkdown(source []byte) []byte {
	s := string(source)
	trailingNL := strings.HasSuffix(s, "\n")
	if trailingNL {
		s = s[:len(s)-1]
	}
	rawLines := strings.Split(s, "\n")
	out := make([]string, len(rawLines))

	type blockKind uint8
	const (
		blockNone blockKind = iota
		blockCode
		blockQuote
		blockComment
		blockDrawer
	)
	block := blockNone
	tableState := 0 // 0: outside, 1: header row seen, 2: separator seen

	for i, raw := range rawLines {
		line := strings.TrimSuffix(raw, "\r")
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		switch block {
		case blockCode:
			if strings.HasPrefix(lower, "#+end_src") ||
				strings.HasPrefix(lower, "#+end_example") ||
				strings.HasPrefix(lower, "#+end_verbatim") {
				out[i] = orgIndent(line) + "```"
				block = blockNone
			} else {
				out[i] = orgUnescapeCodeLine(line)
			}
			continue
		case blockQuote:
			if strings.HasPrefix(lower, "#+end_quote") {
				out[i] = ""
				block = blockNone
			} else if trimmed == "" {
				out[i] = ">"
			} else {
				out[i] = "> " + orgInline(trimmed)
			}
			continue
		case blockComment:
			if strings.HasPrefix(lower, "#+end_comment") {
				block = blockNone
			}
			out[i] = ""
			continue
		case blockDrawer:
			if lower == ":end:" {
				block = blockNone
			}
			out[i] = ""
			continue
		}

		// Block openers.
		if rest, ok := orgPrefixFold(trimmed, "#+begin_src"); ok {
			lang := ""
			if fields := strings.Fields(rest); len(fields) > 0 {
				lang = fields[0]
			}
			out[i] = orgIndent(line) + "```" + lang
			block = blockCode
			tableState = 0
			continue
		}
		if _, ok := orgPrefixFold(trimmed, "#+begin_example"); ok {
			out[i] = orgIndent(line) + "```"
			block = blockCode
			tableState = 0
			continue
		}
		if _, ok := orgPrefixFold(trimmed, "#+begin_verbatim"); ok {
			out[i] = orgIndent(line) + "```"
			block = blockCode
			tableState = 0
			continue
		}
		if _, ok := orgPrefixFold(trimmed, "#+begin_quote"); ok {
			out[i] = ""
			block = blockQuote
			tableState = 0
			continue
		}
		if _, ok := orgPrefixFold(trimmed, "#+begin_comment"); ok {
			out[i] = ""
			block = blockComment
			tableState = 0
			continue
		}
		if lower == ":properties:" || lower == ":logbook:" {
			out[i] = ""
			block = blockDrawer
			tableState = 0
			continue
		}

		// Keywords and comments.
		if strings.HasPrefix(trimmed, "#+") {
			tableState = 0
			if val, ok := orgKeyword(trimmed, "title"); ok {
				out[i] = "# " + orgInline(val)
			} else if val, ok := orgKeyword(trimmed, "subtitle"); ok {
				out[i] = "*" + orgInline(val) + "*"
			} else {
				out[i] = ""
			}
			continue
		}
		if trimmed == "#" || strings.HasPrefix(trimmed, "# ") {
			tableState = 0
			out[i] = ""
			continue
		}

		// Headings: one or more leading * followed by space.
		if stars, rest, ok := orgHeading(line); ok {
			tableState = 0
			out[i] = strings.Repeat("#", min(stars, 6)) + " " + orgInline(rest)
			continue
		}

		// Horizontal rule: 5 or more hyphens alone on a line.
		if isOrgRule(trimmed) {
			tableState = 0
			out[i] = "---"
			continue
		}

		// Tables: | col | col | with |---+---| separator lines.
		if strings.HasPrefix(trimmed, "|") {
			if isOrgTableSep(trimmed) {
				switch tableState {
				case 1:
					out[i] = strings.ReplaceAll(line, "+", "|")
					tableState = 2
				default:
					// Leading or trailing border rule: omit to keep GFM valid.
					out[i] = ""
				}
			} else {
				if tableState == 0 {
					tableState = 1
				}
				out[i] = orgInline(line)
			}
			continue
		}
		tableState = 0

		// Regular lines (paragraphs, lists, checkboxes).
		out[i] = orgInline(line)
	}

	var b strings.Builder
	b.Grow(len(source) + 64)
	for i, l := range out {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l)
	}
	if trailingNL {
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func orgIndent(line string) string {
	n := 0
	for n < len(line) && (line[n] == ' ' || line[n] == '\t') {
		n++
	}
	return line[:n]
}

// orgUnescapeCodeLine removes Org's leading comma escape before * or #+ inside
// source/example blocks.
func orgUnescapeCodeLine(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i < len(line) && line[i] == ',' && i+1 < len(line) && (line[i+1] == '*' || strings.HasPrefix(line[i+1:], "#+")) {
		return line[:i] + line[i+1:]
	}
	return line
}

func orgPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return "", false
	}
	rest := s[len(prefix):]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

func orgKeyword(trimmed, name string) (string, bool) {
	prefix := "#+" + name + ":"
	if len(trimmed) < len(prefix) || !strings.EqualFold(trimmed[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(trimmed[len(prefix):]), true
}

func orgHeading(line string) (int, string, bool) {
	n := 0
	for n < len(line) && line[n] == '*' {
		n++
	}
	if n == 0 || n >= len(line) || line[n] != ' ' {
		return 0, "", false
	}
	return n, strings.TrimSpace(line[n+1:]), true
}

func isOrgRule(trimmed string) bool {
	if len(trimmed) < 5 {
		return false
	}
	for i := 0; i < len(trimmed); i++ {
		if trimmed[i] != '-' {
			return false
		}
	}
	return true
}

func isOrgTableSep(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	hasDash := false
	for i := 0; i < len(trimmed); i++ {
		switch trimmed[i] {
		case '-':
			hasDash = true
		case '|', '+', ' ', '\t':
		default:
			return false
		}
	}
	return hasDash
}

func isOrgImage(target string) bool {
	if q := strings.IndexAny(target, "?#"); q >= 0 {
		target = target[:q]
	}
	switch strings.ToLower(filepath.Ext(target)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp", ".ico", ".avif":
		return true
	}
	return false
}

// orgInline converts inline Org-mode markup (links, images, verbatim/code,
// emphasis, and LaTeX math delimiters) to Markdown in a single left-to-right
// scan so URLs and code spans protect their contents.
func orgInline(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	i := 0
	for i < len(s) {
		// Org links and images: [[target][desc]] or [[target]].
		if s[i] == '[' && i+1 < len(s) && s[i+1] == '[' {
			if out, end, ok := orgParseLink(s, i); ok {
				b.WriteString(out)
				i = end
				continue
			}
		}

		// Org LaTeX math delimiters: \(...\) and \[...\].
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '(' || s[i+1] == '[') {
			closeSeq := `\)`
			fence := "$"
			if s[i+1] == '[' {
				closeSeq = `\]`
				fence = "$$"
			}
			if end := strings.Index(s[i+2:], closeSeq); end > 0 {
				body := strings.TrimSpace(s[i+2 : i+2+end])
				if body != "" {
					b.WriteString(fence)
					b.WriteString(body)
					b.WriteString(fence)
					i = i + 2 + end + 2
					continue
				}
			}
		}

		// Inline emphasis and verbatim/code markers.
		switch s[i] {
		case '=', '~', '*', '/', '+', '_':
			if out, end, ok := orgParseMark(s, i); ok {
				b.WriteString(out)
				i = end
				continue
			}
		}

		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func orgParseLink(s string, start int) (string, int, bool) {
	end := strings.Index(s[start+2:], "]]")
	if end < 0 {
		return "", 0, false
	}
	inner := s[start+2 : start+2+end]
	next := start + 2 + end + 2

	target, desc, hasDesc := strings.Cut(inner, "][")
	target = strings.TrimSpace(target)
	if target == "" {
		return "", 0, false
	}
	if trimmed, ok := strings.CutPrefix(target, "file:"); ok {
		target = trimmed
	}
	if !hasDesc {
		if isOrgImage(target) {
			return "![](" + target + ")", next, true
		}
		return "[" + target + "](" + target + ")", next, true
	}
	return "[" + orgInline(desc) + "](" + target + ")", next, true
}

func orgParseMark(s string, start int) (string, int, bool) {
	mark := s[start]
	// Pre-char check: start of string, whitespace, opening punctuation, or non-ASCII (CJK).
	if start > 0 {
		prev, _ := utf8.DecodeLastRuneInString(s[:start])
		if !isOrgPre(prev) {
			return "", 0, false
		}
	}
	if start+1 >= len(s) {
		return "", 0, false
	}
	first, _ := utf8.DecodeRuneInString(s[start+1:])
	if unicode.IsSpace(first) || (first == rune(mark) && mark != '=') {
		return "", 0, false
	}

	// Find the matching closing mark on this line.
	for j := start + 1; j < len(s); j++ {
		if s[j] != mark {
			continue
		}
		// Must not be preceded by whitespace.
		before, _ := utf8.DecodeLastRuneInString(s[:j])
		if unicode.IsSpace(before) {
			continue
		}
		// Must be at end of string or followed by a valid post-char.
		if j+1 < len(s) {
			after, _ := utf8.DecodeRuneInString(s[j+1:])
			if !isOrgPost(after) {
				continue
			}
		}
		body := s[start+1 : j]
		if body == "" {
			return "", 0, false
		}
		switch mark {
		case '=', '~':
			return orgCodeSpan(body), j + 1, true
		case '*':
			return "**" + orgInline(body) + "**", j + 1, true
		case '/':
			return "*" + orgInline(body) + "*", j + 1, true
		case '+':
			return "~~" + orgInline(body) + "~~", j + 1, true
		case '_':
			return "<u>" + orgInline(body) + "</u>", j + 1, true
		}
	}
	return "", 0, false
}

func isOrgPre(r rune) bool {
	if r >= utf8.RuneSelf {
		return true
	}
	return unicode.IsSpace(r) || strings.ContainsRune("({'\"-[", r)
}

func isOrgPost(r rune) bool {
	if r >= utf8.RuneSelf {
		return true
	}
	return unicode.IsSpace(r) || strings.ContainsRune("-.,;:!?'\")}]", r)
}

func orgCodeSpan(body string) string {
	if !strings.ContainsRune(body, '`') {
		return "`" + body + "`"
	}
	pad := ""
	if strings.HasPrefix(body, "`") || strings.HasSuffix(body, "`") {
		pad = " "
	}
	return "``" + pad + body + pad + "``"
}
