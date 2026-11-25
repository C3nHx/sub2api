-- 用户自助用量 Hub 配置表：按用户隔离独立只读 Token（仅存 SHA-256 哈希）与自定义外部 Magpie 数据源（密钥 AES 加密存储）。
CREATE TABLE IF NOT EXISTS user_usage_hub_configs (
    user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    token_hash VARCHAR(64),
    token_masked VARCHAR(64) NOT NULL DEFAULT '',
    include_sub2api BOOLEAN NOT NULL DEFAULT TRUE,
    include_disabled BOOLEAN NOT NULL DEFAULT FALSE,
    expose_reset_credits BOOLEAN NOT NULL DEFAULT FALSE,
    magpie_sources JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_usage_hub_configs_token_hash
    ON user_usage_hub_configs(token_hash)
    WHERE token_hash IS NOT NULL AND token_hash <> '';
