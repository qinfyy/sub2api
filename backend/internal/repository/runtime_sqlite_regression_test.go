package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newSQLiteBillingProbeAccount(t *testing.T, withProxy bool) (*accountRepository, *service.Account) {
	t.Helper()
	r, ctx := newSQLiteAccountRepo(t)
	a := &service.Account{
		Name: "probe", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"api_key": "test-key", "base_url": "https://example.test"},
		Extra: map[string]any{
			service.UpstreamBillingProbeEnabledExtraKey:    true,
			service.UpstreamBillingRateSyncEnabledExtraKey: true,
			service.UpstreamBillingProbeExtraKey:           map[string]any{"status": "failed", "failure_count": 3, "last_error": "old"},
			"untouched":                                    json.Number("9007199254740993"),
		},
	}
	if withProxy {
		p := &service.Proxy{Name: "proxy", Protocol: "http", Host: "localhost", Port: 3128, Status: service.StatusActive}
		require.NoError(t, NewProxyRepository(r.client, r.db).Create(ctx, p))
		a.ProxyID, a.Proxy = &p.ID, p
	}
	require.NoError(t, r.Create(ctx, a))
	return r, a
}

func TestSQLiteRuntimeProbeSnapshot(t *testing.T) {
	for _, withProxy := range []bool{false, true} {
		name := "direct"
		if withProxy {
			name = "proxy"
		}
		t.Run(name, func(t *testing.T) {
			r, a := newSQLiteBillingProbeAccount(t, withProxy)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			rate := 0.25
			err := r.UpdateUpstreamBillingProbeSnapshot(ctx, a, &service.UpstreamBillingProbeSnapshot{Status: "ok"}, &rate)
			require.NoError(t, err)
			var raw string
			require.NoError(t, r.db.QueryRow(`SELECT extra FROM accounts WHERE id=?`, a.ID).Scan(&raw))
			require.Contains(t, raw, "9007199254740993")
			var extra map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(raw), &extra))
			require.NotContains(t, string(extra[service.UpstreamBillingProbeExtraKey]), "last_error")
			require.NotContains(t, string(extra[service.UpstreamBillingProbeExtraKey]), "failure_count")
			loaded, err := r.GetByID(ctx, a.ID)
			require.NoError(t, err)
			require.Equal(t, rate, *loaded.RateMultiplier)
			require.Zero(t, r.db.Stats().InUse)
		})
	}
}

func TestSQLiteRuntimeProbeOuterTransaction(t *testing.T) {
	for _, borrowed := range []bool{false, true} {
		t.Run(map[bool]string{false: "context", true: "borrowed-client"}[borrowed], func(t *testing.T) {
			r, a := newSQLiteBillingProbeAccount(t, false)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			tx, err := r.client.Tx(ctx)
			require.NoError(t, err)
			defer tx.Rollback()
			writer, txctx := r, dbent.NewTxContext(ctx, tx)
			if borrowed {
				writer, txctx = newAccountRepositoryWithSQL(tx.Client(), tx, nil), ctx
			}
			require.NoError(t, writer.UpdateUpstreamBillingProbeSnapshot(txctx, a, &service.UpstreamBillingProbeSnapshot{Status: "ok"}, nil))
			require.NoError(t, tx.Rollback())
			loaded, err := r.GetByID(context.Background(), a.ID)
			require.NoError(t, err)
			require.Equal(t, "failed", loaded.Extra[service.UpstreamBillingProbeExtraKey].(map[string]any)["status"])
		})
	}
}

func TestSQLiteRuntimeProbeRejectsChangedIdentity(t *testing.T) {
	r, a := newSQLiteBillingProbeAccount(t, false)
	_, err := r.db.Exec(`UPDATE accounts SET credentials=json_set(credentials,'$.api_key','changed') WHERE id=?`, a.ID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = r.UpdateUpstreamBillingProbeSnapshot(ctx, a, &service.UpstreamBillingProbeSnapshot{Status: "ok"}, nil)
	require.ErrorIs(t, err, service.ErrUpstreamBillingProbeIdentityChanged)
}

func TestSQLiteRuntimeAuditReads(t *testing.T) {
	r, _ := newSQLiteAccountRepo(t)
	audit := NewAuditLogRepository(r.db)
	ctx := context.Background()
	when := time.Date(2026, 10, 10, 13, 0, 1, 123456000, time.UTC)
	for i := 0; i < 2; i++ {
		require.NoError(t, audit.Insert(ctx, &service.AuditLog{CreatedAt: when.Add(time.Duration(i) * time.Hour), Action: "test", RequestBody: "body", StatusCode: 200}))
	}
	start, end := when.Add(-time.Second), when.Add(time.Second)
	list, err := audit.List(ctx, &service.AuditLogFilter{StartTime: &start, EndTime: &end})
	require.NoError(t, err)
	require.Equal(t, 1, list.Total)
	require.Len(t, list.Logs, 1)
	require.True(t, when.Equal(list.Logs[0].CreatedAt))
	require.Empty(t, list.Logs[0].RequestBody)
	detail, err := audit.GetByID(ctx, list.Logs[0].ID)
	require.NoError(t, err)
	require.Equal(t, "body", detail.RequestBody)
	_, err = audit.GetByID(ctx, -1)
	require.ErrorIs(t, err, service.ErrAuditLogNotFound)
	_, err = r.db.Exec(`UPDATE audit_logs SET created_at='invalid' WHERE id=?`, detail.ID)
	require.NoError(t, err)
	_, err = audit.GetByID(ctx, detail.ID)
	require.Error(t, err)
}

func TestSQLiteRuntimeGroupRateBatch(t *testing.T) {
	u, ctx := newSQLiteUserRepo(t)
	g, err := u.client.Group.Create().SetName("rates").Save(ctx)
	require.NoError(t, err)
	_, err = u.db.Exec(`INSERT INTO user_group_rate_multipliers(user_id,group_id,rate_multiplier) VALUES(1,?,0.25),(2,?,NULL)`, g.ID, g.ID)
	require.NoError(t, err)
	r := NewUserGroupRateRepository(u.db).(*userGroupRateRepository)
	rates, err := r.GetByUserIDs(ctx, []int64{1, 2, 1, 0, -1, 999})
	require.NoError(t, err)
	require.Equal(t, map[int64]float64{g.ID: 0.25}, rates[1])
	require.Empty(t, rates[2])
	require.Empty(t, rates[999])
	require.NotContains(t, rates, int64(0))
	rates, err = r.GetByUserIDs(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, rates)
}

func TestSQLiteRuntimeProbeCanceledReleasesConnection(t *testing.T) {
	r, a := newSQLiteBillingProbeAccount(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := r.UpdateUpstreamBillingProbeSnapshot(ctx, a, &service.UpstreamBillingProbeSnapshot{Status: "ok"}, nil)
	require.True(t, errors.Is(err, context.Canceled))
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, r.db.PingContext(ctx))
}

func TestSQLiteRuntimeProbePoolWaitDeadline(t *testing.T) {
	r, a := newSQLiteBillingProbeAccount(t, false)
	held, err := r.client.Tx(context.Background())
	require.NoError(t, err)
	defer held.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err = r.UpdateUpstreamBillingProbeSnapshot(ctx, a, &service.UpstreamBillingProbeSnapshot{Status: "ok"}, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, held.Rollback())
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, r.db.PingContext(ctx))
	require.Zero(t, r.db.Stats().InUse)
}

func TestSQLiteRuntimeProbeRejectsChangedProxy(t *testing.T) {
	r, a := newSQLiteBillingProbeAccount(t, true)
	_, err := r.db.Exec(`UPDATE proxies SET host='changed.test' WHERE id=?`, *a.ProxyID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = r.UpdateUpstreamBillingProbeSnapshot(ctx, a, &service.UpstreamBillingProbeSnapshot{Status: "ok"}, nil)
	require.ErrorIs(t, err, service.ErrUpstreamBillingProbeIdentityChanged)
	require.NoError(t, r.db.PingContext(ctx))
}

func TestSQLiteRuntimeProbeOutboxRollback(t *testing.T) {
	r, a := newSQLiteBillingProbeAccount(t, false)
	var before string
	require.NoError(t, r.db.QueryRow(`SELECT extra FROM accounts WHERE id=?`, a.ID).Scan(&before))
	// Account creation enqueues the same deduplicated event. Remove that event
	// so this save must insert one and exercises the transaction rollback.
	_, err := r.db.Exec(`DELETE FROM scheduler_outbox`)
	require.NoError(t, err)
	_, err = r.db.Exec(`CREATE TRIGGER reject_probe_outbox BEFORE INSERT ON scheduler_outbox BEGIN SELECT RAISE(ABORT, 'test outbox failure'); END`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	rate := 0.25
	err = r.UpdateUpstreamBillingProbeSnapshot(ctx, a, &service.UpstreamBillingProbeSnapshot{Status: "ok"}, &rate)
	require.ErrorContains(t, err, "test outbox failure")
	var after string
	var rateAfter float64
	require.NoError(t, r.db.QueryRowContext(ctx, `SELECT extra,rate_multiplier FROM accounts WHERE id=?`, a.ID).Scan(&after, &rateAfter))
	require.Equal(t, before, after)
	require.Equal(t, float64(1), rateAfter)
	require.NoError(t, r.db.PingContext(ctx))
}

func TestSQLiteRuntimeProbeJSONAndRate(t *testing.T) {
	for _, flag := range []any{true, false, "true", 1, nil} {
		encoded, _ := json.Marshal(flag)
		t.Run(string(encoded), func(t *testing.T) {
			r, a := newSQLiteBillingProbeAccount(t, false)
			_, err := r.db.Exec(`UPDATE accounts SET credentials=?,extra=json_set(extra,'$.upstream_billing_rate_sync_enabled',json(?)) WHERE id=?`,
				`{ "base_url": "https://example.test", "api_key": "test-key" }`, string(encoded), a.ID)
			require.NoError(t, err)
			a.Extra[service.UpstreamBillingRateSyncEnabledExtraKey] = flag
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			rate := 0.25
			require.NoError(t, r.UpdateUpstreamBillingProbeSnapshot(ctx, a, &service.UpstreamBillingProbeSnapshot{Status: "ok"}, &rate))
			var got float64
			require.NoError(t, r.db.QueryRowContext(ctx, `SELECT rate_multiplier FROM accounts WHERE id=?`, a.ID).Scan(&got))
			if flag == true {
				require.Equal(t, rate, got)
			} else {
				require.Equal(t, float64(1), got)
			}
		})
	}
}

func TestSQLiteRuntimeManualAndScheduledProbe(t *testing.T) {
	r, a := newSQLiteBillingProbeAccount(t, false)
	svc := service.NewUpstreamBillingProbeService(r, nil, nil)
	defer svc.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	snapshot, err := svc.ProbeAccount(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, "transport_unavailable", snapshot.LastError)
	_, err = r.db.Exec(`UPDATE accounts SET extra=json_remove(extra,'$.upstream_billing_probe') WHERE id=?`, a.ID)
	require.NoError(t, err)
	require.NoError(t, svc.RunDue(ctx))
	loaded, err := r.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Contains(t, loaded.Extra, service.UpstreamBillingProbeExtraKey)
	require.NoError(t, r.db.PingContext(ctx))
}

func TestProbeJSONEquality(t *testing.T) {
	for _, tc := range []struct {
		left, right string
		equal       bool
	}{
		{`{"a":9007199254740993,"b":true}`, `{"b":true,"a":9007199254740993}`, true},
		{`9007199254740993`, `9007199254740992`, false},
		{`true`, `"true"`, false}, {`true`, `1`, false}, {`null`, `null`, true},
		{`{}`, `{"a":null}`, false}, {`[1,2]`, `[2,1]`, false},
		{`1.00`, `1e0`, true}, {`0`, `-0.0`, true}, {`1e9999999`, `10e9999998`, true},
	} {
		equal, err := probeJSONEqual(tc.left, tc.right)
		require.NoError(t, err)
		require.Equal(t, tc.equal, equal, "%s / %s", tc.left, tc.right)
	}
	_, err := probeJSONEqual(`{`, `{}`)
	require.Error(t, err)
}
