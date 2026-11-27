package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mayoi/locales"
)

func TestOrgToMarkdown(t *testing.T) {
	src := strings.Join([]string{
		"#+TITLE: CoreEQ",
		"#+SUBTITLE: 适用于 macOS 的系统级均衡器",
		"#+AUTHOR: Test",
		"# hidden comment",
		":PROPERTIES:",
		":ID: abc-123",
		":END:",
		"",
		"CoreEQ 会均衡 *你听到的一切*——以及 /斜体/、+删除+、_下划线_、=brew update=、~code~ 和 a_b_c 与 path/to/file。",
		"",
		"[[./design/screenshots/core-eq.png]]",
		"[[file:docs/USER_GUIDE.org][用户指南]] 和 [[https://example.com][Example]]",
		"",
		"* 亮点",
		"** Homebrew（推荐）",
		"#+begin_src sh :results output",
		"brew install --cask core-eq",
		",* escaped star",
		"#+end_src",
		"",
		"#+begin_quote",
		"A *quoted* line.",
		"#+end_quote",
		"",
		"#+begin_comment",
		"not shown",
		"#+end_comment",
		"",
		"-----",
		"",
		"|---+---|",
		"| 文档 | 内容 |",
		"|---+---|",
		"| [[file:docs/USER_GUIDE.org][用户指南]] | 说明 |",
		"|---+---|",
		"",
		`Inline math \(E = mc^2\) here.`,
		"",
	}, "\n")

	got := string(orgToMarkdown([]byte(src)))

	// Line count must stay 1:1 with the Org source for scroll sync.
	if gotLines, wantLines := strings.Count(got, "\n"), strings.Count(src, "\n"); gotLines != wantLines {
		t.Fatalf("line count = %d, want %d\n%s", gotLines, wantLines, got)
	}

	lines := strings.Split(got, "\n")
	checks := []struct {
		line int
		want string
	}{
		{0, "# CoreEQ"},
		{1, "*适用于 macOS 的系统级均衡器*"},
		{2, ""},
		{3, ""},
		{4, ""},
		{5, ""},
		{6, ""},
		{8, "CoreEQ 会均衡 **你听到的一切**——以及 *斜体*、~~删除~~、<u>下划线</u>、`brew update`、`code` 和 a_b_c 与 path/to/file。"},
		{10, "![](./design/screenshots/core-eq.png)"},
		{11, "[用户指南](docs/USER_GUIDE.org) 和 [Example](https://example.com)"},
		{13, "# 亮点"},
		{14, "## Homebrew（推荐）"},
		{15, "```sh"},
		{16, "brew install --cask core-eq"},
		{17, "* escaped star"},
		{18, "```"},
		{21, "> A **quoted** line."},
		{25, ""},
		{28, "---"},
		{30, ""},
		{31, "| 文档 | 内容 |"},
		{32, "|---|---|"},
		{33, "| [用户指南](docs/USER_GUIDE.org) | 说明 |"},
		{34, ""},
		{36, "Inline math $E = mc^2$ here."},
	}
	for _, c := range checks {
		if lines[c.line] != c.want {
			t.Errorf("line %d:\n got: %q\nwant: %q", c.line, lines[c.line], c.want)
		}
	}
}

func TestParseOrgDocument(t *testing.T) {
	src := `#+TITLE: CoreEQ
#+SUBTITLE: System-wide EQ

CoreEQ shapes *everything you hear* with =Core Audio=.

[[./shot.png]]

* Highlights

- *All apps.* No drivers.
- Run =brew install --cask core-eq=.

** Build

#+begin_src sh
make install
#+end_src

| Doc | About |
|-----+-------|
| [[file:docs/GUIDE.org][Guide]] | Details |
`
	blocks := parseMarkdown([]byte(src), "/proj/README.org", "/proj", nil)

	var headings, codeBlocks, tableHeaders, tableRows int
	var hasBold, hasCode, hasLink, hasImage bool
	for _, b := range blocks {
		switch b.kind {
		case markdownHeading:
			headings++
		case markdownCodeBlock:
			codeBlocks++
			if b.language != "sh" {
				t.Errorf("code block language = %q, want sh", b.language)
			}
		case markdownTableHeader:
			tableHeaders++
		case markdownTable:
			tableRows++
			for _, cell := range b.cells {
				for _, r := range cell {
					if r.link == "docs/GUIDE.org" && r.text == "Guide" {
						hasLink = true
					}
				}
			}
		case markdownImageBlock:
			hasImage = true
		}
		for _, r := range b.runs {
			if r.bold && r.text == "everything you hear" {
				hasBold = true
			}
			if r.code && r.text == "Core Audio" {
				hasCode = true
			}
			if r.image == "./shot.png" {
				hasImage = true
			}
		}
	}
	if headings != 3 {
		t.Errorf("headings = %d, want 3", headings)
	}
	if codeBlocks != 1 {
		t.Errorf("codeBlocks = %d, want 1", codeBlocks)
	}
	if tableHeaders != 1 || tableRows != 1 {
		t.Errorf("table = %d header / %d rows, want 1 / 1", tableHeaders, tableRows)
	}
	if !hasBold || !hasCode || !hasLink || !hasImage {
		t.Errorf("bold=%v code=%v link=%v image=%v, want all true", hasBold, hasCode, hasLink, hasImage)
	}
}

func TestOrgPreviewAndComment(t *testing.T) {
	hs := newHarness(t, true)
	orgPath := filepath.Join(hs.root, "notes.org")
	if err := os.WriteFile(orgPath, []byte("#+TITLE: Notes\n\n* Section\n\nHello *world*.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hs.a.openFile(orgPath, true)
	hs.until("org file loaded", func() bool {
		d := hs.a.activeEditor()
		return d != nil && d.path == orgPath && !d.unread()
	})
	d := hs.a.activeEditor()
	if !canPreview(d.document) {
		t.Fatal("canPreview(.org) = false, want true")
	}
	hs.settle()
	for _, want := range []string{
		hs.a.withKey(locales.MarkdownModeSource(), "view.togglePreview"),
		hs.a.withKey(locales.MarkdownModeReading(), "view.togglePreview"),
	} {
		if !hs.tt.HasText(want) {
			t.Errorf("missing Org mode button %q", want)
		}
	}

	// Toggle Preview opens the Reading view via markdownPreview.
	hs.a.run("view.togglePreview")
	hs.until("org preview loaded", func() bool {
		return d.preview != nil && d.preview.markdown != nil && d.preview.loaded
	})
	if len(d.preview.markdown.blocks) == 0 {
		t.Fatal("org preview produced no blocks")
	}

	// Toggle back to source and verify Toggle Line Comment uses #.
	hs.a.run("view.togglePreview")
	hs.settle()
	if d.preview != nil {
		t.Fatal("preview still open after toggling back")
	}
	s, _ := d.buf.Snapshot().LineRange(4)
	hs.place(d, s, s)
	hs.a.run("sel.comment")
	hs.settle()
	if !strings.Contains(d.buf.String(), "# Hello *world*.") {
		t.Errorf("commented org buffer:\n%s", d.buf.String())
	}
}
