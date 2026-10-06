-- These constraints live in PostgreSQL migration 016, not in the Ent schema.
CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique_active
    ON users(email) WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS groups_name_unique_active
    ON groups(name) WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS user_subscriptions_user_group_unique_active
    ON user_subscriptions(user_id, group_id) WHERE deleted_at IS NULL;
