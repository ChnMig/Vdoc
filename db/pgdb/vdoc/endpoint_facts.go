package vdoc

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vdoc/db/pgdb"
	domain "vdoc/domain/vdoc"
)

func (r *Repository) RefreshEndpointFacts(ctx context.Context, versionID, rawHash string, parserVersion int, parsedHash string, endpoints []domain.Endpoint) error {
	return r.transaction(ctx, func(tx *gorm.DB) error {
		var version DocumentVersion
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", versionID).First(&version).Error; err != nil {
			return mapRecordLookupError(err)
		}
		if version.RawSchemaHash != rawHash {
			return fmt.Errorf("%w: version source changed", domain.ErrFailedPrecondition)
		}
		if version.ParserVersion >= parserVersion {
			return nil
		}
		var stored []APIEndpoint
		if err := tx.WithContext(ctx).Where("document_version_id = ?", versionID).Find(&stored).Error; err != nil {
			return err
		}
		byID := map[string]domain.Endpoint{}
		for _, endpoint := range endpoints {
			if endpoint.ContractVersionID != versionID {
				return domain.ErrInvalidArgument
			}
			byID[endpoint.ID] = endpoint
		}
		for _, old := range stored {
			next, ok := byID[domainID(old.ID)]
			if !ok || next.Method != codeToMethod(old.Method) || next.Path != old.Path {
				return fmt.Errorf("%w: endpoint identity changed", domain.ErrFailedPrecondition)
			}
		}
		for _, endpoint := range endpoints {
			method, ok := methodToCode(endpoint.Method)
			if !ok {
				return domain.ErrInvalidArgument
			}
			model := &APIEndpoint{Base: pgdb.Base{ID: endpoint.ID, CreatedAt: endpoint.CreatedAt, UpdatedAt: endpoint.UpdatedAt}, DocumentVersionID: versionID, DocumentID: version.DocumentID, BranchID: version.BranchID, Method: method, Path: endpoint.Path, OperationID: stringPtr(endpoint.OperationID), Summary: stringPtr(endpoint.Summary), Tags: pgdb.StringArray(endpoint.Tags), Deprecated: endpoint.Deprecated, RequestHash: endpoint.Hash, ResponseHash: endpoint.Hash, EndpointHash: endpoint.Hash}
			if err := tx.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"operation_id", "summary", "tags", "deprecated", "request_hash", "response_hash", "endpoint_hash"})}).Create(model).Error; err != nil {
				return err
			}
			detail := &APIEndpointDetail{EndpointID: endpoint.ID, ParametersJSON: pgdb.NewJSONB(endpoint.Parameters, "[]"), RequestBodyJSON: nullableJSON(endpoint.RequestBody), ResponsesJSON: pgdb.NewJSONB(endpoint.Responses, "{}"), SecurityJSON: nullableJSON(endpoint.Security), ServersJSON: nullableJSON(endpoint.Servers), NormalizedOperationJSON: pgdb.NewJSONB(endpoint.NormalizedOperation, "{}"), SchemaRefsJSON: nullableJSON(endpoint.SchemaRefs)}
			// UpdateAll 会跳过带数据库默认值的 JSONB 列；必须显式更新全部派生详情。
			if err := tx.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "endpoint_id"}}, DoUpdates: clause.AssignmentColumns([]string{"parameters_json", "request_body_json", "responses_json", "security_json", "servers_json", "normalized_operation_json", "schema_refs_json", "updated_at"})}).Create(detail).Error; err != nil {
				return err
			}
		}
		return tx.WithContext(ctx).Model(&DocumentVersion{}).Where("id = ?", versionID).UpdateColumns(map[string]any{"parser_version": parserVersion, "parsed_schema_hash": parsedHash, "endpoint_count": len(endpoints)}).Error
	})
}
