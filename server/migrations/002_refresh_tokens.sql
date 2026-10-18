DELETE FROM sessions;
CREATE TABLE refresh_tokens (
 token_hash BLOB NOT NULL PRIMARY KEY CHECK(length(token_hash)=32),
 session_hash BLOB NOT NULL REFERENCES sessions(token_hash) ON DELETE CASCADE,
 consumed INTEGER NOT NULL DEFAULT 0 CHECK(consumed IN (0,1))
);
CREATE INDEX refresh_session ON refresh_tokens(session_hash);
