//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIQTestBuildCannotInstallOfficialBinary(t *testing.T) {
	for _, build := range []struct{ version, kind string }{{"0.2.18-iqtest.123", "release"}, {"0.2.18", "iqtest"}} {
		svc := NewUpdateService(nil, nil, build.version, build.kind)
		info, err := svc.CheckUpdate(context.Background(), true)
		require.NoError(t, err)
		require.False(t, info.HasUpdate)
		require.Equal(t, "iqtest", info.BuildType)
		require.ErrorIs(t, svc.PerformUpdate(context.Background()), ErrIQTestManagedUpdate)
		require.ErrorIs(t, svc.Rollback(), ErrIQTestManagedUpdate)
		require.ErrorIs(t, svc.RollbackToVersion(context.Background(), "0.2.17"), ErrIQTestManagedUpdate)
		versions, err := svc.ListRollbackVersions(context.Background())
		require.NoError(t, err)
		require.Empty(t, versions)
	}
}
