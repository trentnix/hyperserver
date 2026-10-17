package blog

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSlug(t *testing.T) {
	for input, want := range map[string]string{
		"Hello, World!": "hello-world", "  multiple --- spaces  ": "multiple-spaces",
		"2026 / Go": "2026-go", "日本語": "", "already-valid": "already-valid",
	} {
		if got := Slug(input); got != want {
			t.Errorf("Slug(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFeedMarkdownURLs(t *testing.T) {
	base, _ := url.Parse("https://blog.example/post/s/hello")
	html, err := renderMarkdown("![image](/images/123)\n\n[relative](../next) [safe](https://other.example/path) [unsafe](javascript:alert%281%29)", base)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`src="https://blog.example/images/123"`, `href="https://blog.example/post/next"`, `href="https://other.example/path"`} {
		if !strings.Contains(string(html), want) {
			t.Fatalf("missing %s in %s", want, html)
		}
	}
	if strings.Contains(string(html), "javascript:") {
		t.Fatal("unsafe feed link", html)
	}
}

func TestPostValidation(t *testing.T) {
	valid := Post{ID: uuid.NewString(), Slug: "hello", Title: "Hello", Markdown: "Text", PublishedAt: time.Now(), Version: 1}
	if err := Validate(valid); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Post){
		"id":          func(p *Post) { p.ID = "../bad" },
		"slug":        func(p *Post) { p.Slug = "Bad Slug" },
		"empty title": func(p *Post) { p.Title = " " },
		"long title":  func(p *Post) { p.Title = strings.Repeat("日", 201) },
		"summary":     func(p *Post) { p.Summary = strings.Repeat("a", 1001) },
		"empty body":  func(p *Post) { p.Markdown = "\n" },
		"long body":   func(p *Post) { p.Markdown = strings.Repeat("a", MaxContentBytes+1) },
		"utf8":        func(p *Post) { p.Markdown = "\xff" },
		"split title and summary UTF-8": func(p *Post) {
			p.Title, p.Summary = "\xc3", "\xa9"
		},
		"split summary and body UTF-8": func(p *Post) {
			p.Summary, p.Markdown = "\xc3", "\xa9"
		},
		"date":          func(p *Post) { p.PublishedAt = time.Time{} },
		"date overflow": func(p *Post) { p.PublishedAt = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC) },
		"version":       func(p *Post) { p.Version = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			p := valid
			change(&p)
			if Validate(p) == nil {
				t.Fatal("accepted invalid post")
			}
		})
	}
}

func TestMarkdownTrustBoundary(t *testing.T) {
	for _, input := range []string{
		`<script>alert(1)</script>`, `<img src=x onerror=alert(1)>`,
		`[click](javascript:alert%281%29)`, `![image](data:image/svg+xml;base64,PHN2Zz4=)`,
	} {
		got, err := RenderMarkdown(input)
		if err != nil {
			t.Fatal(err)
		}
		for _, dangerous := range []string{"<script", "onerror=", "javascript:", "data:image/svg"} {
			if strings.Contains(string(got), dangerous) {
				t.Fatalf("unsafe Markdown output: %s", got)
			}
		}
	}
	got, err := RenderMarkdown("# Heading\n\n**bold** & text\n\n![caption](/images/123)")
	if err != nil || !strings.Contains(string(got), "<strong>bold</strong>") || !strings.Contains(string(got), "&amp;") || !strings.Contains(string(got), `src="/images/123"`) {
		t.Fatalf("Markdown = %s, %v", got, err)
	}
	if _, err := RenderMarkdown(strings.Repeat("a", MaxContentBytes+1)); err == nil {
		t.Fatal("unbounded Markdown")
	}
}
