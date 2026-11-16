package documentimage

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	_ "golang.org/x/image/webp"
)

const MaxBytes = 4 << 20
const MaxPixels = 40_000_000
const ReferencePrefix = "vdoc-asset:"

var assetIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Asset struct {
	AssetID     string `json:"asset_id"`
	Reference   string `json:"reference"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	ByteLength  int    `json:"byte_length"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
}

func ValidID(id string) bool { return assetIDPattern.MatchString(id) }

// Validate 不相信扩展名或客户端 MIME，先限制输入大小，再解码尺寸和像素。
func Validate(filename, encoded string) (Asset, []byte, error) {
	if filename == "" || len(filename) > 255 || !utf8.ValidString(filename) || strings.ContainsAny(filename, `/\\`) || strings.IndexFunc(filename, unicode.IsControl) >= 0 {
		return Asset{}, nil, fmt.Errorf("image filename must be a plain filename of at most 255 bytes")
	}
	if len(encoded) == 0 || len(encoded) > base64.StdEncoding.EncodedLen(MaxBytes) {
		return Asset{}, nil, fmt.Errorf("image must be at most 4 MiB")
	}
	body, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(body) == 0 || len(body) > MaxBytes {
		return Asset{}, nil, fmt.Errorf("image must be valid base64 and at most 4 MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(body))
	contentTypes := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}
	if err != nil || contentTypes[format] == "" {
		return Asset{}, nil, fmt.Errorf("image must be a valid PNG, JPEG, GIF or WebP")
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MaxPixels {
		return Asset{}, nil, fmt.Errorf("image must contain at most 40 million pixels")
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(body))
	if err != nil || decodedFormat != format || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return Asset{}, nil, fmt.Errorf("image data is corrupt or unsupported")
	}
	id := fmt.Sprintf("%x", sha256.Sum256(body))
	return Asset{AssetID: id, Reference: ReferencePrefix + id, Filename: filename, ContentType: contentTypes[format], ByteLength: len(body), Width: config.Width, Height: config.Height}, body, nil
}

func Referenced(content, id string) bool {
	if !ValidID(id) {
		return false
	}
	ids, err := References(content)
	if err != nil {
		return false
	}
	for _, value := range ids {
		if value == id {
			return true
		}
	}
	return false
}

func References(content string) ([]string, error) {
	// 只授权实际图片节点；代码示例、普通链接和未使用的引用定义不能开放未发布图片。
	ids := []string{}
	seen := map[string]bool{}
	document := goldmark.DefaultParser().Parse(text.NewReader([]byte(content)))
	err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if image, ok := node.(*ast.Image); entering && ok {
			// Match CommonMark rendering: destinations resolve punctuation escapes
			// and character references, while percent escapes remain literal.
			destination := string(util.URLEscape(image.Destination, true))
			if !strings.HasPrefix(destination, ReferencePrefix) {
				return ast.WalkContinue, nil
			}
			id := strings.TrimPrefix(destination, ReferencePrefix)
			if !ValidID(id) {
				return ast.WalkStop, fmt.Errorf("invalid managed image reference")
			}
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
		return ast.WalkContinue, nil
	})
	return ids, err
}
