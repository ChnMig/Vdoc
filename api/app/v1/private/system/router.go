package system

import (
	"context"
	"errors"
	"time"

	"vdoc/api/app/v1/private/shared"
	"vdoc/api/response"
	systemsvc "vdoc/services/system"
	"vdoc/utils/log"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type updateChecker interface {
	Check(context.Context, string) (systemsvc.UpdateResult, error)
}

type versionDTO struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
	Channel   string `json:"channel"`
}

type updatesDTO struct {
	CurrentVersion  string     `json:"current_version"`
	Channel         string     `json:"channel"`
	LatestVersion   *string    `json:"latest_version"`
	UpdateAvailable bool       `json:"update_available"`
	ReleaseURL      *string    `json:"release_url"`
	PublishedAt     *time.Time `json:"published_at"`
}

var checker = systemsvc.NewChecker()

func RegisterRoutes(private *gin.RouterGroup) {
	registerRoutes(private, systemsvc.CurrentVersion, checker)
}

func registerRoutes(private *gin.RouterGroup, version func() systemsvc.VersionInfo, checker updateChecker) {
	if private == nil {
		return
	}
	private.GET("/system/version", func(c *gin.Context) {
		if _, ok := shared.CurrentUserID(c); !ok {
			return
		}
		info := version()
		response.ReturnOk(c, versionDTO{Version: info.Version, GitCommit: info.GitCommit, BuildTime: info.BuildTime, Channel: string(info.Channel)})
	})
	private.GET("/system/updates", func(c *gin.Context) {
		if _, ok := shared.CurrentUserID(c); !ok {
			return
		}
		result, err := checker.Check(c.Request.Context(), version().Version)
		if err != nil {
			switch {
			case errors.Is(err, systemsvc.ErrUnsupportedVersion):
				response.ReturnError(c, response.FAILED_PRECONDITION, "当前部署为开发构建，无法检查发布更新")
			case errors.Is(err, context.Canceled):
				response.ReturnError(c, response.CANCELLED, "更新检查已取消")
			case errors.Is(err, context.DeadlineExceeded):
				response.ReturnError(c, response.DEADLINE_EXCEEDED, "更新检查超时，请稍后重试")
			default:
				log.WithRequest(c).Warn("检查发布更新失败", zap.Error(err))
				response.ReturnError(c, response.UNAVAILABLE, "暂时无法连接更新服务，请稍后重试")
			}
			return
		}
		response.ReturnOk(c, updatesDTO{
			CurrentVersion: result.CurrentVersion, Channel: string(result.Channel),
			LatestVersion: result.LatestVersion, UpdateAvailable: result.UpdateAvailable,
			ReleaseURL: result.ReleaseURL, PublishedAt: result.PublishedAt,
		})
	})
}
