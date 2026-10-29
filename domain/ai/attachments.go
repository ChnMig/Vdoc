package ai

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ChatMaxAttachments          = 4
	ChatMaxFileBytes            = 4 << 20
	ChatMaxTotalAttachmentBytes = 6 << 20
	ChatMaxTextFileBytes        = 100 << 10
)

// 附件随消息保存；只接受内联内容，避免模型或服务端主动抓取任意 URL。
type ChatAttachment struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

func ValidateChatAttachments(input []ChatAttachment) ([]ChatAttachment, error) {
	if len(input) > ChatMaxAttachments {
		return nil, fmt.Errorf("at most %d attachments are allowed", ChatMaxAttachments)
	}
	var total int
	out := make([]ChatAttachment, 0, len(input))
	for _, attachment := range input {
		attachment.Name = strings.TrimSpace(attachment.Name)
		if attachment.Name == "" || utf8.RuneCountInString(attachment.Name) > 160 || strings.ContainsAny(attachment.Name, "/\\") || strings.IndexFunc(attachment.Name, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("invalid attachment filename")
		}
		if len(attachment.Data) > base64.StdEncoding.EncodedLen(ChatMaxFileBytes) {
			return nil, fmt.Errorf("each attachment must be 4 MB or smaller")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(attachment.Data)
		if err != nil || len(data) == 0 || len(data) > ChatMaxFileBytes {
			return nil, fmt.Errorf("invalid attachment data or size")
		}
		total += len(data)
		if total > ChatMaxTotalAttachmentBytes {
			return nil, fmt.Errorf("attachments must total 6 MB or less")
		}
		switch attachment.MediaType {
		case "image/png", "image/jpeg", "image/webp", "image/gif", "application/pdf":
			if http.DetectContentType(data) != attachment.MediaType {
				return nil, fmt.Errorf("attachment contents do not match its file type")
			}
		case "text/plain":
			if len(data) > ChatMaxTextFileBytes || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
				return nil, fmt.Errorf("text files must be UTF-8 and 100 KB or smaller")
			}
		default:
			return nil, fmt.Errorf("supported attachments: PNG, JPEG, WebP, GIF, PDF and UTF-8 text files")
		}
		out = append(out, attachment)
	}
	return out, nil
}

func (a ChatAttachment) DataURL() string {
	return "data:" + a.MediaType + ";base64," + a.Data
}

func (a ChatAttachment) Text() string {
	data, _ := base64.StdEncoding.DecodeString(a.Data)
	return string(data)
}
