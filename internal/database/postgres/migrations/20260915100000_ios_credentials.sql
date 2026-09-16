-- +goose Up
CREATE TABLE ios_certificates (
    id UUID PRIMARY KEY,
    sealed_certificate TEXT NOT NULL,
    sealed_certificate_password TEXT NOT NULL,
    common_name TEXT NOT NULL,
    serial_number TEXT NOT NULL,
    fingerprint_sha1 TEXT NOT NULL UNIQUE,
    certificate_type TEXT NOT NULL CHECK (certificate_type IN ('distribution', 'development')),
    team_id TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('generated', 'uploaded')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ios_signing_settings (
    app_identifier_id UUID PRIMARY KEY REFERENCES app_identifiers(id) ON DELETE CASCADE,
    mode TEXT NOT NULL CHECK (mode IN ('automatic', 'certificate')),
    -- NULL in mode 'certificate' means the selected certificate was deleted.
    certificate_id UUID REFERENCES ios_certificates(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (mode = 'certificate' OR certificate_id IS NULL)
);

CREATE TABLE app_store_connect_api_keys (
    id UUID PRIMARY KEY,
    app_id UUID NOT NULL UNIQUE REFERENCES apps(id) ON DELETE CASCADE,
    key_id TEXT NOT NULL,
    issuer_id TEXT NOT NULL,
    sealed_private_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE app_store_connect_api_keys;
DROP TABLE ios_signing_settings;
DROP TABLE ios_certificates;
