package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/common/dsn"
)

var (
	ErrSchemaVersion = errors.New("unsupported schema version")
	ErrInvalid       = errors.New("invalid snapshot")
)

// The caps of a snapshot. They are far above what a real host sends, and
// keep one snapshot from making the link computation of its whole
// namespace explode: the server looks up the domains of the namespace for
// every vhost of an app and every dependency, and tries every regex domain
// at each lookup.
const (
	// MaxApps is the number of apps of a host; a shared hosting server
	// holds a few hundred.
	MaxApps = 2000
	// MaxDomains is the number of names the webservers of a host serve:
	// the server names of the vhosts and the names of the Traefik rules. A
	// big shared front serves a few thousand.
	MaxDomains = 20000
	// MaxRegexDomains is the number of those names that are regexes: nginx
	// "~" server names and Traefik HostRegexp. They cost a match each at
	// every lookup, where the others cost a map access.
	MaxRegexDomains = 500
	// MaxContainers is the number of Docker containers of a host.
	MaxContainers = 5000
)

// Validate checks the snapshot structure. It does not check that the
// namespace matches the agent token: that is the server's job.
func (s *Snapshot) Validate() error {
	if !IsAccepted(s.SchemaVersion) {
		return fmt.Errorf("%w: %d", ErrSchemaVersion, s.SchemaVersion)
	}
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...))
	}
	if s.CollectedAt.IsZero() {
		add("collected_at is required")
	}
	if s.Host.Hostname == "" {
		add("host.hostname is required")
	}
	if err := s.Host.Labels.Validate(); err != nil {
		add("host.labels: %v", err)
	}
	if s.Namespace != s.Host.Labels.Namespace {
		add("namespace %q differs from host.labels.namespace %q", s.Namespace, s.Host.Labels.Namespace)
	}
	for i, app := range s.Apps {
		if app.Path == "" {
			add("apps[%d].path is required", i)
		}
		if app.Role != AppRoleWeb && app.Role != AppRoleWorker {
			add("apps[%d].role %q is invalid", i, app.Role)
		}
		for j, addr := range app.Listen {
			if !isHostPort(addr) {
				add("apps[%d].listen[%d] %q is not a host:port address", i, j, addr)
			}
		}
		for j, l := range app.Launchers {
			if l.Role != AppRoleWeb && l.Role != AppRoleWorker {
				add("apps[%d].launchers[%d].role %q is invalid", i, j, l.Role)
			}
		}
		for j, dep := range app.Dependencies {
			if dep.Key == "" {
				add("apps[%d].dependencies[%d].key is required", i, j)
			}
			if dep.Unparsed {
				continue
			}
			if dep.Target == nil {
				add("apps[%d].dependencies[%d].target is required", i, j)
				continue
			}
			if !slices.Contains([]dsn.Kind{dsn.KindIP, dsn.KindName, dsn.KindSocket}, dsn.Kind(dep.Target.Kind)) {
				add("apps[%d].dependencies[%d].target.kind %q is invalid", i, j, dep.Target.Kind)
			}
			if dep.Target.Value == "" {
				add("apps[%d].dependencies[%d].target.value is required", i, j)
			}
		}
	}
	for i, ws := range s.Webservers {
		for j, addr := range ws.Listen {
			if !isHostPort(addr) {
				add("webservers[%d].listen[%d] %q is not a host:port address", i, j, addr)
			}
		}
	}
	for i, c := range s.Containers {
		if c.Name == "" {
			add("containers[%d].name is required", i)
		}
	}
	if len(s.Containers) > MaxContainers {
		add("%d containers, %d at most", len(s.Containers), MaxContainers)
	}
	if len(s.Apps) > MaxApps {
		add("%d apps, %d at most", len(s.Apps), MaxApps)
	}
	domains, regexes := s.countDomains()
	if domains > MaxDomains {
		add("%d domains served, %d at most", domains, MaxDomains)
	}
	if regexes > MaxRegexDomains {
		add("%d regex domains served, %d at most", regexes, MaxRegexDomains)
	}
	for i, c := range s.Collectors {
		switch c.Status {
		case CollectorStatusOK, CollectorStatusSkipped, CollectorStatusDisabled, CollectorStatusError:
		default:
			add("collectors[%d].status %q is invalid", i, c.Status)
		}
	}
	return errors.Join(errs...)
}

// isHostPort tells a TCP address such as "0.0.0.0:80" or "[::]:443".
func isHostPort(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
}

// Hash returns the fingerprint of the snapshot, computed on its canonical
// JSON without the values that change at every collection: collected_at and
// the durations, and the collector errors, whose text may vary (a timeout,
// a PID). Two snapshots of an unchanged host have the same hash.
func (s *Snapshot) Hash() (string, error) {
	c := *s
	c.CollectedAt = time.Time{}
	c.DurationMs = 0
	c.CollectorErrors = nil
	c.Collectors = make([]CollectorRun, len(s.Collectors))
	for i, run := range s.Collectors {
		run.DurationMs = 0
		c.Collectors[i] = run
	}
	b, err := json.Marshal(&c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

var (
	ruleHost   = regexp.MustCompile(`\bHost(?:SNI)?(Regexp)?\(([^)]*)\)`)
	backquoted = regexp.MustCompile("`[^`]*`")
)

// countDomains counts the names the webservers serve, and the regexes
// among them.
func (s *Snapshot) countDomains() (domains, regexes int) {
	for _, w := range s.Webservers {
		for _, v := range w.Vhosts {
			domains += len(v.ServerNames)
			for _, name := range v.ServerNames {
				if strings.HasPrefix(name, "~") {
					regexes++
				}
			}
		}
		for _, r := range w.Routers {
			for _, m := range ruleHost.FindAllStringSubmatch(r.Rule, -1) {
				n := len(backquoted.FindAllString(m[2], -1))
				domains += n
				if m[1] != "" {
					regexes += n
				}
			}
		}
	}
	return domains, regexes
}
