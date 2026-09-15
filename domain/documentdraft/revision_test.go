package documentdraft

import (
	"testing"
	"time"
)

func TestRevisionSurvivesTimestampRoundTripAndContentHydration(t *testing.T) {
	draft := &ContractDraft{ID: "draft", VersionName: "1.0.0", RawSchemaHash: "content-hash", Status: 1, UpdatedAt: time.Now()}
	revision := draft.Revision()
	draft.UpdatedAt = draft.UpdatedAt.Add(time.Second).Truncate(time.Microsecond)
	draft.RawSchema = "loaded lazily from object storage"
	draft.NormalizedSchema = "refreshed parser cache"
	if draft.Revision() != revision {
		t.Fatal("timestamps and hydrated caches must not invalidate an editor's revision")
	}
	draft.SourceGitCommitID = "new-commit"
	if draft.Revision() == revision {
		t.Fatal("an edited source commit must invalidate the old revision")
	}
}

func TestReviewRevisionBindsSubmissionAndIgnoresCacheIdentity(t *testing.T) {
	draft := &ContractDraft{ID: "draft", VersionName: "1", RawSchemaHash: "hash", Status: 1}
	now := time.Now()
	if err := Submit(draft, now); err != nil {
		t.Fatal(err)
	}
	first := draft.ReviewRevision()
	draft.UpdatedAt = now.Add(time.Hour)
	draft.RawSchema = "hydrated"
	roundTrip := draft.SubmittedAt.UTC().Truncate(time.Microsecond)
	draft.SubmittedAt = &roundTrip
	if first != draft.ReviewRevision() {
		t.Fatal("hydration or database round trip invalidated review")
	}
	if _, err := Review(draft, "request-changes", now); err != nil {
		t.Fatal(err)
	}
	if err := Submit(draft, now); err != nil {
		t.Fatal(err)
	}
	if first == draft.ReviewRevision() {
		t.Fatal("resubmission at the same clock time reused an old review")
	}
}
