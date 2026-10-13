-- Preserve the default alert rules from PostgreSQL migration 033.
-- 1) High error rate (P1)
INSERT INTO ops_alert_rules (
    name, description, enabled, metric_type, operator, threshold,
    window_minutes, sustained_minutes, severity, notify_email, cooldown_minutes,
    created_at, updated_at
) VALUES (
    '错误率过高',
    '当错误率超过 5% 且持续 5 分钟时触发告警',
    true, 'error_rate', '>', 5.0, 5, 5, 'P1', true, 20, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
) ON CONFLICT (name) DO NOTHING;

-- 2) Low success rate (P0)
INSERT INTO ops_alert_rules (
    name, description, enabled, metric_type, operator, threshold,
    window_minutes, sustained_minutes, severity, notify_email, cooldown_minutes,
    created_at, updated_at
) VALUES (
    '成功率过低',
    '当成功率低于 95% 且持续 5 分钟时触发告警（服务可用性下降）',
    true, 'success_rate', '<', 95.0, 5, 5, 'P0', true, 15, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
) ON CONFLICT (name) DO NOTHING;

-- 3) P99 latency too high (P2)
INSERT INTO ops_alert_rules (
    name, description, enabled, metric_type, operator, threshold,
    window_minutes, sustained_minutes, severity, notify_email, cooldown_minutes,
    created_at, updated_at
) VALUES (
    'P99延迟过高',
    '当 P99 延迟超过 3000ms 且持续 10 分钟时触发告警',
    true, 'p99_latency_ms', '>', 3000.0, 5, 10, 'P2', true, 30, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
) ON CONFLICT (name) DO NOTHING;

-- 4) P95 latency too high (P2)
INSERT INTO ops_alert_rules (
    name, description, enabled, metric_type, operator, threshold,
    window_minutes, sustained_minutes, severity, notify_email, cooldown_minutes,
    created_at, updated_at
) VALUES (
    'P95延迟过高',
    '当 P95 延迟超过 2000ms 且持续 10 分钟时触发告警',
    true, 'p95_latency_ms', '>', 2000.0, 5, 10, 'P2', true, 30, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
) ON CONFLICT (name) DO NOTHING;

-- 5) CPU usage too high (P2)
INSERT INTO ops_alert_rules (
    name, description, enabled, metric_type, operator, threshold,
    window_minutes, sustained_minutes, severity, notify_email, cooldown_minutes,
    created_at, updated_at
) VALUES (
    'CPU使用率过高',
    '当 CPU 使用率超过 85% 且持续 10 分钟时触发告警',
    true, 'cpu_usage_percent', '>', 85.0, 5, 10, 'P2', true, 30, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
) ON CONFLICT (name) DO NOTHING;

-- 6) Memory usage too high (P1)
INSERT INTO ops_alert_rules (
    name, description, enabled, metric_type, operator, threshold,
    window_minutes, sustained_minutes, severity, notify_email, cooldown_minutes,
    created_at, updated_at
) VALUES (
    '内存使用率过高',
    '当内存使用率超过 90% 且持续 10 分钟时触发告警（可能导致 OOM）',
    true, 'memory_usage_percent', '>', 90.0, 5, 10, 'P1', true, 20, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
) ON CONFLICT (name) DO NOTHING;

-- 7) Concurrency queue buildup (P1)
INSERT INTO ops_alert_rules (
    name, description, enabled, metric_type, operator, threshold,
    window_minutes, sustained_minutes, severity, notify_email, cooldown_minutes,
    created_at, updated_at
) VALUES (
    '并发队列积压',
    '当并发队列深度超过 100 且持续 5 分钟时触发告警（系统处理能力不足）',
    true, 'concurrency_queue_depth', '>', 100.0, 5, 5, 'P1', true, 20, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
) ON CONFLICT (name) DO NOTHING;

-- 8) Extremely high error rate (P0)
INSERT INTO ops_alert_rules (
    name, description, enabled, metric_type, operator, threshold,
    window_minutes, sustained_minutes, severity, notify_email, cooldown_minutes,
    created_at, updated_at
) VALUES (
    '错误率极高',
    '当错误率超过 20% 且持续 1 分钟时触发告警（服务严重异常）',
    true, 'error_rate', '>', 20.0, 1, 1, 'P0', true, 15, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
) ON CONFLICT (name) DO NOTHING;
