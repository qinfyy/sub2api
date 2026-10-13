-- Database-enforced invalidation, including raw SQL and Ent upserts.
CREATE TRIGGER api_keys_auth_invalidation_update AFTER UPDATE ON api_keys
WHEN OLD.key IS NOT NEW.key OR OLD.status IS NOT NEW.status
  OR OLD.deleted_at IS NOT NEW.deleted_at OR OLD.user_id IS NOT NEW.user_id
  OR OLD.group_id IS NOT NEW.group_id OR OLD.ip_whitelist IS NOT NEW.ip_whitelist
  OR OLD.ip_blacklist IS NOT NEW.ip_blacklist OR OLD.expires_at IS NOT NEW.expires_at
BEGIN
  INSERT INTO auth_cache_invalidation_outbox(cache_key)
    SELECT s2a_sha256(OLD.key) WHERE OLD.key IS NOT NULL AND OLD.key <> '';
  INSERT INTO auth_cache_invalidation_outbox(cache_key)
    SELECT s2a_sha256(NEW.key) WHERE NEW.deleted_at IS NULL AND NEW.key IS NOT OLD.key
      AND NEW.key IS NOT NULL AND NEW.key <> '';
END;

CREATE TRIGGER api_keys_auth_invalidation_delete AFTER DELETE ON api_keys
WHEN OLD.key IS NOT NULL AND OLD.key <> ''
BEGIN
  INSERT INTO auth_cache_invalidation_outbox(cache_key) VALUES(s2a_sha256(OLD.key));
END;

CREATE TRIGGER users_auth_invalidation_update AFTER UPDATE ON users
WHEN OLD.status IS NOT NEW.status OR OLD.role IS NOT NEW.role OR OLD.deleted_at IS NOT NEW.deleted_at
BEGIN
  INSERT INTO auth_cache_invalidation_outbox(cache_key)
    SELECT s2a_sha256(key) FROM api_keys WHERE user_id=OLD.id AND deleted_at IS NULL AND key <> '';
END;

CREATE TRIGGER users_auth_invalidation_delete AFTER DELETE ON users
BEGIN
  INSERT INTO auth_cache_invalidation_outbox(cache_key)
    SELECT s2a_sha256(key) FROM api_keys WHERE user_id=OLD.id AND deleted_at IS NULL AND key <> '';
END;

CREATE TRIGGER groups_auth_invalidation_update AFTER UPDATE ON groups
WHEN OLD.status IS NOT NEW.status OR OLD.is_exclusive IS NOT NEW.is_exclusive
  OR OLD.allow_image_generation IS NOT NEW.allow_image_generation OR OLD.platform IS NOT NEW.platform
  OR OLD.subscription_type IS NOT NEW.subscription_type OR OLD.rate_multiplier IS NOT NEW.rate_multiplier
  OR OLD.peak_rate_enabled IS NOT NEW.peak_rate_enabled OR OLD.peak_start IS NOT NEW.peak_start
  OR OLD.peak_end IS NOT NEW.peak_end OR OLD.peak_rate_multiplier IS NOT NEW.peak_rate_multiplier
  OR OLD.profit_control_enabled IS NOT NEW.profit_control_enabled OR OLD.profit_min_margin IS NOT NEW.profit_min_margin
  OR OLD.profit_safety_buffer IS NOT NEW.profit_safety_buffer OR OLD.deleted_at IS NOT NEW.deleted_at
BEGIN
  INSERT INTO auth_cache_invalidation_outbox(cache_key)
    SELECT s2a_sha256(key) FROM api_keys WHERE group_id=OLD.id AND deleted_at IS NULL AND key <> '';
END;

CREATE TRIGGER groups_auth_invalidation_delete AFTER DELETE ON groups
BEGIN
  INSERT INTO auth_cache_invalidation_outbox(cache_key)
    SELECT s2a_sha256(key) FROM api_keys WHERE group_id=OLD.id AND deleted_at IS NULL AND key <> '';
END;

CREATE TRIGGER allowed_groups_auth_invalidation_insert AFTER INSERT ON user_allowed_groups
WHEN EXISTS(SELECT 1 FROM groups WHERE id=NEW.group_id AND is_exclusive=1)
BEGIN
  INSERT INTO auth_cache_invalidation_outbox(cache_key)
    SELECT s2a_sha256(key) FROM api_keys WHERE user_id=NEW.user_id AND group_id=NEW.group_id
      AND deleted_at IS NULL AND key <> '';
END;

CREATE TRIGGER allowed_groups_auth_invalidation_delete AFTER DELETE ON user_allowed_groups
WHEN EXISTS(SELECT 1 FROM groups WHERE id=OLD.group_id AND is_exclusive=1)
BEGIN
  INSERT INTO auth_cache_invalidation_outbox(cache_key)
    SELECT s2a_sha256(key) FROM api_keys WHERE user_id=OLD.user_id AND group_id=OLD.group_id
      AND deleted_at IS NULL AND key <> '';
END;

CREATE TRIGGER allowed_groups_auth_invalidation_update AFTER UPDATE ON user_allowed_groups
WHEN OLD.user_id IS NOT NEW.user_id OR OLD.group_id IS NOT NEW.group_id
BEGIN
  INSERT INTO auth_cache_invalidation_outbox(cache_key)
    SELECT s2a_sha256(key) FROM api_keys WHERE user_id=OLD.user_id AND group_id=OLD.group_id
      AND deleted_at IS NULL AND key <> ''
      AND EXISTS(SELECT 1 FROM groups WHERE id=OLD.group_id AND is_exclusive=1);
  INSERT INTO auth_cache_invalidation_outbox(cache_key)
    SELECT s2a_sha256(key) FROM api_keys WHERE user_id=NEW.user_id AND group_id=NEW.group_id
      AND deleted_at IS NULL AND key <> ''
      AND EXISTS(SELECT 1 FROM groups WHERE id=NEW.group_id AND is_exclusive=1);
END;

CREATE TRIGGER auth_invalidation_validate_hash_insert BEFORE INSERT ON auth_cache_invalidation_outbox
WHEN length(NEW.cache_key) <> 64 OR NEW.cache_key GLOB '*[^0-9a-f]*'
BEGIN
  SELECT RAISE(ABORT, 'auth invalidation cache key must be a SHA-256 hex digest');
END;

CREATE TRIGGER auth_invalidation_validate_hash_update BEFORE UPDATE OF cache_key ON auth_cache_invalidation_outbox
WHEN length(NEW.cache_key) <> 64 OR NEW.cache_key GLOB '*[^0-9a-f]*'
BEGIN
  SELECT RAISE(ABORT, 'auth invalidation cache key must be a SHA-256 hex digest');
END;
