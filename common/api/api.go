// Package api describes the agent API: routes, payloads, headers and error
// codes shared by the agent, which calls it, and the server, which exposes it.
package api

const (
	// PathEnroll and PathSnapshot are relative to the server base path.
	PathEnroll   = "api/v1/agent/enroll"
	PathSnapshot = "api/v1/agent/snapshot"

	HeaderAuthorization   = "Authorization"
	HeaderContentType     = "Content-Type"
	HeaderContentEncoding = "Content-Encoding"
	HeaderSchemaVersion   = "X-Lacis-Schema-Version"
	HeaderAgentVersion    = "X-Lacis-Agent-Version"

	ContentTypeJSON = "application/json"
	EncodingGzip    = "gzip"

	// BearerPrefix precedes the enrollment token on enroll and the agent
	// token on snapshot.
	BearerPrefix = "Bearer "
)

// EnrollResponse answers an enrollment: a POST on PathEnroll, without body,
// authenticated with an enrollment token. It gives the agent its identity;
// the host and its namespace are created from its first snapshot. The agent stores both values
// in its state directory, readable by root only.
type EnrollResponse struct {
	AgentID    string `json:"agent_id"`
	AgentToken string `json:"agent_token"`
}

// SnapshotStatus tells what the server did with a snapshot.
type SnapshotStatus string

const (
	// SnapshotAccepted (202): stored, normalized in the background.
	SnapshotAccepted SnapshotStatus = "accepted"
	// SnapshotUnchanged (200): same hash as the previous snapshot, only the
	// last seen date moved.
	SnapshotUnchanged SnapshotStatus = "unchanged"
)

// SnapshotResponse answers a snapshot: a POST on PathSnapshot, authenticated
// with the agent token, whose body is the snapshot JSON compressed with gzip.
type SnapshotResponse struct {
	Status SnapshotStatus `json:"status"`
}

// ErrorCode is a stable code the clients can rely on, unlike messages.
type ErrorCode string

const (
	ErrCodeInvalidToken      ErrorCode = "invalid_token"
	ErrCodeNamespaceMismatch ErrorCode = "namespace_mismatch"
	ErrCodePayloadTooLarge   ErrorCode = "payload_too_large"
	ErrCodeInvalidSchema     ErrorCode = "invalid_schema"
	ErrCodeInvalidEncoding   ErrorCode = "invalid_encoding"
	ErrCodeInternal          ErrorCode = "internal_error"
)

// Error is the body of an error response of the agent API.
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message,omitempty"`
}
