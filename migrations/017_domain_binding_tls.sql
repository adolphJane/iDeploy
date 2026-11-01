ALTER TABLE domain_bindings ADD COLUMN certificate_path TEXT NOT NULL DEFAULT '';
ALTER TABLE domain_bindings ADD COLUMN certificate_key_path TEXT NOT NULL DEFAULT '';
ALTER TABLE domain_bindings ADD COLUMN ingress_mode TEXT NOT NULL DEFAULT 'caddy' CHECK(ingress_mode IN ('caddy','nginx'));
