package release

import (
	"regexp"
	"strings"
)

type Channel string

const (
	Beta        Channel = "beta"
	Stable      Channel = "release"
	Development Channel = "development"
)

var versionPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-beta\.(0|[1-9][0-9]*))?$`)

type Version struct {
	Tag     string
	Channel Channel
	numbers [4]string
}

// Parse 只接受实际发布使用的稳定版和 beta 标签；提交号、dirty 和 RC 不冒充发布版。
func Parse(tag string) (Version, bool) {
	if len(tag) > 256 {
		return Version{}, false
	}
	parts := versionPattern.FindStringSubmatch(tag)
	if parts == nil {
		return Version{}, false
	}
	v := Version{Tag: tag, Channel: Stable}
	copy(v.numbers[:], parts[1:])
	if parts[4] != "" {
		v.Channel = Beta
	}
	return v, true
}

func ChannelOf(tag string) Channel {
	if version, ok := Parse(tag); ok {
		return version.Channel
	}
	return Development
}

func (v Version) Compare(other Version) int {
	for i := 0; i < 3; i++ {
		if result := compareNumber(v.numbers[i], other.numbers[i]); result != 0 {
			return result
		}
	}
	if v.Channel != other.Channel {
		if v.Channel == Stable {
			return 1
		}
		return -1
	}
	return compareNumber(v.numbers[3], other.numbers[3])
}

// 数字字符串按位数比较，避免版本号超过机器整数范围后溢出。
func compareNumber(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return strings.Compare(a, b)
}
