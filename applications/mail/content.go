package mail

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"regexp"
	"strings"
	"unicode/utf8"

	"horizon/foundation/system/governance"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	goldhtml "github.com/yuin/goldmark/renderer/html"
)

var whitespace = regexp.MustCompile(`[ \t\r\n]+`)
var mailDimension = regexp.MustCompile(`^[0-9]{1,4}%?$`)
var mailColor = regexp.MustCompile(`(?i)^(#[0-9a-f]{3}|#[0-9a-f]{6}|[a-z]{1,24})$`)
var mailAlign = regexp.MustCompile(`(?i)^(left|center|right|justify)$`)
var mailVerticalAlign = regexp.MustCompile(`(?i)^(top|middle|bottom|baseline)$`)
var mailImageURL = regexp.MustCompile(`(?i)^https://[^\s]+$`)

func mailHTMLPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("a", "b", "blockquote", "br", "code", "dd", "div", "dl", "dt", "em",
		"h1", "h2", "h3", "h4", "h5", "h6", "hr", "i", "li", "ol", "p", "pre", "s",
		"strong", "sub", "sup", "table", "tbody", "td", "th", "thead", "tr", "u", "ul")
	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("colspan", "rowspan").Matching(bluemonday.Integer).OnElements("td", "th")
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireNoFollowOnLinks(true)
	return p
}

func SanitizeHTML(value string, limit int) (string, error) {
	return sanitizeHTML(mailHTMLPolicy(), value, limit)
}

// Newsletters use layout tables and inline styles. Keep visual declarations
// while leaving active content and URL-bearing CSS outside the allowlist.
func inboundHTMLPolicy() *bluemonday.Policy {
	p := mailHTMLPolicy()
	p.AllowElements("caption", "center", "col", "colgroup", "font", "img", "span", "tfoot")
	p.AllowAttrs("width", "height").Matching(mailDimension).OnElements("table", "td", "th", "img", "col")
	p.AllowAttrs("border", "cellspacing", "cellpadding").Matching(bluemonday.Integer).OnElements("table")
	p.AllowAttrs("bgcolor").Matching(mailColor).OnElements("table", "tr", "td", "th")
	p.AllowAttrs("color").Matching(mailColor).OnElements("font")
	p.AllowAttrs("align").Matching(mailAlign).OnElements("table", "tr", "td", "th", "div", "p")
	p.AllowAttrs("valign").Matching(mailVerticalAlign).OnElements("tr", "td", "th")
	p.AllowAttrs("alt").OnElements("img")
	p.AllowAttrs("src").Matching(mailImageURL).OnElements("img")
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.RequireNoReferrerOnLinks(true)
	p.AllowStyles(
		"background-color", "border", "border-bottom", "border-collapse", "border-color",
		"border-left", "border-radius", "border-right", "border-spacing", "border-style",
		"border-top", "border-width", "color", "display", "font-family", "font-size",
		"font-style", "font-weight", "height", "letter-spacing", "line-height",
		"margin", "margin-bottom", "margin-left", "margin-right", "margin-top",
		"max-height", "max-width", "min-height", "min-width", "overflow",
		"padding", "padding-bottom", "padding-left", "padding-right", "padding-top",
		"table-layout", "text-align", "text-decoration", "text-transform", "vertical-align",
		"white-space", "width",
	).Globally()
	return p
}

func sanitizeHTML(policy *bluemonday.Policy, value string, limit int) (string, error) {
	if len(value) > limit {
		return "", governance.Problem("invalid")
	}
	out := policy.Sanitize(strings.ToValidUTF8(value, ""))
	if len(out) > limit {
		return "", governance.Problem("invalid")
	}
	return out, nil
}

func RenderMarkdown(markdown string) (string, string, error) {
	if strings.TrimSpace(markdown) == "" || !utf8.ValidString(markdown) {
		return "", "", governance.Problem("invalid")
	}
	var rendered bytes.Buffer
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(goldhtml.WithHardWraps()),
	)
	if err := md.Convert([]byte(markdown), &rendered); err != nil {
		return "", "", err
	}
	safe, err := SanitizeHTML(rendered.String(), 1000000)
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(HTMLToText(safe)), safe, nil
}

func renderOutboundBody(markdown, html string, limit int) (string, string, error) {
	if html == "" {
		return RenderMarkdown(markdown)
	}
	if markdown != "" {
		return "", "", governance.Problem("invalid")
	}
	safe, err := SanitizeHTML(html, limit)
	if err != nil {
		return "", "", err
	}
	text := strings.TrimSpace(HTMLToText(safe))
	if text == "" {
		return "", "", governance.Problem("invalid")
	}
	return text, safe, nil
}

func HTMLToText(value string) string {
	text := mailHTMLPolicy().Sanitize(value)
	text = strings.ReplaceAll(text, "</p>", "\n")
	text = strings.ReplaceAll(text, "<br>", "\n")
	text = strings.ReplaceAll(text, "<br/>", "\n")
	text = regexp.MustCompile(`<[^>]*>`).ReplaceAllString(text, "")
	text = stdhtml.UnescapeString(text)
	return strings.ToValidUTF8(text, "")
}

func Snippet(text string) string {
	value := strings.TrimSpace(whitespace.ReplaceAllString(text, " "))
	runes := []rune(value)
	if len(runes) > 200 {
		return string(runes[:200])
	}
	return value
}

func endpoint(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
