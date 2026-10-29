package ai

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestChatAttachmentsRejectInvalidDataAndEnforceBudgets(t *testing.T) {
	valid := ChatAttachment{Name: "notes.md", MediaType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("# 你好\nContext"))}
	if result, err := ValidateChatAttachments([]ChatAttachment{valid}); err != nil || len(result) != 1 || result[0].Text() != "# 你好\nContext" {
		t.Fatalf("valid text rejected: %v", err)
	}
	for name, input := range map[string][]ChatAttachment{
		"filename":     {{Name: "../secret", MediaType: "text/plain", Data: valid.Data}},
		"mime spoof":   {{Name: "x.png", MediaType: "image/png", Data: valid.Data}},
		"svg":          {{Name: "x.svg", MediaType: "image/svg+xml", Data: valid.Data}},
		"base64":       {{Name: "x.txt", MediaType: "text/plain", Data: "https://example.test/file"}},
		"invalid utf8": {{Name: "x.txt", MediaType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe})}},
		"binary text":  {{Name: "x.txt", MediaType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte{0, 1})}},
		"large text":   {{Name: "x.txt", MediaType: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", ChatMaxTextFileBytes+1)))}},
		"large file":   {{Name: "x.pdf", MediaType: "application/pdf", Data: strings.Repeat("A", base64.StdEncoding.EncodedLen(ChatMaxFileBytes)+1)}},
		"too many":     {valid, valid, valid, valid, valid},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateChatAttachments(input); err == nil {
				t.Fatal("invalid attachment accepted")
			}
		})
	}
	pdf := ChatAttachment{Name: "large.pdf", MediaType: "application/pdf", Data: base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\n" + strings.Repeat(" ", 3<<20)))}
	if _, err := ValidateChatAttachments([]ChatAttachment{pdf, pdf}); err == nil {
		t.Fatal("combined attachment budget not enforced")
	}
}
