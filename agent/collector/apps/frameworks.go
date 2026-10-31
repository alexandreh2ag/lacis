package apps

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/config"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

// detector recognizes the framework of an app root.
type detector struct {
	name   string
	detect func(fsys afero.Fs, root string) (version string, ok bool)
}

// builtinDetectors are tried in order; a declarative detector of the agent
// configuration with the same name replaces one.
var builtinDetectors = []detector{
	{name: "symfony", detect: composerPackage("symfony/framework-bundle")},
	{name: "laminas", detect: composerPackage("laminas/laminas-mvc")},
	{name: "zend", detect: composerPackage("zendframework/zend-mvc")},
	{name: "zend1", detect: detectZend1},
	{name: "next", detect: npmPackage("next")},
}

// detectors returns the declarative detectors of the configuration, then
// the built-in ones they do not replace.
func detectors(declared []config.FrameworkDetector) []detector {
	var out []detector
	names := map[string]bool{}
	for _, d := range declared {
		names[d.Name] = true
		out = append(out, declarative(d))
	}
	for _, d := range builtinDetectors {
		if !names[d.name] {
			out = append(out, d)
		}
	}
	return out
}

func declarative(d config.FrameworkDetector) detector {
	return detector{name: d.Name, detect: func(fsys afero.Fs, root string) (string, bool) {
		switch {
		case d.Detect.ComposerPackage != "":
			return composerPackage(d.Detect.ComposerPackage)(fsys, root)
		case d.Detect.NpmPackage != "":
			return npmPackage(d.Detect.NpmPackage)(fsys, root)
		case d.Detect.File != "":
			_, err := fsys.Stat(path.Join(root, d.Detect.File))
			return "", err == nil
		}
		return "", false
	}}
}

// framework returns the framework of root, nil when none is recognized.
func framework(fsys afero.Fs, root string, detectors []detector) *snapshot.Framework {
	for _, d := range detectors {
		if version, ok := d.detect(fsys, root); ok {
			return &snapshot.Framework{Name: d.name, Version: version}
		}
	}
	return nil
}

// composerPackage detects a package of composer.lock, its version without
// the "v" prefix.
func composerPackage(name string) func(afero.Fs, string) (string, bool) {
	return func(fsys afero.Fs, root string) (string, bool) {
		var lock struct {
			Packages []struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"packages"`
		}
		if !readJSON(fsys, path.Join(root, "composer.lock"), &lock) {
			return "", false
		}
		for _, p := range lock.Packages {
			if p.Name == name {
				return strings.TrimPrefix(p.Version, "v"), true
			}
		}
		return "", false
	}
}

// npmPackage detects a dependency of package.json, its version read in
// node_modules, else in package-lock.json, else the range of package.json.
func npmPackage(name string) func(afero.Fs, string) (string, bool) {
	return func(fsys afero.Fs, root string) (string, bool) {
		var manifest struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if !readJSON(fsys, path.Join(root, "package.json"), &manifest) {
			return "", false
		}
		rng, ok := manifest.Dependencies[name]
		if !ok {
			if rng, ok = manifest.DevDependencies[name]; !ok {
				return "", false
			}
		}
		var installed struct {
			Version string `json:"version"`
		}
		if readJSON(fsys, path.Join(root, "node_modules", name, "package.json"), &installed) && installed.Version != "" {
			return installed.Version, true
		}
		var lock struct {
			Packages map[string]struct {
				Version string `json:"version"`
			} `json:"packages"`
		}
		if readJSON(fsys, path.Join(root, "package-lock.json"), &lock) {
			if p, ok := lock.Packages["node_modules/"+name]; ok && p.Version != "" {
				return p.Version, true
			}
		}
		return rng, true
	}
}

var zend1Version = regexp.MustCompile(`const VERSION\s*=\s*'([^']+)'`)

// detectZend1 recognizes a Zend Framework 1 application, its version read
// in library/Zend/Version.php when bundled.
func detectZend1(fsys afero.Fs, root string) (string, bool) {
	_, iniErr := fsys.Stat(path.Join(root, "application", "configs", "application.ini"))
	b, libErr := afero.ReadFile(fsys, path.Join(root, "library", "Zend", "Version.php"))
	if iniErr != nil && libErr != nil {
		return "", false
	}
	if m := zend1Version.FindSubmatch(b); m != nil {
		return string(m[1]), true
	}
	return "", true
}

func readJSON(fsys afero.Fs, file string, out any) bool {
	b, err := afero.ReadFile(fsys, file)
	return err == nil && json.Unmarshal(b, out) == nil
}
