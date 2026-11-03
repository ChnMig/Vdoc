-- 资源标识既有 UUID，也有文档仓库提交的完整 SHA-256；不能强制转成 UUID。
-- 旧 UUID 保持领域层使用的无连字符形式，确保升级后的资源筛选仍能匹配旧日志。
ALTER TABLE audit_logs
    ALTER COLUMN resource_id TYPE text USING replace(resource_id::text, '-', '');
