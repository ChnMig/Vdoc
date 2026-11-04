-- 保留 1–4 的类型限制；新增 read/write 能力，存量令牌不得自动扩大权限。
ALTER TABLE mcp_tokens DROP CONSTRAINT mcp_tokens_scopes_check;
ALTER TABLE mcp_tokens ADD CONSTRAINT mcp_tokens_scopes_check
  CHECK (scopes <@ ARRAY[1,2,3,4,5,6]::smallint[]);
