-- Classify Copilot fetch failures so the UI can distinguish a transient
-- upstream outage (HTTP 5xx, rate limit, network) from a local problem
-- (missing gh binary, expired authentication). The column stores a stable
-- machine-readable kind; the human-readable text stays in error_message.
-- Existing rows keep NULL, which renders as the generic "unknown" failure.
ALTER TABLE copilot_snapshots ADD COLUMN error_kind TEXT;
