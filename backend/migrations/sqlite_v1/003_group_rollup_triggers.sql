-- SQLite serializes this write with the rollup publisher's write transaction.
CREATE TRIGGER usage_logs_group_rollup_invalidate_insert AFTER INSERT ON usage_logs
WHEN NEW.group_id IS NOT NULL
BEGIN
  UPDATE usage_group_rollup_state
    SET closed_before=MIN(closed_before,s2a_local_day(NEW.created_at,s2a_timezone())), updated_at=s2a_utc_now()
    WHERE id=1 AND closed_before>s2a_local_day(NEW.created_at,s2a_timezone());
END;

CREATE TRIGGER usage_logs_group_rollup_invalidate_delete AFTER DELETE ON usage_logs
WHEN OLD.group_id IS NOT NULL
BEGIN
  UPDATE usage_group_rollup_state
    SET closed_before=MIN(closed_before,s2a_local_day(OLD.created_at,s2a_timezone())), updated_at=s2a_utc_now()
    WHERE id=1 AND closed_before>s2a_local_day(OLD.created_at,s2a_timezone());
END;

CREATE TRIGGER usage_logs_group_rollup_invalidate_update AFTER UPDATE OF created_at,group_id,actual_cost ON usage_logs
WHEN (OLD.created_at IS NOT NEW.created_at OR OLD.group_id IS NOT NEW.group_id OR OLD.actual_cost IS NOT NEW.actual_cost)
  AND (OLD.group_id IS NOT NULL OR NEW.group_id IS NOT NULL)
BEGIN
  UPDATE usage_group_rollup_state SET closed_before=MIN(closed_before,
      CASE WHEN OLD.group_id IS NULL THEN s2a_local_day(NEW.created_at,s2a_timezone())
           WHEN NEW.group_id IS NULL THEN s2a_local_day(OLD.created_at,s2a_timezone())
           ELSE MIN(s2a_local_day(OLD.created_at,s2a_timezone()),s2a_local_day(NEW.created_at,s2a_timezone())) END),
      updated_at=s2a_utc_now()
    WHERE id=1 AND closed_before >
      CASE WHEN OLD.group_id IS NULL THEN s2a_local_day(NEW.created_at,s2a_timezone())
           WHEN NEW.group_id IS NULL THEN s2a_local_day(OLD.created_at,s2a_timezone())
           ELSE MIN(s2a_local_day(OLD.created_at,s2a_timezone()),s2a_local_day(NEW.created_at,s2a_timezone())) END;
END;
