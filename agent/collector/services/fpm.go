package services

import (
	"bufio"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/spf13/afero"
)

// fpmMasterConfig reads the configuration path in the title of the master
// process: "php-fpm: master process (/etc/php/8.2/fpm/php-fpm.conf)".
var fpmMasterConfig = regexp.MustCompile(`\((/[^)]+)\)`)

// describePHPFPM names the instance after its PHP version, and reads the
// pools of its configuration.
func describePHPFPM(d *detection, svc *snapshot.Service) error {
	svc.Instance = d.pathVersion
	if svc.Instance == "" && svc.Version != "" {
		parts := strings.SplitN(svc.Version, ".", 3)
		svc.Instance = strings.Join(parts[:min(2, len(parts))], ".")
	}
	if m := fpmMasterConfig.FindStringSubmatch(strings.Join(d.process.Cmdline, " ")); m != nil {
		svc.ConfigPath = m[1]
	}
	if svc.ConfigPath == "" {
		return nil
	}
	pools, err := readFPMConfig(d.ctx.Fs, svc.ConfigPath, 0)
	svc.Pools = pools
	slices.SortFunc(svc.Pools, func(a, b snapshot.FPMPool) int { return strings.Compare(a.Name, b.Name) })
	return err
}

// maxIncludeDepth bounds the includes of an FPM configuration.
const maxIncludeDepth = 4

// readFPMConfig returns the pools of an FPM configuration file and of the
// files it includes: every section but [global] is a pool.
func readFPMConfig(fsys afero.Fs, path string, depth int) ([]snapshot.FPMPool, error) {
	f, err := fsys.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var pools []snapshot.FPMPool
	var includes []string
	var current *snapshot.FPMPool
	flush := func() {
		if current != nil {
			current.Listen = strings.ReplaceAll(current.Listen, "$pool", current.Name)
			if strings.HasPrefix(current.Listen, "/") {
				current.Listen = "unix:" + current.Listen
			}
			pools = append(pools, *current)
			current = nil
		}
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "" || line[0] == ';' || line[0] == '#':
			continue
		case line[0] == '[':
			flush()
			if name := strings.Trim(line, "[] "); name != "global" {
				current = &snapshot.FPMPool{Name: name}
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`)
		switch {
		case key == "include" && current == nil:
			if !filepath.IsAbs(value) {
				value = filepath.Join(filepath.Dir(path), value)
			}
			includes = append(includes, value)
		case key == "listen" && current != nil:
			current.Listen = value
		case key == "user" && current != nil:
			current.User = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	flush()

	if depth >= maxIncludeDepth {
		return pools, nil
	}
	for _, include := range includes {
		files, _ := afero.Glob(fsys, include) // only ErrBadPattern
		slices.Sort(files)
		for _, file := range files {
			p, err := readFPMConfig(fsys, file, depth+1)
			if err != nil {
				return pools, err
			}
			pools = append(pools, p...)
		}
	}
	return pools, nil
}
