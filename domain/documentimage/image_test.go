package documentimage

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestValidateChecksActualFormatAndFullDecoding(t *testing.T) {
	picture := image.NewRGBA(image.Rect(0, 0, 2, 3))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	for _, tc := range []struct {
		name   string
		mime   string
		encode func(*bytes.Buffer) error
	}{
		{"png", "image/png", func(b *bytes.Buffer) error { return png.Encode(b, picture) }},
		{"jpeg", "image/jpeg", func(b *bytes.Buffer) error { return jpeg.Encode(b, picture, nil) }},
		{"gif", "image/gif", func(b *bytes.Buffer) error { return gif.Encode(b, picture, nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := tc.encode(&b); err != nil {
				t.Fatal(err)
			}
			asset, body, err := Validate("picture."+tc.name, base64.StdEncoding.EncodeToString(b.Bytes()))
			if err != nil || asset.ContentType != tc.mime || asset.Width != 2 || asset.Height != 3 || !ValidID(asset.AssetID) || asset.Reference != ReferencePrefix+asset.AssetID || !bytes.Equal(body, b.Bytes()) {
				t.Fatalf("asset=%+v err=%v", asset, err)
			}
			if _, _, err = Validate("picture."+tc.name, base64.StdEncoding.EncodeToString(b.Bytes()[:len(b.Bytes())/2])); err == nil {
				t.Fatal("accepted truncated image")
			}
		})
	}
	// golang.org/x/image 的 gopher-doc WebP 测试样例，验证真实像素解码。
	const webp = "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA=="
	asset, _, err := Validate("image.webp", webp)
	if err != nil || asset.ContentType != "image/webp" || asset.Width != 75 || asset.Height != 100 {
		t.Fatalf("WebP=%+v %v", asset, err)
	}
}

func TestValidateRejectsOversizeSpoofingAndUnsafeNames(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(b.Bytes())
	for _, name := range []string{"", "../image.png", `a\b.png`, "bad\n.png", strings.Repeat("a", 256)} {
		if _, _, err := Validate(name, encoded); err == nil {
			t.Errorf("accepted filename %q", name)
		}
	}
	for _, invalid := range []string{"", "not-base64", base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)), base64.StdEncoding.EncodeToString([]byte("RIFF1234WEBPVP8 ")), strings.Repeat("A", base64.StdEncoding.EncodedLen(MaxBytes)+4)} {
		if _, _, err := Validate("spoof.png", invalid); err == nil {
			t.Error("accepted invalid image bytes")
		}
	}
	huge := bytes.Clone(b.Bytes())
	binary.BigEndian.PutUint32(huge[16:20], 7000)
	binary.BigEndian.PutUint32(huge[20:24], 7000)
	binary.BigEndian.PutUint32(huge[29:33], crc32.ChecksumIEEE(huge[12:29]))
	if _, _, err := Validate("huge.png", base64.StdEncoding.EncodeToString(huge)); err == nil || !strings.Contains(err.Error(), "40 million") {
		t.Fatalf("dimension limit: %v", err)
	}
}

func TestReferencedRequiresActualImageInMarkdown(t *testing.T) {
	id := strings.Repeat("a", 64)
	ref := ReferencePrefix + id
	for _, value := range []string{"![image](" + ref + ")", "![image](<" + ref + "> \"title\")", "![image][ref]\n\n[ref]: " + ref,
		"![entity](vdoc-asset&#58;" + id + ")", "![hex](vdoc-asset&#x3a;" + id + ")", "![named](vdoc-asset&colon;" + id + ")",
		"![escape](vdoc-asset\\:" + id + ")", "![scheme](vdoc&#45;asset:" + id + ")", "![image][entity]\n\n[entity]: vdoc-asset&#58;" + id,
	} {
		if !Referenced(value, id) {
			t.Errorf("image not found in %q", value)
		}
	}
	for _, value := range []string{ref, "[link](" + ref + ")", "`![image](" + ref + ")`", "```md\n![image](" + ref + ")\n```", "[unused]: " + ref, "![image](" + ref + "b)", "![encoded](vdoc-asset%3A" + id + ")"} {
		if Referenced(value, id) {
			t.Errorf("non-image opened access: %q", value)
		}
	}
}

func TestReferencesRejectsMalformedNormalizedManagedDestinations(t *testing.T) {
	for _, value := range []string{"![entity](vdoc-asset&#58;not-a-hash)", "![escape](vdoc-asset\\:invalid)", "![scheme](vdoc&#45;asset:invalid)"} {
		if _, err := References(value); err == nil {
			t.Fatalf("malformed rendered managed reference was accepted: %q", value)
		}
	}
}
