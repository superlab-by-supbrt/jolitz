-- users get the identity at the OIDC provider they are bound to:
-- issuer and subject, both NULL until the first OIDC login. One
-- pair belongs to one user at most.
ALTER TABLE users ADD COLUMN oidc_issuer TEXT;
ALTER TABLE users ADD COLUMN oidc_subject TEXT;
CREATE UNIQUE INDEX users_oidc ON users(oidc_issuer, oidc_subject)
  WHERE oidc_issuer IS NOT NULL AND oidc_subject IS NOT NULL;
