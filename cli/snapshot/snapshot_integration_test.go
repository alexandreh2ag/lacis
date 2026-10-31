//go:build integration

package snapshot

import (
	"strings"
	"testing"

	"github.com/alexandreh2ag/lacis/context"
	"github.com/alexandreh2ag/lacis/database/dbtest"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImport_Fixtures imports the fictitious snapshots of testdata twice:
// they must stay valid, and a second import changes nothing.
func TestImport_Fixtures(t *testing.T) {
	db := dbtest.Open(t, "snapshot")
	ctx := context.TestContext(nil)
	ctx.Fs = afero.NewOsFs()
	ctx.Config.Database.DSN = dbtest.DSN(t, "snapshot")

	out, err := run(ctx, "import", "../../testdata/snapshots")
	require.NoError(t, err)
	assert.Contains(t, out, "acme-shop-prod-1 (acme): accepted, agent created\n")
	assert.Equal(t, 25, strings.Count(out, "\n"))

	out, err = run(ctx, "import", "../../testdata/snapshots")
	require.NoError(t, err)
	assert.Contains(t, out, "acme-shop-prod-1 (acme): unchanged\n", "the same agent, the same snapshot")

	out, err = run(ctx, "normalize")
	require.NoError(t, err)
	assert.Equal(t, "25 snapshots normalized\n86 links computed\n", out)
	var arch int64
	require.NoError(t, db.Model(&model.Host{}).Where("arch is not null").Count(&arch).Error)
	assert.Equal(t, int64(25), arch, "every host normalized")

	for model, want := range map[any]int64{&model.Host{}: 25, &model.Agent{}: 25, &model.Snapshot{}: 25, &model.Namespace{}: 1, &model.EnrollmentToken{}: 1} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		assert.Equal(t, want, count, "%T", model)
	}
}
