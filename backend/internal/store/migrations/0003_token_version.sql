-- token_version supports refresh-token revocation. Access/refresh tokens embed
-- the value at issue time; bumping it (on logout or disable) invalidates all
-- outstanding refresh tokens for the user at their next refresh.
ALTER TABLE users ADD COLUMN token_version integer NOT NULL DEFAULT 0;
