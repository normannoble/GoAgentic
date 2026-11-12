package dash

import (
	"html"
	"html/template"
	"regexp"
	"strings"
)

// paneMarkdown renders an agent's message for the Tasks chat pane with
// real blocks: paragraphs, ordered and unordered lists, headings, fenced
// code and tables, and the inline marks of chatInline plus italics. Like
// chatMarkdown, every piece of the message is escaped before any tag is
// added, so no HTML from the transcript reaches the page. The dash's own
// agent page keeps chatMarkdown, which it shows with pre-wrap.
func paneMarkdown(s string, img func(path string) string) template.HTML {
	var b strings.Builder
	parts := strings.Split(strings.ReplaceAll(s, "\r", ""), "```")
	for i, part := range parts {
		if i%2 == 1 {
			if nl := strings.IndexByte(part, '\n'); nl >= 0 && !strings.ContainsAny(part[:nl], " \t") {
				part = part[nl+1:]
			}
			b.WriteString("<pre><code>" + html.EscapeString(strings.TrimRight(part, "\n")) + "</code></pre>")
			continue
		}
		b.WriteString(paneBlocks(part, img))
	}
	return template.HTML(b.String()) // escaped piece by piece
}

var (
	olRe     = regexp.MustCompile(`^\s{0,3}(\d{1,3})[.)]\s+(.*)$`)
	ulRe     = regexp.MustCompile(`^\s{0,3}[-*+]\s+(.*)$`)
	headRe   = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.*)$`)
	italicRe = regexp.MustCompile(`(^|[\s(])[*_]([^*_\s][^*_\n]*?)[*_]([\s).,;:!?]|$)`)
	quoteRe  = regexp.MustCompile(`^\s{0,3}>\s?(.*)$`)
)

// mdImageRe is a markdown image naming an absolute path.
var mdImageRe = regexp.MustCompile(`!\[([^\]\n]*)\]\((/[^)\s]+)\)`)

// paneInline is chatInline with italics, and with images that img allows
// (it returns their link, or "" to leave the text as it is).
func paneInline(s string, img func(string) string) string {
	var b strings.Builder
	rest := s
	for img != nil {
		m := mdImageRe.FindStringSubmatchIndex(rest)
		if m == nil {
			break
		}
		u := img(rest[m[4]:m[5]])
		if u == "" {
			b.WriteString(inlineText(rest[:m[1]]))
			rest = rest[m[1]:]
			continue
		}
		b.WriteString(inlineText(rest[:m[0]]))
		alt, link := html.EscapeString(rest[m[2]:m[3]]), html.EscapeString(u)
		b.WriteString(`<a class="tk-img" href="` + link + `"><img src="` + link + `" alt="` + alt + `" loading="lazy"></a>`)
		rest = rest[m[1]:]
	}
	b.WriteString(inlineText(rest))
	return b.String()
}

func inlineText(s string) string {
	return italicRe.ReplaceAllString(chatInline(s), "$1<em>$2</em>$3")
}

// paneBlocks renders text outside code fences, a line at a time.
func paneBlocks(s string, img func(string) string) string {
	var b strings.Builder
	lines := strings.Split(s, "\n")
	var para []string
	list := "" // "ol" or "ul" while one is open
	var item []string
	flushItem := func() {
		if item != nil {
			b.WriteString("<li>" + paneInline(strings.Join(item, " "), img) + "</li>")
			item = nil
		}
	}
	closeList := func() {
		flushItem()
		if list != "" {
			b.WriteString("</" + list + ">")
			list = ""
		}
	}
	flushPara := func() {
		if len(para) > 0 {
			b.WriteString("<p>" + strings.Join(func() []string {
				out := make([]string, len(para))
				for i, l := range para {
					out[i] = paneInline(l, img)
				}
				return out
			}(), "<br>") + "</p>")
			para = nil
		}
	}
	openList := func(kind, start string) {
		flushPara()
		if list != kind {
			closeList()
			list = kind
			if kind == "ol" && start != "" && start != "1" {
				b.WriteString(`<ol start="` + html.EscapeString(start) + `">`)
			} else {
				b.WriteString("<" + kind + ">")
			}
		}
		flushItem()
	}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)
		switch {
		case trim == "":
			flushPara()
			// A blank line inside a list keeps it open if the list goes on.
			if list != "" {
				next := ""
				for j := i + 1; j < len(lines); j++ {
					if strings.TrimSpace(lines[j]) != "" {
						next = lines[j]
						break
					}
				}
				if !(list == "ol" && olRe.MatchString(next)) && !(list == "ul" && ulRe.MatchString(next)) {
					closeList()
				}
			}
		case i+1 < len(lines) && isTableRow(line) && mdRuleRe.MatchString(strings.TrimSpace(lines[i+1])):
			flushPara()
			closeList()
			end := i + 2
			for end < len(lines) && isTableRow(lines[end]) {
				end++
			}
			b.WriteString(chatTable(line, lines[i+2:end]))
			i = end - 1
		case olRe.MatchString(line):
			m := olRe.FindStringSubmatch(line)
			openList("ol", m[1])
			item = []string{m[2]}
		case ulRe.MatchString(line) && !strings.HasPrefix(trim, "**"):
			openList("ul", "")
			item = []string{ulRe.FindStringSubmatch(line)[1]}
		case headRe.MatchString(line):
			flushPara()
			closeList()
			b.WriteString("<h4>" + paneInline(headRe.FindStringSubmatch(line)[1], img) + "</h4>")
		case quoteRe.MatchString(line):
			flushPara()
			closeList()
			b.WriteString("<blockquote>" + paneInline(quoteRe.FindStringSubmatch(line)[1], img) + "</blockquote>")
		case list != "" && item != nil && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")):
			// A continuation of the list item.
			item = append(item, trim)
		default:
			closeList()
			para = append(para, trim)
		}
	}
	flushPara()
	closeList()
	return b.String()
}
