package system

import (
	"sync/atomic"

	"vdoc/domain/release"
)

type VersionInfo struct {
	Version   string
	GitCommit string
	BuildTime string
	Channel   release.Channel
}

var deployedVersion atomic.Pointer[VersionInfo]

// SetBuildInfo 在 main 启动时接收同一份链接器元数据，不从镜像标签或前端版本猜测。
func SetBuildInfo(version, gitCommit, buildTime string) {
	deployedVersion.Store(&VersionInfo{
		Version: version, GitCommit: gitCommit, BuildTime: buildTime,
		Channel: release.ChannelOf(version),
	})
}

func CurrentVersion() VersionInfo {
	if value := deployedVersion.Load(); value != nil {
		return *value
	}
	return VersionInfo{Version: "dev", GitCommit: "unknown", BuildTime: "unknown", Channel: release.Development}
}
