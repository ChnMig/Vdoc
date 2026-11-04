package documentrepo

import (
	"errors"
	"strings"
	"testing"

	domain "vdoc/domain/vdoc"
)

func TestTextEditsUseUniqueNonOverlappingOriginalAnchors(t *testing.T) {
	for _, tc := range []struct {
		name, source, want string
		edits              []TextEdit
		err                error
	}{
		{"preserve bytes", "\ufeffopaque=a\r\nother=b\r\n", "\ufeffopaque=c\r\nother=b\r\n", []TextEdit{{"opaque=a", "opaque=c"}}, nil},
		{"original anchors", "one two", "two three", []TextEdit{{"one", "two"}, {"two", "three"}}, nil},
		{"reverse input order", "one two", "three four", []TextEdit{{"two", "four"}, {"one", "three"}}, nil},
		{"empty file", "", "new\r\n", []TextEdit{{"", "new\r\n"}}, nil},
		{"delete", "before middle after", "before  after", []TextEdit{{"middle", ""}}, nil},
		{"missing", "one", "", []TextEdit{{"two", "three"}}, domain.ErrFailedPrecondition},
		{"repeated", "one one", "", []TextEdit{{"one", "two"}}, domain.ErrFailedPrecondition},
		{"overlapping matches", "aaa", "", []TextEdit{{"aa", "b"}}, domain.ErrFailedPrecondition},
		{"overlapping edits", "abc", "", []TextEdit{{"ab", "x"}, {"bc", "y"}}, domain.ErrInvalidArgument},
		{"ambiguous empty", "x", "", []TextEdit{{"", "y"}}, domain.ErrInvalidArgument},
		{"too many", "x", "", make([]TextEdit, 101), domain.ErrInvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ApplyTextEdits(tc.source, tc.edits)
			if !errors.Is(err, tc.err) || err == nil && got != tc.want {
				t.Fatalf("got %q, %v; want %q, %v", got, err, tc.want, tc.err)
			}
		})
	}
}

func TestRawLineViewsPreserveSourceAndHaveExplicitBounds(t *testing.T) {
	source := "\ufefffirst\r\n\r\n第三行\nlast"
	first, err := ReadTextLines(source, 1, 2)
	if err != nil || first.Content != "\ufefffirst\r\n\r\n" || first.TotalLines != 4 || first.NextLine != 3 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	last, err := ReadTextLines(source, first.NextLine, 0)
	if err != nil || first.Content+last.Content != source || last.NextLine != 0 {
		t.Fatalf("last=%+v err=%v", last, err)
	}
	if lines := SourceLines("a\n"); len(lines) != 1 {
		t.Fatalf("fabricated trailing line: %q", lines)
	}
	if empty, err := ReadTextLines("", 0, 0); err != nil || empty.Content != "" || empty.TotalLines != 0 {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
	for _, span := range [][2]int{{-1, 2}, {3, 2}, {5, 0}, {1, 5}} {
		if _, err := ReadTextLines(source, span[0], span[1]); !errors.Is(err, domain.ErrInvalidArgument) {
			t.Fatalf("accepted invalid range %v: %v", span, err)
		}
	}
	if _, err := ReadTextLines(strings.Repeat("x", SectionByteLimit+1), 1, 1); !errors.Is(err, domain.ErrFailedPrecondition) {
		t.Fatalf("large line was silently truncated: %v", err)
	}
}

func TestMarkdownSectionsKeepNestedHeadingsAndIgnoreFences(t *testing.T) {
	source := "\ufeff# Intro\r\ntext\r\n## Child\r\n```md\r\n# fake\r\n```\r\n# Next\r\nend\r\n"
	headings := MarkdownHeadings(source)
	if len(headings) != 3 || headings[0].StartLine != 1 || headings[0].EndLine != 6 {
		t.Fatalf("outline=%+v", headings)
	}
	section, err := ReadMarkdownHeading(source, "Intro")
	if err != nil || section.Content != strings.Split(source, "# Next")[0] {
		t.Fatalf("section=%+v err=%v", section, err)
	}
	if _, err := ReadMarkdownHeading("# Repeat\n# Repeat\n", "Repeat"); !errors.Is(err, domain.ErrInvalidArgument) {
		t.Fatalf("ambiguous heading accepted: %v", err)
	}
}
