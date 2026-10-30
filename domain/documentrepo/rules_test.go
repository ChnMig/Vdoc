package documentrepo

import (
	"strings"
	"testing"
	"time"

	common "vdoc/common/vdoc"
)

func TestMergeTextPreservesBytesAndRejectsOverlappingEdits(t *testing.T) {
	for _, tt := range []struct {
		name, base, left, right, want string
		ok                            bool
	}{
		{"separate CRLF lines", "one\r\ntwo\r\nthree\r\n", "ONE\r\ntwo\r\nthree\r\n", "one\r\ntwo\r\nTHREE\r\n", "ONE\r\ntwo\r\nTHREE\r\n", true},
		{"same edit", "a\nb\n", "A\nb\n", "A\nb\n", "A\nb\n", true},
		{"overlap", "a\nb\n", "A\nb\n", "other\nb\n", "", false},
		{"same insertion", "a\nb\n", "a\nx\nb\n", "a\nx\nb\n", "a\nx\nb\n", true},
		{"different insertion at same point", "a\nb\n", "a\nx\nb\n", "a\ny\nb\n", "", false},
		{"delete and unrelated change", "a\nb\nc\n", "b\nc\n", "a\nb\nC\n", "b\nC\n", true},
		{"missing final newline", "a\nb\nc", "A\nb\nc", "a\nb\nC", "A\nb\nC", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := MergeText(tt.base, tt.left, tt.right)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("MergeText() = %q, %v, want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
	if _, ok := MergeText(strings.Repeat("a\n", 2500), strings.Repeat("b\n", 2500), strings.Repeat("c\n", 2500)); ok {
		t.Fatal("unbounded LCS merge accepted")
	}
}

func TestCommitIdentityIncludesContentMetadataParentsAndServerTime(t *testing.T) {
	files := []File{{DocumentID: "one", RelativePath: "one.md", Name: "One", DocumentType: common.DocumentTypeMarkdown, ContentHash: Hash([]byte("one\r\n")), ObjectKey: "a"}}
	now := time.Date(2026, 10, 30, 1, 2, 3, 123456000, time.UTC)
	a := NewCommit("project", "author", "message", []string{"parent"}, files, now)
	files[0].ObjectKey = "b"
	b := NewCommit("project", "author", "message", []string{"parent"}, files, now)
	if a.ID != b.ID || a.TreeHash != b.TreeHash || len(a.ID) != 64 {
		t.Fatal("storage location changed commit identity")
	}
	files[0].Name = "Renamed"
	c := NewCommit("project", "author", "message", []string{"parent"}, files, now)
	d := NewCommit("project", "author", "message", []string{"other-parent"}, files, now)
	e := NewCommit("project", "author", "message", []string{"other-parent"}, files, now.Add(time.Microsecond))
	if c.ID == a.ID || c.TreeHash == a.TreeHash || d.ID == c.ID || e.ID == d.ID || a.Files[0].Name != "One" {
		t.Fatal("metadata, ancestry or server time lost from immutable identity")
	}
}

func TestValidateFilesRejectsAmbiguousPathsAndUnboundedMetadata(t *testing.T) {
	base := File{DocumentID: "one", RelativePath: "guides/one.md", Name: "One", DocumentType: common.DocumentTypeMarkdown}
	for _, path := range []string{"../one.md", "/one.md", "guides/../one.md", "guides//one.md", "guides\\one.md", "one.txt", "one\x00.md"} {
		f := base
		f.RelativePath = path
		if err := ValidateFiles([]File{f}); err == nil {
			t.Errorf("accepted path %q", path)
		}
	}
	duplicate := base
	duplicate.DocumentID = "two"
	if err := ValidateFiles([]File{base, duplicate}); err == nil {
		t.Fatal("duplicate path accepted")
	}
	base.Description = strings.Repeat("x", 4001)
	if err := ValidateFiles([]File{base}); err == nil {
		t.Fatal("unbounded metadata accepted")
	}
}
