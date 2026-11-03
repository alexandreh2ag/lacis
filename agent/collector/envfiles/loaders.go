package envfiles

import (
	"errors"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/agent/fsutil"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

// ConfigLoader reads the configuration of the apps of one framework, in the
// order the framework loads it: keys for .env-like sources, blocks for a
// structured configuration.
type ConfigLoader interface {
	// Framework is the name of the framework, as the apps collector
	// detects it.
	Framework() string
	Load(r *reader, root string, app *snapshot.App) (env, []block, error)
}

// loaders are the built-in loaders by framework; the agent configuration
// can add or replace one with the env sources of a declarative detector.
var loaders = map[string]ConfigLoader{}

// RegisterLoader adds a loader, replacing the one of the same framework.
func RegisterLoader(l ConfigLoader) {
	loaders[l.Framework()] = l
}

func init() {
	RegisterLoader(loadFunc{name: "symfony", load: symfonySources})
	RegisterLoader(zend1Loader{})
	// Laminas and Zend Framework 2/3 merge config/autoload/*.php.
	RegisterLoader(laminasLoader{name: "laminas"})
	RegisterLoader(laminasLoader{name: "zend"})
	// next start: .env, .env.production, .env.local, .env.production.local,
	// the last winning.
	RegisterLoader(envSources("next", ".env", ".env.production", ".env.local", ".env.production.local"))
	// Laravel reads .env alone.
	RegisterLoader(envSources("laravel", ".env"))
	// Nuxt reads .env, then the file of its NODE_ENV.
	RegisterLoader(envSources("nuxt", ".env", ".env.${NODE_ENV}"))
	// @nestjs/config reads .env by default.
	RegisterLoader(envSources("nestjs", ".env"))
}

// defaultLoader reads the .env of the root of an app of another framework.
var defaultLoader = envSources("", ".env")

// loader returns the loader of a framework: that of a declarative detector
// with sources, else the built-in one, else defaultLoader.
func (r *reader) loader(name string) ConfigLoader {
	for _, d := range r.cfg.Apps.Frameworks {
		if d.Name == name && len(d.Config) > 0 {
			return envLoader{name: d.Name, sources: d.Config}
		}
	}
	if l, ok := loaders[name]; ok {
		return l
	}
	return defaultLoader
}

func isEnvSource(s config.FrameworkSource) bool { return s.Format == "env" }

// loadFunc is a loader of .env-like sources written in Go.
type loadFunc struct {
	name string
	load func(r *reader, e env, root string) error
}

func (l loadFunc) Framework() string { return l.name }

func (l loadFunc) Load(r *reader, root string, _ *snapshot.App) (env, []block, error) {
	e := env{}
	return e, nil, l.load(r, e, root)
}

// envLoader reads declarative sources in order, the last file winning. A
// path is relative to the app root, may be a glob and may refer to a
// variable read so far, such as .env.${NODE_ENV}. A php, ini, yaml or json
// source is flattened into env keys (see envKey).
type envLoader struct {
	name    string
	sources []config.FrameworkSource
}

func envSources(name string, paths ...string) envLoader {
	l := envLoader{name: name}
	for _, p := range paths {
		l.sources = append(l.sources, config.FrameworkSource{Path: p, Format: "env"})
	}
	return l
}

func (l envLoader) Framework() string { return l.name }

// sourceDefaults are the values of the variables of a source path when no
// file read so far sets them: the apps found run in production.
var sourceDefaults = map[string]string{"NODE_ENV": "production", "APP_ENV": "production"}

// pathValueRe is a value a source path may take from a variable: a name,
// never a path.
var pathValueRe = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)

func (l envLoader) Load(r *reader, root string, _ *snapshot.App) (env, []block, error) {
	e := env{}
	var errs []error
	for _, src := range l.sources {
		e.resolve()
		name, ok := expand(src.Path, func(ref string) (string, bool) {
			v, found := e[ref]
			if !found || v.unresolved || v.value == "" {
				v.value, found = sourceDefaults[ref]
			}
			return v.value, found && pathValueRe.MatchString(v.value) && !strings.Contains(v.value, "..")
		})
		if !ok {
			continue
		}
		files := []string{path.Join(root, name)}
		if hasMeta(name) {
			// A plain path is read as is: a link is not listed by every
			// file system.
			files, _ = afero.Glob(r.fs, path.Join(root, name)) // only ErrBadPattern
			slices.Sort(files)
		}
		for _, file := range files {
			if !strings.HasPrefix(file, path.Clean(root)+"/") {
				continue
			}
			if isEnvSource(src) {
				errs = append(errs, r.dotenv(e, file))
			} else {
				errs = append(errs, r.structured(e, src.Format, file))
			}
		}
	}
	return e, nil, errors.Join(errs...)
}

func hasMeta(p string) bool { return strings.ContainsAny(p, "*?[\\") }

// laminasLoader reads the .env of the root, then the configuration Laminas
// (and Zend Framework 2/3) merges: config/autoload/global.php,
// *.global.php, local.php, then *.local.php, a later file overriding an
// earlier one key by key. The PHP arrays are parsed statically; their
// getenv() and env() references are resolved against the env of the app.
type laminasLoader struct{ name string }

func (l laminasLoader) Framework() string { return l.name }

// LaminasAutoload is the directory of the merged configuration files.
const LaminasAutoload = "config/autoload"

func (laminasLoader) Load(r *reader, root string, _ *snapshot.App) (env, []block, error) {
	e := env{}
	errs := []error{r.dotenv(e, path.Join(root, ".env"))}
	dir := path.Join(root, LaminasAutoload)
	var files []string
	for _, group := range []string{"global", "local"} {
		files = append(files, path.Join(dir, group+".php"))
		matches, _ := afero.Glob(r.fs, path.Join(dir, "*."+group+".php")) // only ErrBadPattern
		slices.Sort(matches)
		files = append(files, matches...)
	}
	merged := map[string]value{}
	for _, file := range files {
		b, err := fsutil.ReadAppFile(r.fs, file)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		maps.Copy(merged, parsePHPConfig(file, string(b)))
	}
	return e, blocks(merged), errors.Join(errs...)
}

// zend1Loader reads application.ini, in the section of the env of the app.
type zend1Loader struct{}

func (zend1Loader) Framework() string { return "zend1" }

func (zend1Loader) Load(r *reader, root string, app *snapshot.App) (env, []block, error) {
	blocks, err := r.zend1Blocks(root, app.Env)
	return env{}, blocks, err
}

// symfonySources loads like Symfony Dotenv: .env.local.php alone when
// composer dump-env wrote it, else .env, .env.local (skipped in test),
// .env.<APP_ENV>, .env.<APP_ENV>.local.
func symfonySources(r *reader, e env, root string) error {
	if b, err := fsutil.ReadAppFile(r.fs, path.Join(root, ".env.local.php")); err == nil {
		e.parsePHPEnv(path.Join(root, ".env.local.php"), string(b))
		return nil
	}
	errs := []error{r.dotenv(e, path.Join(root, ".env"))}
	appEnv := func() string {
		e.resolve()
		if v, ok := e["APP_ENV"]; ok && v.value != "" && pathValueRe.MatchString(v.value) && !strings.Contains(v.value, "..") {
			return v.value
		}
		return "dev"
	}
	if appEnv() != "test" {
		errs = append(errs, r.dotenv(e, path.Join(root, ".env.local")))
	}
	current := appEnv()
	errs = append(errs, r.dotenv(e, path.Join(root, ".env."+current)))
	if current != "test" {
		errs = append(errs, r.dotenv(e, path.Join(root, ".env."+current+".local")))
	}
	return errors.Join(errs...)
}
