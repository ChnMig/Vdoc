package documentversion

import (
	"errors"
	"testing"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func TestBuildPromoteDraftEnforcesSourceTargetAndBaseInvariants(t *testing.T) {
	promote, err := BuildPromoteDraft(PromoteInput{SourceBranchID: "dev", TargetBranchID: "test", VersionName: "1.0.0-test", Changelog: "promote"}, PromoteSource{SourceVersionID: "version-dev", SourceRawSchema: "openapi: 3.0.0", SourceGitCommitID: "abc", BaseVersionID: "version-test-base", TargetBranchExists: true})
	if err != nil {
		t.Fatalf("BuildPromoteDraft() error = %v", err)
	}
	if promote.BranchID != "test" || promote.SourceBranchID != "dev" || promote.SourceVersionID != "version-dev" || promote.BaseVersionID != "version-test-base" || promote.SourceGitCommitID != "abc" {
		t.Fatalf("promote draft = %+v", promote)
	}
	if _, err := BuildPromoteDraft(PromoteInput{SourceBranchID: "dev", TargetBranchID: "dev", VersionName: "same"}, PromoteSource{SourceVersionID: "version-dev", SourceRawSchema: "schema", TargetBranchExists: true}); !errors.Is(err, commonvdoc.ErrFailedPrecondition) {
		t.Fatalf("same branch error = %v, want failed precondition", err)
	}
	if _, err := BuildPromoteDraft(PromoteInput{SourceBranchID: "dev", TargetBranchID: "test", VersionName: "missing"}, PromoteSource{TargetBranchExists: true}); !errors.Is(err, commonvdoc.ErrNotFound) {
		t.Fatalf("missing source error = %v, want not found", err)
	}
}

func TestPublishFromDraftRequiresSubmittedDraft(t *testing.T) {
	now := time.Now()
	_, err := PublishFromDraft(PublishParams{ID: "version-a", Draft: DraftSnapshot{ID: "draft-a", Status: commonvdoc.DraftStatusDraft}, PublishedBy: "admin", Now: now})
	if !errors.Is(err, commonvdoc.ErrFailedPrecondition) {
		t.Fatalf("PublishFromDraft(draft) error = %v, want failed precondition", err)
	}
	version, err := PublishFromDraft(PublishParams{ID: "version-a", Draft: DraftSnapshot{ID: "draft-a", ProjectID: "project-a", DocumentID: "doc-a", BranchID: "branch-a", VersionName: "1.0.0", Status: commonvdoc.DraftStatusSubmitted}, PublishedBy: "admin", Now: now})
	if err != nil {
		t.Fatalf("PublishFromDraft(submitted) error = %v", err)
	}
	if version.Status != commonvdoc.VersionStatusPublished || version.DraftID != "draft-a" || version.ServiceID != "doc-a" || version.PublishedBy != "admin" {
		t.Fatalf("version = %+v", version)
	}
}
