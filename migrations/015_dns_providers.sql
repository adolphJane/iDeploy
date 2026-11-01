-- Existing connections are AliDNS; preserve their Keychain references and domain associations.
ALTER TABLE dns_connections ADD COLUMN provider TEXT NOT NULL DEFAULT 'aliyun'
    CHECK (provider IN ('aliyun', 'cloudflare'));
