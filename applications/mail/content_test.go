package mail

import (
	"strings"
	"testing"

	"github.com/emersion/go-message"
)

func TestInboundNewsletterPreservesSafeFormatting(t *testing.T) {
	entity, err := message.Read(strings.NewReader("Content-Type: text/html; charset=utf-8\r\n\r\n" +
		`<style>@import "https://example.test/a.css";</style><script>bad()</script>` +
		`<table width="100%" cellpadding="0"><tr><td style="background-color:#3264ff; padding:24px; background-image:url(https://example.test/pixel); position:fixed">` +
		`<span style="display:none">Preview</span><strong style="color:#ffffff">October</strong>` +
		`<img src="https://example.test/banner.png" alt="Banner" onerror="bad()">` +
		`<img src="http://example.test/image.png"><img src="mailto:invalid@example.test">` +
		`</td></tr></table>`))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseMIME(entity, 4096)
	if err != nil {
		t.Fatal(err)
	}
	for _, wanted := range []string{`width="100%"`, `cellpadding="0"`, "background-color", "#3264ff", "padding", "24px", "display", "none", "#ffffff", `src="https://example.test/banner.png"`} {
		if !strings.Contains(parsed.HTML, wanted) {
			t.Errorf("missing %q from %s", wanted, parsed.HTML)
		}
	}
	for _, blocked := range []string{"<style", "@import", "<script", "bad()", "background-image", "position", "onerror", "http://", "mailto:"} {
		if strings.Contains(parsed.HTML, blocked) {
			t.Errorf("unexpected %q in %s", blocked, parsed.HTML)
		}
	}
}

func TestRenderMarkdownSanitizesHTML(t *testing.T) {
	text, html, err := RenderMarkdown("# Title\n\n<script>alert(1)</script>\n\n[unsafe](javascript:alert(1)) [safe](https://example.test)")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "Title") || strings.Contains(text, "# Title") || strings.Contains(text, "https://example.test") {
		t.Fatalf("plain alternative was not rendered: %q", text)
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "javascript:") {
		t.Fatalf("unsafe HTML survived: %s", html)
	}
	if !strings.Contains(html, "<h1>Title</h1>") || !strings.Contains(html, `href="https://example.test"`) {
		t.Fatalf("safe markdown was not rendered: %s", html)
	}
}

func TestSanitizeHTMLRejectsOversizeInput(t *testing.T) {
	if _, err := SanitizeHTML("<p>"+strings.Repeat("a", 11)+"</p>", 10); err == nil {
		t.Fatal("oversize HTML was accepted")
	}
}
