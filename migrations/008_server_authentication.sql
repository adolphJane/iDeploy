ALTER TABLE servers ADD COLUMN auth_method TEXT NOT NULL DEFAULT 'agent'
    CHECK (auth_method IN ('agent', 'password', 'private_key'));

ALTER TABLE servers ADD COLUMN private_key_path TEXT NOT NULL DEFAULT '';

UPDATE servers
SET auth_method = 'password'
WHERE credential_ref <> '';
