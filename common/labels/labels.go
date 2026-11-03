// Package labels defines the host labels declared in the agent configuration.
//
// namespace, project and env are required: the agent refuses to start without
// them and the server rejects a snapshot that lacks one. index and role are
// optional; role is only used as a filter.
package labels

import (
	"errors"
	"fmt"
	"regexp"
)

const (
	KeyNamespace = "namespace"
	KeyProject   = "project"
	KeyEnv       = "env"
	KeyIndex     = "index"
	KeyRole      = "role"

	// MaxLength is the maximum length of a label value.
	MaxLength = 63
)

var (
	ErrRequired = errors.New("label is required")
	ErrInvalid  = errors.New("label is invalid")

	// slugRe matches the values that become slugs on the server (namespace,
	// project, env): lower case letters, digits, '.', '_' and '-', starting and
	// ending with a letter or a digit.
	slugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)
	// valueRe matches the free-form optional labels.
	valueRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)
)

// Labels are the labels of a host. They are the default values of the apps
// it runs; an app can override project and env with its .app.lacis file.
type Labels struct {
	Namespace string `json:"namespace" mapstructure:"namespace"`
	Project   string `json:"project" mapstructure:"project"`
	Env       string `json:"env" mapstructure:"env"`
	Index     string `json:"index,omitempty" mapstructure:"index"`
	Role      string `json:"role,omitempty" mapstructure:"role"`
}

// Validate checks that the required labels are set and that every label has
// a valid value. All the errors are returned, joined.
func (l Labels) Validate() error {
	var errs []error
	for _, f := range []struct {
		key, value string
		required   bool
		re         *regexp.Regexp
	}{
		{KeyNamespace, l.Namespace, true, slugRe},
		{KeyProject, l.Project, true, slugRe},
		{KeyEnv, l.Env, true, slugRe},
		{KeyIndex, l.Index, false, valueRe},
		{KeyRole, l.Role, false, valueRe},
	} {
		if f.value == "" {
			if f.required {
				errs = append(errs, fmt.Errorf("%s: %w", f.key, ErrRequired))
			}
			continue
		}
		if len(f.value) > MaxLength || !f.re.MatchString(f.value) {
			errs = append(errs, fmt.Errorf("%s %q: %w", f.key, f.value, ErrInvalid))
		}
	}
	return errors.Join(errs...)
}

// IsSlug reports whether value is valid for a required label.
func IsSlug(value string) bool {
	return len(value) <= MaxLength && slugRe.MatchString(value)
}
