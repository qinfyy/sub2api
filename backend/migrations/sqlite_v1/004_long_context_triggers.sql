CREATE TRIGGER accounts_long_context_validate_insert BEFORE INSERT ON accounts
WHEN NEW.platform='openai' AND (
  json_type(COALESCE(NEW.extra,'{}')) IS NOT 'object' OR (
    NOT (NEW.parent_account_id IS NOT NULL AND NEW.quota_dimension='spark')
    AND json_type(NEW.extra,'$.openai_long_context_billing_enabled') IS NOT NULL
    AND json_type(NEW.extra,'$.openai_long_context_billing_enabled') NOT IN ('true','false')))
BEGIN
  SELECT RAISE(ABORT,'openai_long_context_billing_enabled must be a boolean');
END;

CREATE TRIGGER accounts_long_context_validate_update BEFORE UPDATE OF platform,extra,parent_account_id,quota_dimension ON accounts
WHEN NEW.platform='openai' AND (
  json_type(COALESCE(NEW.extra,'{}')) IS NOT 'object' OR (
    NOT (NEW.parent_account_id IS NOT NULL AND NEW.quota_dimension='spark')
    AND json_type(NEW.extra,'$.openai_long_context_billing_enabled') IS NOT NULL
    AND json_type(NEW.extra,'$.openai_long_context_billing_enabled') NOT IN ('true','false')))
BEGIN
  SELECT RAISE(ABORT,'openai_long_context_billing_enabled must be a boolean');
END;

CREATE TRIGGER accounts_long_context_normalize_insert AFTER INSERT ON accounts
WHEN NEW.platform='openai'
BEGIN
  UPDATE accounts SET extra=json_set(COALESCE(extra,'{}'),'$.openai_long_context_billing_enabled',json(
    CASE WHEN NEW.parent_account_id IS NOT NULL AND NEW.quota_dimension='spark' THEN
      COALESCE((SELECT CASE WHEN platform='openai' AND json_type(extra,'$.openai_long_context_billing_enabled') IN ('true','false')
        THEN json_type(extra,'$.openai_long_context_billing_enabled') ELSE 'false' END FROM accounts WHERE id=NEW.parent_account_id),'false')
    ELSE COALESCE(json_type(NEW.extra,'$.openai_long_context_billing_enabled'),'false') END))
    WHERE id=NEW.id AND json_type(extra,'$.openai_long_context_billing_enabled') IS NOT
      CASE WHEN NEW.parent_account_id IS NOT NULL AND NEW.quota_dimension='spark' THEN
        COALESCE((SELECT CASE WHEN platform='openai' AND json_type(extra,'$.openai_long_context_billing_enabled') IN ('true','false')
          THEN json_type(extra,'$.openai_long_context_billing_enabled') ELSE 'false' END FROM accounts WHERE id=NEW.parent_account_id),'false')
      ELSE COALESCE(json_type(NEW.extra,'$.openai_long_context_billing_enabled'),'false') END;
END;

CREATE TRIGGER accounts_long_context_normalize_update AFTER UPDATE OF platform,extra,parent_account_id,quota_dimension ON accounts
WHEN NEW.platform='openai'
BEGIN
  UPDATE accounts SET extra=json_set(COALESCE(extra,'{}'),'$.openai_long_context_billing_enabled',json(
    CASE WHEN NEW.parent_account_id IS NOT NULL AND NEW.quota_dimension='spark' THEN
      COALESCE((SELECT CASE WHEN platform='openai' AND json_type(extra,'$.openai_long_context_billing_enabled') IN ('true','false')
        THEN json_type(extra,'$.openai_long_context_billing_enabled') ELSE 'false' END FROM accounts WHERE id=NEW.parent_account_id),'false')
    WHEN json_type(NEW.extra,'$.openai_long_context_billing_enabled') IN ('true','false') THEN json_type(NEW.extra,'$.openai_long_context_billing_enabled')
    WHEN OLD.platform='openai' AND json_type(OLD.extra,'$.openai_long_context_billing_enabled') IN ('true','false') THEN json_type(OLD.extra,'$.openai_long_context_billing_enabled')
    ELSE 'false' END))
    WHERE id=NEW.id AND json_type(extra,'$.openai_long_context_billing_enabled') IS NOT
      CASE WHEN NEW.parent_account_id IS NOT NULL AND NEW.quota_dimension='spark' THEN
        COALESCE((SELECT CASE WHEN platform='openai' AND json_type(extra,'$.openai_long_context_billing_enabled') IN ('true','false')
          THEN json_type(extra,'$.openai_long_context_billing_enabled') ELSE 'false' END FROM accounts WHERE id=NEW.parent_account_id),'false')
      WHEN json_type(NEW.extra,'$.openai_long_context_billing_enabled') IN ('true','false') THEN json_type(NEW.extra,'$.openai_long_context_billing_enabled')
      WHEN OLD.platform='openai' AND json_type(OLD.extra,'$.openai_long_context_billing_enabled') IN ('true','false') THEN json_type(OLD.extra,'$.openai_long_context_billing_enabled')
      ELSE 'false' END;

  INSERT INTO scheduler_outbox(event_type,account_id)
    SELECT 'account_changed',shadow.id FROM accounts AS shadow
    WHERE NEW.parent_account_id IS NULL AND shadow.parent_account_id=NEW.id AND shadow.platform='openai' AND shadow.quota_dimension='spark'
      AND json_type(shadow.extra,'$.openai_long_context_billing_enabled') IS NOT
        (SELECT json_type(extra,'$.openai_long_context_billing_enabled') FROM accounts WHERE id=NEW.id);
  UPDATE accounts SET extra=json_set(COALESCE(extra,'{}'),'$.openai_long_context_billing_enabled',
      json((SELECT json_type(extra,'$.openai_long_context_billing_enabled') FROM accounts WHERE id=NEW.id)))
    WHERE NEW.parent_account_id IS NULL AND parent_account_id=NEW.id AND platform='openai' AND quota_dimension='spark'
      AND json_type(extra,'$.openai_long_context_billing_enabled') IS NOT
        (SELECT json_type(extra,'$.openai_long_context_billing_enabled') FROM accounts WHERE id=NEW.id);
END;
