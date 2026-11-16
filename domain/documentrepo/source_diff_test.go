package documentrepo

import (
	"fmt"
	"strings"
	"testing"
)

func TestCompareSourcePreservesExactBytesAndDirection(t *testing.T) {
	for _, pair := range [][2]string{
		{"", "added\n"}, {"removed\r\n", ""},
		{"\ufefftitle\r\nold\r\nfooter", "\ufefftitle\r\nnew\r\nfooter"},
		{"space \n", "space\n"}, {"line\n", "line"}, {"line\r\n", "line\n"},
	} {
		t.Run(fmt.Sprintf("%q-to-%q", pair[0], pair[1]), func(t *testing.T) {
			diff, used := CompareSource(pair[0], pair[1], SourceDiffByteLimit)
			if diff.Truncated || len(diff.Hunks) != 1 || used != len(pair[0])+len(pair[1]) {
				t.Fatalf("preview=%+v used=%d", diff, used)
			}
			hunk := diff.Hunks[0]
			if hunk.OldStartLine != 1 || hunk.NewStartLine != 1 || hunk.OldContent != pair[0] || hunk.NewContent != pair[1] {
				t.Fatalf("source bytes or direction changed: %+v", hunk)
			}
			reverse, _ := CompareSource(pair[1], pair[0], SourceDiffByteLimit)
			if reverse.AddedLines != diff.RemovedLines || reverse.RemovedLines != diff.AddedLines {
				t.Fatalf("reverse counts=%+v forward=%+v", reverse, diff)
			}
		})
	}
	unchanged, _ := CompareSource("same\n", "same\n", SourceDiffByteLimit)
	if len(unchanged.Hunks) != 0 || unchanged.Truncated || unchanged.AddedLines != 0 || unchanged.RemovedLines != 0 {
		t.Fatalf("unchanged=%+v", unchanged)
	}
}

func TestCompareSourceSingleChangeInLargeDocument(t *testing.T) {
	lines := make([]string, 5000)
	for i := range lines {
		lines[i] = fmt.Sprintf("Requirement %d stays unchanged.\n", i+1)
	}
	before := strings.Join(lines, "")
	lines[2499] = "Requirement 2500 changed.\n"
	after := strings.Join(lines, "")
	diff, used := CompareSource(before, after, SourceDiffByteLimit)
	if diff.Truncated || diff.AddedLines != 1 || diff.RemovedLines != 1 || diff.HunkCount != 1 || len(diff.Hunks) != 1 || used > 1000 {
		t.Fatalf("large sparse preview=%+v used=%d", diff, used)
	}
	hunk := diff.Hunks[0]
	if hunk.OldStartLine != 2497 || hunk.NewStartLine != 2497 || hunk.OldLineCount != 7 || hunk.NewLineCount != 7 {
		t.Fatalf("context or positions=%+v", hunk)
	}
}

func TestCompareSourceBudgetsDeclareOmissions(t *testing.T) {
	large := strings.Repeat("大", SourceDiffByteLimit)
	diff, used := CompareSource("before\n", large, SourceDiffByteLimit)
	if !diff.Truncated || used != 0 || len(diff.Hunks) != 1 || !diff.Hunks[0].ContentOmitted || diff.Hunks[0].NewLineCount != 1 {
		t.Fatalf("oversized hunk must retain range and declare omission: %+v used=%d", diff, used)
	}
	oldLines, newLines := []string{}, []string{}
	for i := 0; i < 25; i++ {
		oldLines = append(oldLines, fmt.Sprintf("old-%d\n", i))
		newLines = append(newLines, fmt.Sprintf("new-%d\n", i))
		for j := 0; j < 10; j++ {
			line := fmt.Sprintf("stable-%d-%d\n", i, j)
			oldLines, newLines = append(oldLines, line), append(newLines, line)
		}
	}
	diff, used = CompareSource(strings.Join(oldLines, ""), strings.Join(newLines, ""), SourceDiffByteLimit)
	if !diff.Truncated || diff.HunkCount != 25 || len(diff.Hunks) != SourceDiffHunkLimit || diff.AddedLines != 25 || diff.RemovedLines != 25 || used > SourceDiffByteLimit {
		t.Fatalf("hunk budget=%+v used=%d", diff, used)
	}
	diff, used = CompareSource("a\nb\nc\n", "a\nB\nc\n", 0)
	if !diff.Truncated || used != 0 || !diff.Hunks[0].ContentOmitted {
		t.Fatalf("exhausted shared budget=%+v used=%d", diff, used)
	}
}
