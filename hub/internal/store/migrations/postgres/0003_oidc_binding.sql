-- users get the identity at the OIDC provider they are bound to:
-- issuer and subject, both NULL until the first OIDC login. One
-- pair belongs to one user at most; NULLs are distinct in a
-- unique index, so unbound users do not collide.
ALTER TABLE users ADD COLUMN oidc_issuer TEXT COLLATE "C";
ALTER TABLE users ADD COLUMN oidc_subject TEXT COLLATE "C";
CREATE UNIQUE INDEX users_oidc ON users(oidc_issuer, oidc_subject);
