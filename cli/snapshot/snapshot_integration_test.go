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
	assert.Contains(t, out, "globex-billing-prod-1 (globex): accepted, agent created\n")
	assert.Equal(t, 29, strings.Count(out, "\n"))

	out, err = run(ctx, "import", "../../testdata/snapshots")
	require.NoError(t, err)
	assert.Contains(t, out, "acme-shop-prod-1 (acme): unchanged\n", "the same agent, the same snapshot")

	out, err = run(ctx, "normalize")
	require.NoError(t, err)
	assert.Equal(t, "29 snapshots normalized\n99 links computed\n", out)
	var arch int64
	require.NoError(t, db.Model(&model.Host{}).Where("arch is not null").Count(&arch).Error)
	assert.Equal(t, int64(29), arch, "every host normalized")

	for model, want := range map[any]int64{&model.Host{}: 29, &model.Agent{}: 29, &model.Snapshot{}: 29, &model.Namespace{}: 2, &model.EnrollmentToken{}: 1} {
		var count int64
		require.NoError(t, db.Model(model).Count(&count).Error)
		assert.Equal(t, want, count, "%T", model)
	}
}
