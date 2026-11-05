package service

import "fmt"

// The reasons of the refusals a client tells in its own words.
const (
	ReasonPasswordTooShort        = "PASSWORD_TOO_SHORT"
	ReasonPasswordTooLong         = "PASSWORD_TOO_LONG"
	ReasonCurrentPasswordRequired = "CURRENT_PASSWORD_REQUIRED"
	ReasonCurrentPasswordWrong    = "CURRENT_PASSWORD_WRONG"
	ReasonInvalidUsername         = "INVALID_USERNAME"
	ReasonInvalidRoleCode         = "INVALID_ROLE_CODE"
	ReasonUserExists              = "USER_EXISTS"
	ReasonRoleExists              = "ROLE_EXISTS"
	ReasonLastAdmin               = "LAST_ADMIN"
	ReasonOwnAccount              = "OWN_ACCOUNT"
	ReasonSSOUser                 = "SSO_USER"
	ReasonNameTooLong             = "NAME_TOO_LONG"
	ReasonInvalidOIDCGroup        = "INVALID_OIDC_GROUP"
	ReasonInactiveBeforeStale     = "INACTIVE_BEFORE_STALE"
	ReasonDurationTooShort        = "DURATION_TOO_SHORT"
	ReasonNetworkExists           = "NETWORK_EXISTS"
	ReasonPublicNetworkSplit      = "PUBLIC_NETWORK_SPLIT"
	ReasonNameNeedsLetter         = "NAME_NEEDS_LETTER"
	ReasonNoHost                  = "NO_HOST"
	ReasonHostNotInRange          = "HOST_NOT_IN_RANGE"
	ReasonHostAlreadyIn           = "HOST_ALREADY_IN"
	ReasonHostNotIn               = "HOST_NOT_IN"
	ReasonNetworkNotSplit         = "NETWORK_NOT_SPLIT"
	ReasonHostNotFound            = "HOST_NOT_FOUND"
	ReasonProjectInUse            = "PROJECT_IN_USE"
	ReasonEnvironmentInUse        = "ENVIRONMENT_IN_USE"
)

// Refusal is a refusal of a service that a client can tell in its own
// words: Reason names it, Args are the values it names. It is Err, so
// errors.Is still sorts it, and reads as fmt.Errorf("%w: "+format) did.
type Refusal struct {
	Err    error
	Reason string
	Args   map[string]any
	Detail string
}

func (r *Refusal) Error() string { return r.Err.Error() + ": " + r.Detail }

func (r *Refusal) Unwrap() error { return r.Err }

// refuse is a Refusal of the kind err.
func refuse(err error, reason string, args map[string]any, format string, a ...any) error {
	return &Refusal{Err: err, Reason: reason, Args: args, Detail: fmt.Sprintf(format, a...)}
}
