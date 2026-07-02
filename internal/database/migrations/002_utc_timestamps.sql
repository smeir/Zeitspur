-- Normalize all stored timestamps to UTC (RFC3339 with trailing Z).
--
-- Timestamps were previously written with the local UTC offset (e.g.
-- "+02:00"). SQL compares these TEXT columns lexically, which breaks
-- chronological ordering across DST changes and against new UTC values.
-- strftime parses RFC3339 values including their offset and renders them
-- in UTC; invalid values are left untouched via COALESCE.

UPDATE activity_events SET
    occurred_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', occurred_at), occurred_at),
    created_at  = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', created_at), created_at);

UPDATE work_blocks SET
    started_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', started_at), started_at),
    ended_at   = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', ended_at), ended_at),
    created_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', created_at), created_at),
    updated_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', updated_at), updated_at);

UPDATE day_status SET
    booked_at  = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', booked_at), booked_at),
    updated_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', updated_at), updated_at);

UPDATE booking_settings SET
    updated_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', updated_at), updated_at);

UPDATE booking_closures SET
    closed_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', closed_at), closed_at);

UPDATE audit_log SET
    occurred_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%fZ', occurred_at), occurred_at);
