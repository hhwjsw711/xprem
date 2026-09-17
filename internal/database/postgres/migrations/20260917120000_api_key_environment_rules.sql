-- +goose Up
CREATE TABLE api_key_environment_rules (
    api_key_id BIGINT NOT NULL,
    -- An environment name, or a name with "*" standing for any run of characters.
    pattern VARCHAR(255) NOT NULL,
    PRIMARY KEY (api_key_id, pattern),
    CONSTRAINT fk_api_key_environment_rules_api_key FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE CASCADE
);

-- +goose Down
-- Refuse a downgrade that would let restricted tokens read every environment again.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM api_key_environment_rules r JOIN api_keys k ON k.id = r.api_key_id WHERE k.revoked_at IS NULL) THEN
        RAISE EXCEPTION 'Cannot downgrade while live tokens have Environment rules; remove those rules first';
    END IF;
    DROP TABLE api_key_environment_rules;
END $$;
-- +goose StatementEnd
