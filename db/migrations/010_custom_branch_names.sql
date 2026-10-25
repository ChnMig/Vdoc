-- 自定义分支名称由领域层校验，保留历史分支记录和其他约束。
ALTER TABLE document_branches DROP CONSTRAINT IF EXISTS document_branches_feature_name_check;
ALTER TABLE document_branches DROP CONSTRAINT IF EXISTS api_contract_branches_feature_name_check;
