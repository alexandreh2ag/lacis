package envfiles

import (
	"errors"
	"path"
	"slices"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

// extraConfig reads into e the files collectors.apps.extra_config adds to
// the app, after those of its framework: in order, the last winning. A
// relative path is joined to the app root; an absolute one, declared by
// the administrator of the agent, may lie anywhere.
func (r *reader) extraConfig(e env, root string, app *snapshot.App) error {
	var errs []error
	for _, x := range r.cfg.Apps.ExtraConfig {
		if !matchApp(x.App, app) {
			continue
		}
		for _, src := range x.Config {
			pattern := src.Path
			if !path.IsAbs(pattern) {
				pattern = path.Join(root, pattern)
			}
			files, _ := afero.Glob(r.fs, pattern) // only ErrBadPattern
			slices.Sort(files)
			for _, file := range files {
				if src.Format == "" || isEnvSource(src) {
					errs = append(errs, r.dotenv(e, file))
				} else {
					errs = append(errs, r.structured(e, src.Format, file))
				}
			}
		}
	}
	return errors.Join(errs...)
}

// matchApp tells an app whose path, as seen or resolved, matches the glob.
func matchApp(glob string, app *snapshot.App) bool {
	for _, p := range []string{app.Path, app.ResolvedPath} {
		if ok, _ := path.Match(glob, path.Clean(p)); ok && p != "" {
			return true
		}
	}
	return false
}
