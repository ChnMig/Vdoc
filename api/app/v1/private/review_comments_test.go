package private

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	app "vdoc/appstore"
)

type reviewDraftInput struct {
	Fixture   privateTask5Fixture
	Document  *app.APIService
	BranchID  string
	Version   string
	Operation string
}

type reviewDraftActionInput struct {
	Fixture  privateTask5Fixture
	Document *app.APIService
	Draft    app.ContractDraft
	Action   string
}

func TestDocumentDraftReviewBodyRecordsTrimmedAuditComment(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	document, branchID := createReviewDocument(t, fixture, "review-body")

	changesDraft := createSubmittedReviewDraft(t, reviewDraftInput{Fixture: fixture, Document: document, BranchID: branchID, Version: "1.0.0", Operation: "reviewBodyChanges"})
	changesEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, draftReviewPath(reviewDraftActionInput{Fixture: fixture, Document: document, Draft: changesDraft, Action: "request-changes"}), fixture.adminToken, `{"comment":"  please clarify the error response  "}`))
	if changesEnvelope.Code != 200 || changesEnvelope.Status != "OK" {
		t.Fatalf("request changes response = code %d status %q body %s", changesEnvelope.Code, changesEnvelope.Status, changesEnvelope.Message)
	}
	var changes app.ContractDraft
	if err := json.Unmarshal(changesEnvelope.Detail, &changes); err != nil || changes.ReviewComment != "please clarify the error response" {
		t.Fatalf("request changes draft = %+v error=%v, want persisted review comment", changes, err)
	}
	changesAudit := requirePrivateReviewAudit(t, changesDraft.ID, "request-changes")
	if changesAudit.Metadata["review_comment"] != "please clarify the error response" {
		t.Fatalf("request changes audit metadata = %+v, want trimmed comment", changesAudit.Metadata)
	}

	resubmitEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, draftReviewPath(reviewDraftActionInput{Fixture: fixture, Document: document, Draft: changesDraft, Action: "submit"}), fixture.writerToken, ""))
	if resubmitEnvelope.Code != 200 || resubmitEnvelope.Status != "OK" {
		t.Fatalf("resubmit response = code %d status %q body %s", resubmitEnvelope.Code, resubmitEnvelope.Status, resubmitEnvelope.Message)
	}
	rejectEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, draftReviewPath(reviewDraftActionInput{Fixture: fixture, Document: document, Draft: changesDraft, Action: "reject"}), fixture.adminToken, `{"reason":"  does not match the contract  "}`))
	if rejectEnvelope.Code != 200 || rejectEnvelope.Status != "OK" {
		t.Fatalf("reject response = code %d status %q body %s", rejectEnvelope.Code, rejectEnvelope.Status, rejectEnvelope.Message)
	}
	var rejected app.ContractDraft
	if err := json.Unmarshal(rejectEnvelope.Detail, &rejected); err != nil || rejected.ReviewComment != "does not match the contract" {
		t.Fatalf("rejected draft = %+v error=%v, want persisted reason", rejected, err)
	}
	rejectAudit := requirePrivateReviewAudit(t, changesDraft.ID, "reject")
	if rejectAudit.Metadata["review_comment"] != "does not match the contract" {
		t.Fatalf("reject audit metadata = %+v, want reason alias", rejectAudit.Metadata)
	}

	publishDraft := createSubmittedReviewDraft(t, reviewDraftInput{Fixture: fixture, Document: document, BranchID: branchID, Version: "1.0.1", Operation: "reviewBodyApprove"})
	approveEnvelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, draftReviewPath(reviewDraftActionInput{Fixture: fixture, Document: document, Draft: publishDraft, Action: "approve"}), fixture.adminToken, `{"comment":"  approved note wins  ","reason":"ignored alias"}`))
	if approveEnvelope.Code != 200 || approveEnvelope.Status != "OK" {
		t.Fatalf("approve response = code %d status %q body %s", approveEnvelope.Code, approveEnvelope.Status, approveEnvelope.Message)
	}
	var version app.ContractVersion
	if err := json.Unmarshal(approveEnvelope.Detail, &version); err != nil {
		t.Fatalf("decode approved version: %v", err)
	}
	approveAudit := requirePrivateReviewAudit(t, publishDraft.ID, "approve")
	if approveAudit.Metadata["review_comment"] != "approved note wins" || approveAudit.Metadata["version_id"] != version.ID {
		t.Fatalf("approve audit metadata = %+v, want comment precedence and published version", approveAudit.Metadata)
	}
	publishedDraft, err := app.DefaultStore().Draft(fixture.adminUser.ID, fixture.project.ID, document.ID, publishDraft.ID)
	if err != nil || publishedDraft.ReviewComment != "approved note wins" {
		t.Fatalf("published draft review comment = (%+v, %v)", publishedDraft, err)
	}
}

func TestDocumentDraftReviewBodyRejectsOverLimitComment(t *testing.T) {
	fixture := setupPrivateTask5Project(t)
	document, branchID := createReviewDocument(t, fixture, "review-limit")
	draft := createSubmittedReviewDraft(t, reviewDraftInput{Fixture: fixture, Document: document, BranchID: branchID, Version: "1.0.0", Operation: "reviewLimit"})

	body := `{"comment":"` + strings.Repeat("x", 1001) + `"}`
	envelope := decodePrivateEnvelope(t, performPrivateJSON(fixture.router, http.MethodPost, draftReviewPath(reviewDraftActionInput{Fixture: fixture, Document: document, Draft: draft, Action: "approve"}), fixture.adminToken, body))
	if envelope.Code != 400 || envelope.Status != "INVALID_ARGUMENT" {
		t.Fatalf("over-limit response = code %d status %q body %s", envelope.Code, envelope.Status, envelope.Message)
	}
}

func createReviewDocument(t *testing.T, fixture privateTask5Fixture, name string) (*app.APIService, string) {
	t.Helper()
	document, err := app.DefaultStore().CreateDocument(fixture.adminUser.ID, fixture.project.ID, name, app.DocumentTypeOpenAPI, "apis/"+name+".yaml", "")
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	branches, err := app.DefaultStore().ListBranches(fixture.adminUser.ID, fixture.project.ID, document.ID)
	if err != nil {
		t.Fatalf("list branches: %v", err)
	}
	return document, branchesByName(branches)["dev"].ID
}

func createSubmittedReviewDraft(t *testing.T, input reviewDraftInput) app.ContractDraft {
	t.Helper()
	body := `{"branch_id":"` + input.BranchID + `","version_name":"` + input.Version + `","schema_content":` + jsonString(privateTestOpenAPI(input.Operation)) + `}`
	createEnvelope := decodePrivateEnvelope(t, performPrivateJSON(input.Fixture.router, http.MethodPost, "/api/v1/private/projects/"+input.Fixture.project.ID+"/documents/"+input.Document.ID+"/drafts", input.Fixture.writerToken, body))
	if createEnvelope.Code != 200 || createEnvelope.Status != "OK" {
		t.Fatalf("create review draft response = code %d status %q body %s", createEnvelope.Code, createEnvelope.Status, createEnvelope.Message)
	}
	var draft app.ContractDraft
	if err := json.Unmarshal(createEnvelope.Detail, &draft); err != nil {
		t.Fatalf("decode review draft: %v", err)
	}
	submitEnvelope := decodePrivateEnvelope(t, performPrivateJSON(input.Fixture.router, http.MethodPost, draftReviewPath(reviewDraftActionInput{Fixture: input.Fixture, Document: input.Document, Draft: draft, Action: "submit"}), input.Fixture.writerToken, ""))
	if submitEnvelope.Code != 200 || submitEnvelope.Status != "OK" {
		t.Fatalf("submit review draft response = code %d status %q body %s", submitEnvelope.Code, submitEnvelope.Status, submitEnvelope.Message)
	}
	return draft
}

func draftReviewPath(input reviewDraftActionInput) string {
	return "/api/v1/private/projects/" + input.Fixture.project.ID + "/documents/" + input.Document.ID + "/drafts/" + input.Draft.ID + "/" + input.Action
}

func requirePrivateReviewAudit(t *testing.T, draftID, action string) *app.AuditLog {
	t.Helper()
	for _, audit := range app.DefaultStore().AuditLogsForTest() {
		if (audit.Action == "contract_draft.review" || audit.Action == "markdown_draft.review") && audit.ResourceID == draftID && audit.Metadata["review_action"] == action {
			return audit
		}
	}
	t.Fatalf("missing review audit draft=%s action=%s logs=%+v", draftID, action, app.DefaultStore().AuditLogsForTest())
	return nil
}
