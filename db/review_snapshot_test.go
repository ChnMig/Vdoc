package db_test

import (
	"testing"
	app "vdoc/services/vdoc"
)

func reviewInputForTest(t *testing.T, store *app.Store, actorID, projectID, documentID, draftID string) app.DraftReviewInput {
	t.Helper()
	draft, err := store.Draft(actorID, projectID, documentID, draftID)
	if err != nil {
		t.Fatal(err)
	}
	return app.DraftReviewInput{ExpectedReviewRevision: draft.ReviewRevision()}
}
