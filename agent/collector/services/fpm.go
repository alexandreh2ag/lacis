package services

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/ini"
	"github.com/alexandreh2ag/lacis/agent/netaddr"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

// fpmMasterConfig reads the configuration path in the title of the master
// process: "php-fpm: master process (/etc/php/8.2/fpm/php-fpm.conf)".
var fpmMasterConfig = regexp.MustCompile(`^php-fpm: master process \((/[^)]+)\)$`)

// describePHPFPM names the instance after its PHP version, and reads the
// pools of its configuration.
func describePHPFPM(d *detection, svc *snapshot.Service) error {
	svc.Instance = d.pathVersion
	if svc.Instance == "" && svc.Version != "" {
		parts := strings.SplitN(svc.Version, ".", 3)
		svc.Instance = strings.Join(parts[:min(2, len(parts))], ".")
	}
	if m := fpmMasterConfig.FindStringSubmatch(strings.TrimSpace(strings.Join(d.process.Cmdline, " "))); m != nil {
		svc.ConfigPath = m[1]
	}
	if svc.ConfigPath == "" {
		return nil
	}
	// The path comes from the title of the process: read with its rights.
	r := &fpmReader{
		read: func(file string) ([]byte, error) { return d.sys.ReadAs(d.stdCtx, d.process, file) },
		glob: func(pattern string) ([]string, error) { return afero.Glob(d.ctx.Fs, pattern) },
	}
	pools, err := r.config(svc.ConfigPath, 0)
	svc.Pools = pools
	slices.SortFunc(svc.Pools, func(a, b snapshot.FPMPool) int { return strings.Compare(a.Name, b.Name) })
	return err
}

// maxIncludeDepth bounds the includes of an FPM configuration.
const maxIncludeDepth = 4

type fpmReader struct {
	read func(file string) ([]byte, error)
	glob func(pattern string) ([]string, error)
}

// fpmOptions are those of the php.ini dialect: values may be quoted.
var fpmOptions = ini.Options{Value: func(v string) string { return strings.Trim(strings.TrimSpace(v), `"'`) }}

// config returns the pools of an FPM configuration file and of the files
// it includes: every section but [global] is a pool.
func (r *fpmReader) config(path string, depth int) ([]snapshot.FPMPool, error) {
	b, err := r.read(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	sections, err := ini.Parse(b, fpmOptions)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var pools []snapshot.FPMPool
	var includes []string
	for i, section := range sections {
		if i == 0 || section.Name == "global" {
			for _, k := range section.Keys {
				if k.Name != "include" {
					continue
				}
				include := k.Value
				if !filepath.IsAbs(include) {
					include = filepath.Join(filepath.Dir(path), include)
				}
				includes = append(includes, include)
			}
			continue
		}
		pool := snapshot.FPMPool{Name: section.Name}
		pool.Listen, _ = section.Get("listen")
		pool.User, _ = section.Get("user")
		if pool.Listen != "" {
			pool.Listen = netaddr.Listen(strings.ReplaceAll(pool.Listen, "$pool", pool.Name), "")
		}
		pools = append(pools, pool)
	}

	if depth >= maxIncludeDepth {
		return pools, nil
	}
	for _, include := range includes {
		files, _ := r.glob(include) // only ErrBadPattern
		slices.Sort(files)
		for _, file := range files {
			p, err := r.config(file, depth+1)
			if err != nil {
				return pools, err
			}
			pools = append(pools, p...)
		}
	}
	return pools, nil
}
