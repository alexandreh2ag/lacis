package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/alexandreh2ag/lacis/common/dsn"
)

var (
	ErrSchemaVersion = errors.New("unsupported schema version")
	ErrInvalid       = errors.New("invalid snapshot")
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
	for i, c := range s.Collectors {
		switch c.Status {
		case CollectorStatusOK, CollectorStatusSkipped, CollectorStatusDisabled, CollectorStatusError:
		default:
			add("collectors[%d].status %q is invalid", i, c.Status)
		}
	}
	return errors.Join(errs...)
}

// Hash returns the fingerprint of the snapshot, computed on its canonical
// JSON without the values that change at every collection: collected_at and
// the durations. Two snapshots of an unchanged host have the same
// hash.
func (s *Snapshot) Hash() (string, error) {
	c := *s
	c.CollectedAt = time.Time{}
	c.DurationMs = 0
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
