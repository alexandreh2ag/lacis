package system

import (
	"bufio"
	"bytes"
	stdContext "context"
	"fmt"
	"regexp"
	"strings"
)

// Package managers, queried with their absolute path.
const (
	DpkgQueryCommand = "/usr/bin/dpkg-query"
	RPMCommand       = "/usr/bin/rpm"
)

// Packages are the installed packages: name to version, as the package
// manager gives it (e.g. "1.22.1-9+deb12u1").
type Packages map[string]string

// Packages returns the installed packages, with dpkg on Debian-like systems
// and rpm on RHEL-like ones; empty when there is no known package manager.
func (s *System) Packages(stdCtx stdContext.Context) (Packages, error) {
	return s.packages.get(func() (Packages, error) {
		managers := []func(stdContext.Context) (Packages, error){s.dpkg, s.rpm}
		if s.Family == FamilyRHEL {
			managers = []func(stdContext.Context) (Packages, error){s.rpm, s.dpkg}
		}
		for _, list := range managers {
			if pkgs, err := list(stdCtx); pkgs != nil || err != nil {
				return pkgs, err
			}
		}
		return Packages{}, nil
	})
}

// dpkg lists the installed packages, or returns nil without dpkg.
func (s *System) dpkg(stdCtx stdContext.Context) (Packages, error) {
	if !s.exists(DpkgQueryCommand) {
		return nil, nil
	}
	out, err := s.ctx.Executor.Run(stdCtx, DpkgQueryCommand, "-W", "-f", `${db:Status-Abbrev}\t${Package}\t${Version}\n`)
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}
	pkgs := Packages{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		// The second letter is the state, "i" for installed, whatever the
		// wanted action: "ii" or "hi" for a held package. Removed packages
		// keeping their config are "rc".
		if len(fields) == 3 && len(fields[0]) >= 2 && fields[0][1] == 'i' {
			pkgs[fields[1]] = fields[2]
		}
	}
	return pkgs, scanner.Err()
}

// rpm lists the installed packages, or returns nil without rpm.
func (s *System) rpm(stdCtx stdContext.Context) (Packages, error) {
	if !s.exists(RPMCommand) {
		return nil, nil
	}
	out, err := s.ctx.Executor.Run(stdCtx, RPMCommand, "-qa", "--qf", `%{NAME}\t%{VERSION}-%{RELEASE}\n`)
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}
	pkgs := Packages{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		if name, version, ok := strings.Cut(scanner.Text(), "\t"); ok {
			pkgs[name] = version
		}
	}
	return pkgs, scanner.Err()
}

func (s *System) exists(path string) bool {
	_, err := s.ctx.Fs.Stat(path)
	return err == nil
}

var revision = regexp.MustCompile(`[-+~].*$`)

// UpstreamVersion strips the epoch and the packaging revision of a package
// version: "1:10.11.6-0+deb12u1" → "10.11.6", "1.20.1-14.el9" → "1.20.1".
func UpstreamVersion(v string) string {
	if _, after, ok := strings.Cut(v, ":"); ok {
		v = after
	}
	return revision.ReplaceAllString(v, "")
}

// Version returns the upstream version of the first installed package of
// names, and that package; empty when none is installed.
func (p Packages) Version(names ...string) (version, pkg string) {
	for _, name := range names {
		if v, ok := p[name]; ok {
			return UpstreamVersion(v), name
		}
	}
	return "", ""
}
