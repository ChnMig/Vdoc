package audit

import (
	"testing"
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func TestBuildAuditDefaultsActorResultAndCopiesMetadata(t *testing.T) {
	metadata := map[string]string{"key": "value"}
	log := Build(BuildParams{ID: "audit-a", Now: time.Now(), Action: "document.create", ResourceType: "document", Metadata: metadata})
	metadata["key"] = "changed"
	if log.ActorType != commonvdoc.AuditActorSystem || log.Metadata["result"] != "success" || log.Metadata["key"] != "value" {
		t.Fatalf("audit log = %+v", log)
	}
}
