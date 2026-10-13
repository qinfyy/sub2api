//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/dbdecimal"
	"github.com/stretchr/testify/require"
)

func TestPostgresPrimaryDurableStateContract(t *testing.T) {
	testPrimaryDurableStateContract(t, integrationDB)
}

func TestPostgresPrimaryImageQueueContract(t *testing.T) {
	testPrimaryImageQueueContract(t, integrationDB)
}

func TestPostgresPrimaryImageQueueConcurrentClaims(t *testing.T) {
	testPrimaryImageQueueConcurrentClaims(t, integrationDB)
}

func TestPostgresExactDecimalCodec(t *testing.T) {
	ctx := context.Background()
	input, err := dbdecimal.Parse("9999999999.9999999999")
	require.NoError(t, err)
	var loaded dbdecimal.Decimal
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT $1::numeric(20,10)`, input).Scan(&loaded))
	require.Equal(t, input.String(), loaded.String())
	err = integrationDB.QueryRowContext(ctx, `SELECT $1::numeric(20,10)`, "10000000000.0000000000").Scan(&loaded)
	require.Error(t, err)
}
