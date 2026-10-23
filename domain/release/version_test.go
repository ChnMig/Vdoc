package release

import (
	"strings"
	"testing"
)

func TestParsePublishedVersions(t *testing.T) {
	tests := []struct {
		tag     string
		channel Channel
	}{
		{tag: "v0.0.0", channel: Stable},
		{tag: "v0.3.17", channel: Stable},
		{tag: "v1.20.300", channel: Stable},
		{tag: "v0.3.17-beta.0", channel: Beta},
		{tag: "v0.3.17-beta.10", channel: Beta},
		{tag: "v184467440737095516160.0.0-beta.184467440737095516160", channel: Beta},
		{tag: "v" + strings.Repeat("1", 251) + ".0.0", channel: Stable},
	}
	for _, test := range tests {
		t.Run(test.tag, func(t *testing.T) {
			version, ok := Parse(test.tag)
			if !ok {
				t.Fatalf("Parse(%q) rejected a published version", test.tag)
			}
			if version.Tag != test.tag || version.Channel != test.channel {
				t.Fatalf("Parse(%q) = %+v; want original tag and channel %q", test.tag, version, test.channel)
			}
			if got := ChannelOf(test.tag); got != test.channel {
				t.Fatalf("ChannelOf(%q) = %q; want %q", test.tag, got, test.channel)
			}
		})
	}
}

func TestParseRejectsUnpublishedVersions(t *testing.T) {
	tags := []string{
		"", "dev", "unknown", "abcdef0", "0.3.17", "V0.3.17", "v0.3", "v0.3.17.1",
		"v0.3.17-dirty", "v0.3.17-1-gabcdef0", "v0.3.17-rc.1", "v0.3.17-alpha.1",
		"v0.3.17-beta", "v0.3.17-beta.", "v0.3.17-beta.1.2", "v0.3.17-beta.-1",
		"v0.3.17-Beta.1", "v0.3.17+build.1", "v0.3.17-beta.1+build.1",
		"v00.3.17", "v0.03.17", "v0.3.017", "v0.3.17-beta.01", "v0.3.17-beta.00",
		"v-1.3.17", "v+1.3.17", " v0.3.17", "v0.3.17 ", "v0.3.17\n", "v１.3.17",
		"v" + strings.Repeat("1", 252) + ".0.0",
	}
	for _, tag := range tags {
		t.Run(tag, func(t *testing.T) {
			if version, ok := Parse(tag); ok {
				t.Fatalf("Parse(%q) accepted unpublished version %+v", tag, version)
			}
			if got := ChannelOf(tag); got != Development {
				t.Fatalf("ChannelOf(%q) = %q; want %q", tag, got, Development)
			}
		})
	}
}

func TestVersionCompareUsesNumericComponents(t *testing.T) {
	tests := []struct {
		name string
		low  string
		high string
	}{
		{name: "major", low: "v9.99.99", high: "v10.0.0"},
		{name: "minor", low: "v1.9.99", high: "v1.10.0"},
		{name: "patch", low: "v1.0.9", high: "v1.0.10"},
		{name: "beta sequence", low: "v0.3.17-beta.9", high: "v0.3.17-beta.10"},
		{name: "stable after beta", low: "v0.3.17-beta.999", high: "v0.3.17"},
		{name: "base version before channel", low: "v0.3.16", high: "v0.3.17-beta.0"},
		{name: "large major", low: "v184467440737095516159.99.99", high: "v184467440737095516160.0.0"},
		{name: "large minor", low: "v1.99999999999999999999.99", high: "v1.100000000000000000000.0"},
		{name: "large patch", low: "v1.0.184467440737095516159", high: "v1.0.184467440737095516160"},
		{name: "large beta sequence", low: "v1.0.0-beta.99999999999999999999", high: "v1.0.0-beta.100000000000000000000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			low, lowOK := Parse(test.low)
			high, highOK := Parse(test.high)
			if !lowOK || !highOK {
				t.Fatal("invalid version in test case")
			}
			if got := low.Compare(high); got >= 0 {
				t.Fatalf("%s.Compare(%s) = %d; want less than zero", test.low, test.high, got)
			}
			if got := high.Compare(low); got <= 0 {
				t.Fatalf("%s.Compare(%s) = %d; want greater than zero", test.high, test.low, got)
			}
			if low.Compare(low) != 0 || high.Compare(high) != 0 {
				t.Fatal("a version must compare equal to itself")
			}
		})
	}
}
