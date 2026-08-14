package vdoc

import (
	"fmt"
	"strings"
	"testing"
)

func TestMarkdownDiffCountsSeparatedReplacementBlocks(t *testing.T) {
	diff := markdownDiff(
		"document-a",
		"version-a",
		"version-b",
		"heading\nold first\nstable anchor\nold second\nfooter",
		"heading\nnew first\nstable anchor\nnew second\nfooter",
	)

	if diff.Summary.ModifiedBlocks != 2 || diff.Summary.ModifiedLines != 2 || diff.Summary.AddedLines != 0 || diff.Summary.RemovedLines != 0 {
		t.Fatalf("summary = %+v, want two replacement blocks and two modified lines", diff.Summary)
	}
}

func TestMarkdownDiffCountsSeparatedInsertionAndDeletionBlocks(t *testing.T) {
	diff := markdownDiff(
		"document-a",
		"version-a",
		"version-b",
		"heading\nremove me\nstable anchor\nfooter",
		"heading\nstable anchor\nadd me\nfooter",
	)

	if diff.Summary.ModifiedBlocks != 2 || diff.Summary.ModifiedLines != 0 || diff.Summary.AddedLines != 1 || diff.Summary.RemovedLines != 1 {
		t.Fatalf("summary = %+v, want separate insertion and deletion blocks", diff.Summary)
	}
}

func TestMarkdownDiffAlignsRepeatedLinesBeforeClassifyingChanges(t *testing.T) {
	diff := markdownDiff(
		"document-a",
		"version-a",
		"version-b",
		"alpha\nbeta\nalpha\nbeta",
		"beta\nalpha\nbeta\nalpha",
	)

	if diff.Summary.ModifiedBlocks != 2 || diff.Summary.ModifiedLines != 0 || diff.Summary.AddedLines != 1 || diff.Summary.RemovedLines != 1 {
		t.Fatalf("repeated-line summary = %+v, want one moved line represented by one removal and one addition", diff.Summary)
	}
}

func TestMarkdownDiffHandlesLargeUnrelatedInputsWithLinearMemoryShape(t *testing.T) {
	const lineCount = 10_000
	from := make([]string, lineCount)
	to := make([]string, lineCount)
	for index := 0; index < lineCount; index++ {
		from[index] = fmt.Sprintf("old-%05d", index)
		to[index] = fmt.Sprintf("new-%05d", index)
	}

	diff := markdownDiff("document-a", "version-a", "version-b", strings.Join(from, "\n"), strings.Join(to, "\n"))

	if diff.Summary.ModifiedBlocks != 1 || diff.Summary.ModifiedLines != lineCount || diff.Summary.AddedLines != 0 || diff.Summary.RemovedLines != 0 {
		t.Fatalf("large summary = %+v", diff.Summary)
	}
}
