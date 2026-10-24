package documentdraft

import (
	"errors"
	"testing"

	commonvdoc "vdoc/common/vdoc"
)

func TestValidateContentAllowsEmptyTXTOnly(t *testing.T) {
	for _, content := range []string{"", " \r\n\t"} {
		if err := ValidateContent("initial", content, commonvdoc.DocumentFormatText); err != nil {
			t.Fatalf("TXT content %q error = %v", content, err)
		}
		for _, format := range []int{commonvdoc.DocumentFormatMarkdown, commonvdoc.DocumentFormatOpenAPI30, commonvdoc.DocumentFormatOpenAPI31} {
			if err := ValidateContent("initial", content, format); !errors.Is(err, commonvdoc.ErrInvalidArgument) {
				t.Fatalf("format %d empty content error = %v, want invalid argument", format, err)
			}
		}
	}
	if err := ValidateContent("  ", "", commonvdoc.DocumentFormatText); !errors.Is(err, commonvdoc.ErrInvalidArgument) {
		t.Fatalf("TXT empty version name error = %v, want invalid argument", err)
	}
}
