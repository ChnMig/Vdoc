package system

import (
	"testing"

	"vdoc/domain/release"
)

func TestCurrentVersionDefaultsToDevelopment(t *testing.T) {
	original := deployedVersion.Load()
	t.Cleanup(func() { deployedVersion.Store(original) })
	deployedVersion.Store(nil)

	got := CurrentVersion()
	want := VersionInfo{Version: "dev", GitCommit: "unknown", BuildTime: "unknown", Channel: release.Development}
	if got != want {
		t.Fatalf("CurrentVersion() = %#v, want %#v", got, want)
	}
}

func TestBuildInfoReportsActualMetadataAndChannel(t *testing.T) {
	original := deployedVersion.Load()
	t.Cleanup(func() { deployedVersion.Store(original) })

	for _, test := range []struct {
		version string
		channel release.Channel
	}{
		{version: "v0.3.17-beta.4", channel: release.Beta},
		{version: "v0.3.17", channel: release.Stable},
		{version: "v0.3.17-2-gabcdef0-dirty", channel: release.Development},
		{version: "dev", channel: release.Development},
	} {
		t.Run(test.version, func(t *testing.T) {
			SetBuildInfo(test.version, "abcdef0123456789", "2026-10-23T12:00:00Z")
			want := VersionInfo{
				Version: test.version, GitCommit: "abcdef0123456789", BuildTime: "2026-10-23T12:00:00Z", Channel: test.channel,
			}
			got := CurrentVersion()
			if got != want {
				t.Fatalf("CurrentVersion() = %#v, want %#v", got, want)
			}
			got.Version = "modified by caller"
			if next := CurrentVersion(); next != want {
				t.Fatalf("caller changed stored build info: %#v", next)
			}
		})
	}
}
