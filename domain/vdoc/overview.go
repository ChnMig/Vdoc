package vdoc

import (
	"context"
	"time"
)

type DocumentOverview struct {
	VersionCount       int64
	EndpointCount      int64
	LatestVersion      *ContractVersion
	PublishedBranchIDs []string
	HasReviewedDraft   bool
	RawSizeBytes       int64
	RawLineCount       *int64
}

type OverviewRepository interface {
	ReadDocumentOverview(context.Context, string) (*DocumentOverview, error)
	SaveVersionContentStats(context.Context, string, string, int64, int64) error
	ReadLatestMCPRead(context.Context, string, string, []string) (*time.Time, error)
}
