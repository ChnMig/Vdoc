package documentshare

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	commonvdoc "vdoc/common/vdoc"
)

const (
	minimumPasswordBytes = 12
	maximumPasswordBytes = 72
)

type ParsedPassword struct {
	value string
}

func ParsePassword(value string) (ParsedPassword, error) {
	if !utf8.ValidString(value) || len(value) < minimumPasswordBytes || len(value) > maximumPasswordBytes {
		return ParsedPassword{}, fmt.Errorf("%w: invalid document share password", commonvdoc.ErrInvalidArgument)
	}
	first, _ := utf8.DecodeRuneInString(value)
	last, _ := utf8.DecodeLastRuneInString(value)
	if unicode.IsSpace(first) || unicode.IsSpace(last) {
		return ParsedPassword{}, fmt.Errorf("%w: invalid document share password", commonvdoc.ErrInvalidArgument)
	}
	return ParsedPassword{value: value}, nil
}

func (password ParsedPassword) Bytes() []byte {
	return []byte(password.value)
}
