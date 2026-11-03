package apps

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/agent/fakefs"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tree writes files under dir and makes the links, on the real file
// system: fakefs does not follow links.
func tree(t *testing.T, dir string, files, links map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	for name, target := range links {
		p := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.Symlink(target, p))
	}
}

// secret is a file out of every app, which the agent must never read: it
// holds a commit id and a version, so that only the confinement keeps it
// out of the snapshot.
const secret = commitLegacy + "\n"

func TestReadGit_Confined(t *testing.T) {
	base := t.TempDir()
	tree(t, base, map[string]string{
		"secret":                        secret,
		"outside/.git/HEAD":             "ref: refs/heads/main\n",
		"outside/.git/refs/heads/main":  secret,
		"poc/.git/HEAD":                 "ref: ../../../../etc/shadow\n",
		"dotdot/.git/HEAD":              "ref: ../../secret\n",
		"headlink/.git/refs/heads/main": commitFront + "\n",
		"reflink/.git/HEAD":             "ref: refs/heads/main\n",
		"absgitdir/.git":                "gitdir: " + filepath.Join(base, "outside", ".git") + "\n",
		"relgitdir/.git":                "gitdir: ../outside/.git\n",
		"inner/.git":                    "gitdir: .repo\n",
		"inner/.repo/HEAD":              "ref: refs/heads/main\n",
		"inner/.repo/refs/heads/main":   commitFront + "\n",
		"inner/.repo/config":            "[remote \"origin\"]\n\turl = https://deploy:p/ss@gitlab.example.com/acme/inner.git\n",
		"linkedgitdir/.git":             "gitdir: .repo\n",
		"configlink/.git/HEAD":          commitFront + "\n",
		"taglink/.git/HEAD":             commitFront + "\n",
		"packedlink/.git/HEAD":          "ref: refs/heads/main\n",
		"outside/.git/packed-refs":      secret + " refs/heads/main\n",
		"outside/.git/config":           "[remote \"origin\"]\n\turl = https://example.com/secret.git\n",
		"configlink/.git/refs/heads/x":  commitFront + "\n",
		"headlink/.git/refs/tags/v1":    commitFront + "\n",
	}, map[string]string{
		"headlink/.git/HEAD":           filepath.Join(base, "outside", ".git", "HEAD"),
		"reflink/.git/refs/heads/main": filepath.Join(base, "secret"),
		"linkedgitdir/.repo":           filepath.Join(base, "outside", ".git"),
		"dirlink/.git":                 filepath.Join(base, "outside", ".git"),
		"configlink/.git/config":       filepath.Join(base, "outside", ".git", "config"),
		"taglink/.git/refs/tags/v9":    filepath.Join(base, "secret"),
		"packedlink/.git/packed-refs":  filepath.Join(base, "outside", ".git", "packed-refs"),
	})
	fsys := afero.NewOsFs()
	app := func(name string) string { return filepath.Join(base, name) }

	// The proof of concept: a HEAD naming a file out of the repository.
	_, err := readGit(fsys, app("poc"))
	assert.ErrorIs(t, err, errInvalidRef)
	_, err = readGit(fsys, app("dotdot"))
	assert.ErrorIs(t, err, errInvalidRef)

	// Links out of the app are refused.
	_, err = readGit(fsys, app("headlink"))
	assert.Error(t, err, "HEAD linked out of the app")
	for _, name := range []string{"dirlink", "linkedgitdir"} {
		repo, err := readGit(fsys, app(name))
		assert.Error(t, err, name)
		assert.Nil(t, repo, name)
	}
	repo, err := readGit(fsys, app("reflink"))
	require.NoError(t, err)
	assert.Equal(t, &gitRepo{branch: "main"}, repo, "a ref linked out of the app is not read")
	repo, err = readGit(fsys, app("configlink"))
	require.NoError(t, err)
	assert.Equal(t, &gitRepo{commit: commitFront}, repo, "a config linked out of the app is not read")
	repo, err = readGit(fsys, app("taglink"))
	require.NoError(t, err)
	assert.Equal(t, &gitRepo{commit: commitFront}, repo, "a tag linked out of the app is not read")
	repo, err = readGit(fsys, app("packedlink"))
	require.NoError(t, err)
	assert.Equal(t, &gitRepo{branch: "main"}, repo, "packed refs linked out of the app are not read")

	// A gitdir is only followed under the app.
	for _, name := range []string{"absgitdir", "relgitdir"} {
		_, err = readGit(fsys, app(name))
		assert.ErrorContains(t, err, "gitdir out of the app", name)
	}
	repo, err = readGit(fsys, app("inner"))
	require.NoError(t, err)
	assert.Equal(t, &gitRepo{commit: commitFront, branch: "main", remote: "https://gitlab.example.com/acme/inner.git"}, repo)

	repo, err = readGit(fsys, app("none"))
	require.NoError(t, err)
	assert.Nil(t, repo)
}

func TestReadGit_FIFO(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	require.NoError(t, syscall.Mkfifo(filepath.Join(root, ".git", "HEAD"), 0o644))
	_, err := readGit(afero.NewOsFs(), root)
	assert.ErrorIs(t, err, errNotRegular, "a FIFO is never opened")
}

func TestReadNvmrc(t *testing.T) {
	base := t.TempDir()
	tree(t, base, map[string]string{
		"secret":        "20\n",
		"major/.nvmrc":  "v20\n",
		"full/.nvmrc":   "20.11.1\n",
		"lts/.nvmrc":    "lts/iron\n",
		"star/.nvmrc":   "lts/*\n",
		"node/.nvmrc":   "node\n",
		"shadow/.nvmrc": "root:$6$secret:19000:0:99999:7:::\n",
		"empty/.nvmrc":  "",
		"big/.nvmrc":    string(make([]byte, maxNvmrc+1)),
	}, map[string]string{
		"link/.nvmrc":   filepath.Join(base, "secret"),
		"dotdot/.nvmrc": "../secret",
	})
	fsys := afero.NewOsFs()
	for name, want := range map[string]string{
		"major":  "20",
		"full":   "20.11.1",
		"lts":    "lts/iron",
		"star":   "lts/*",
		"node":   "node",
		"shadow": "",
		"empty":  "",
		"big":    "",
		"link":   "",
		"dotdot": "",
		"absent": "",
	} {
		assert.Equal(t, want, readNvmrc(fsys, filepath.Join(base, name)), name)
	}

	f := fakefs.New()
	f.Write(t, "/app/.nvmrc", "v18.19\n")
	assert.Equal(t, "18.19", readNvmrc(f, "/app"))
}

func TestStripCredentials(t *testing.T) {
	for raw, want := range map[string]string{
		"git@gitlab.example.com:client/api.git":                "git@gitlab.example.com:client/api.git",
		"https://gitlab.example.com/x.git":                     "https://gitlab.example.com/x.git",
		"https://u:p@gitlab.example.com/x.git":                 "https://gitlab.example.com/x.git",
		"ssh://git@gitlab.example.com/x.git":                   "ssh://gitlab.example.com/x.git",
		"https://deploy:p%ss@gitlab.example.com/x.git":         "https://gitlab.example.com/x.git",
		"https://user:1234/abc@gitlab.example.com/x.git":       "https://gitlab.example.com/x.git",
		"https://user:p@ss#w?rd@gitlab.example.com/x.git":      "https://gitlab.example.com/x.git",
		"HTTPS://oauth2:glpat-xyz@gitlab.example.com/a/b.git":  "HTTPS://gitlab.example.com/a/b.git",
		"https://gitlab-ci-token:%zz@gitlab.example.com/a.git": "https://gitlab.example.com/a.git",
		"": "",
	} {
		assert.Equal(t, want, stripCredentials(raw), raw)
	}
}

func TestLauncherCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		mode config.CommandMode
		want string
	}{
		{cmd: "mysqldump -pSECRET app", mode: config.CommandPath, want: "mysqldump"},
		{cmd: "/usr/bin/php8.2 bin/console --dsn=amqp://user:pass@rabbit", mode: config.CommandPath, want: "/usr/bin/php8.2"},
		{cmd: "  ", mode: config.CommandPath, want: ""},
		{cmd: "", mode: "", want: ""},
		{cmd: "node server.js --token=x", mode: config.CommandFull, want: "node server.js --token=x"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, launcherCommand(tt.cmd, tt.mode), tt.cmd)
	}
}

func TestReadDeployInfo_Slugs(t *testing.T) {
	f := fakefs.New()
	f.Files(t, map[string]string{
		"/ok/.app.lacis.yaml":  "project: shop-v2\nenv: pre_prod\nversion: v1\ncommit: abc\n",
		"/bad/.app.lacis.yaml": "project: \"Shop Admin\"\nenv: ../prod\nversion: v1\ncommit: abc\n",
	})
	info, warnings := readDeployInfo(f, "/ok")
	require.Empty(t, warnings)
	assert.Equal(t, "shop-v2", info.Project)
	assert.Equal(t, "pre_prod", info.Env)

	info, warnings = readDeployInfo(f, "/bad")
	require.Len(t, warnings, 2)
	assert.ErrorContains(t, warnings[0], `project "Shop Admin" is not a slug`)
	assert.ErrorContains(t, warnings[1], `env "../prod" is not a slug`)
	assert.Empty(t, info.Project)
	assert.Empty(t, info.Env)
}
