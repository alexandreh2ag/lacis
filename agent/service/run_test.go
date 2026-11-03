package service

import (
	stdContext "context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/alexandreh2ag/lacis/agent/client"
	"github.com/alexandreh2ag/lacis/agent/context"
	mockClient "github.com/alexandreh2ag/lacis/agent/mocks/client"
	"github.com/alexandreh2ag/lacis/agent/state"
	"github.com/alexandreh2ag/lacis/common/api"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// fakeClock records the waits, and cancels the run on the wait number
// stopAt (1-based).
type fakeClock struct {
	waits  []time.Duration
	stopAt int
	cancel stdContext.CancelFunc
}

func (c *fakeClock) Now() time.Time { return time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC) }

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.waits = append(c.waits, d)
	if len(c.waits) >= c.stopAt {
		c.cancel()
		return nil
	}
	ch := make(chan time.Time, 1)
	ch <- c.Now()
	return ch
}

var (
	errUnavailable = &client.Error{Status: 503}
	errNetwork     = &url.Error{Op: "Post", URL: "https://lacis.example.com", Err: errors.New("connection refused")}
	errRefused     = &client.Error{Status: 401, Code: api.ErrCodeInvalidToken}
	errRejected    = &client.Error{Status: 422, Code: api.ErrCodeInvalidSchema}
)

const testJitter = 7 * time.Second

// runContext returns an agent context with a hostname, enrolled when
// enrolled is set, and a clock stopping the run on the wait stopAt.
func runContext(t *testing.T, enrolled bool, stopAt int) (stdContext.Context, *context.Context, *fakeClock) {
	previous := jitter
	jitter = func(time.Duration) time.Duration { return testJitter }
	t.Cleanup(func() { jitter = previous })

	stdCtx, cancel := stdContext.WithCancel(t.Context())
	t.Cleanup(cancel)
	ctx := context.TestContext(nil)
	ctx.Config.Interval = time.Hour
	ctx.Config.Auth.StateDir = "/var/lib/lacis"
	ctx.Config.Auth.EnrollmentToken = "lacis_et_secret"
	clk := &fakeClock{stopAt: stopAt, cancel: cancel}
	ctx.Clock = clk
	require.NoError(t, afero.WriteFile(ctx.Fs, HostnameFile, []byte("acme-site-prod-1\n"), 0o644))
	if enrolled {
		require.NoError(t, state.Save(ctx.Fs, "/var/lib/lacis", &state.Identity{AgentID: "019a", AgentToken: "lacis_at_secret"}))
	}
	return stdCtx, ctx, clk
}

func TestRun_Loop(t *testing.T) {
	stdCtx, ctx, clk := runContext(t, true, 6)
	c := mockClient.NewMockClient(gomock.NewController(t))
	next := time.Hour + testJitter
	gomock.InOrder(
		c.EXPECT().SendSnapshot(gomock.Any(), "lacis_at_secret", gomock.Any()).Return(api.SnapshotAccepted, nil),
		c.EXPECT().SendSnapshot(gomock.Any(), gomock.Any(), gomock.Any()).Return(api.SnapshotStatus(""), errUnavailable),
		c.EXPECT().SendSnapshot(gomock.Any(), gomock.Any(), gomock.Any()).Return(api.SnapshotStatus(""), errNetwork),
		c.EXPECT().SendSnapshot(gomock.Any(), gomock.Any(), gomock.Any()).Return(api.SnapshotUnchanged, nil),
		c.EXPECT().SendSnapshot(gomock.Any(), gomock.Any(), gomock.Any()).Return(api.SnapshotStatus(""), errRejected),
		c.EXPECT().SendSnapshot(gomock.Any(), gomock.Any(), gomock.Any()).Return(api.SnapshotAccepted, nil),
	)

	require.NoError(t, Run(stdCtx, ctx, c))
	assert.Equal(t, []time.Duration{next, MinBackoff, 2 * MinBackoff, next, next, next}, clk.waits,
		"backoff on transient failures, reset on success; a rejected snapshot waits the interval")
}

func TestRun_Enroll(t *testing.T) {
	stdCtx, ctx, clk := runContext(t, false, 2)
	c := mockClient.NewMockClient(gomock.NewController(t))
	gomock.InOrder(
		c.EXPECT().Enroll(gomock.Any(), "lacis_et_secret").Return(nil, errUnavailable),
		c.EXPECT().Enroll(gomock.Any(), "lacis_et_secret").Return(&api.EnrollResponse{AgentID: "019a", AgentToken: "lacis_at_new"}, nil),
		c.EXPECT().SendSnapshot(gomock.Any(), "lacis_at_new", gomock.Any()).Return(api.SnapshotAccepted, nil),
	)

	require.NoError(t, Run(stdCtx, ctx, c))
	assert.Equal(t, []time.Duration{MinBackoff, time.Hour + testJitter}, clk.waits)
}

func TestRun_CollectionFails(t *testing.T) {
	stdCtx, ctx, clk := runContext(t, true, 1)
	require.NoError(t, ctx.Fs.Remove(HostnameFile))
	require.NoError(t, Run(stdCtx, ctx, mockClient.NewMockClient(gomock.NewController(t))))
	assert.Equal(t, []time.Duration{time.Hour + testJitter}, clk.waits)
}

func TestRun_Stops(t *testing.T) {
	tests := []struct {
		name     string
		enrolled bool
		noToken  bool
		setup    func(c *mockClient.MockClient, cancel stdContext.CancelFunc)
		wantErr  error
		wantMsg  string
	}{
		{name: "no enrollment token", noToken: true, wantErr: ErrNoEnrollmentToken},
		{
			name: "enrollment token refused",
			setup: func(c *mockClient.MockClient, _ stdContext.CancelFunc) {
				c.EXPECT().Enroll(gomock.Any(), gomock.Any()).Return(nil, errRefused)
			},
			wantErr: ErrTokenRefused,
			wantMsg: "check auth.enrollment_token",
		},
		{
			name: "agent token refused",
			setup: func(c *mockClient.MockClient, _ stdContext.CancelFunc) {
				c.EXPECT().SendSnapshot(gomock.Any(), gomock.Any(), gomock.Any()).Return(api.SnapshotStatus(""), errRefused)
			},
			enrolled: true,
			wantErr:  ErrTokenRefused,
			wantMsg:  "remove identity.json and enroll it again",
		},
		{
			name: "stopped while enrolling",
			setup: func(c *mockClient.MockClient, cancel stdContext.CancelFunc) {
				c.EXPECT().Enroll(gomock.Any(), gomock.Any()).DoAndReturn(func(stdContext.Context, string) (*api.EnrollResponse, error) {
					cancel()
					return nil, stdContext.Canceled
				})
			},
		},
		{
			name: "stopped while sending",
			setup: func(c *mockClient.MockClient, cancel stdContext.CancelFunc) {
				c.EXPECT().SendSnapshot(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(stdContext.Context, string, any) (api.SnapshotStatus, error) {
					cancel()
					return "", stdContext.Canceled
				})
			},
			enrolled: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdCtx, ctx, clk := runContext(t, tt.enrolled, 100)
			if tt.noToken {
				ctx.Config.Auth.EnrollmentToken = ""
			}
			c := mockClient.NewMockClient(gomock.NewController(t))
			if tt.setup != nil {
				tt.setup(c, clk.cancel)
			}
			err := Run(stdCtx, ctx, c)
			if tt.wantErr == nil {
				assert.NoError(t, err)
				assert.Empty(t, clk.waits)
				return
			}
			assert.ErrorIs(t, err, tt.wantErr)
			assert.ErrorContains(t, err, tt.wantMsg)
		})
	}
}

func TestRun_StoppedDuringEnrollBackoff(t *testing.T) {
	stdCtx, ctx, clk := runContext(t, false, 1)
	c := mockClient.NewMockClient(gomock.NewController(t))
	c.EXPECT().Enroll(gomock.Any(), gomock.Any()).Return(nil, errNetwork)
	require.NoError(t, Run(stdCtx, ctx, c))
	assert.Equal(t, []time.Duration{MinBackoff}, clk.waits)
}

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		backoff, interval, want time.Duration
	}{
		{backoff: 0, interval: time.Hour, want: MinBackoff},
		{backoff: MinBackoff, interval: time.Hour, want: 2 * MinBackoff},
		{backoff: 4 * time.Minute, interval: time.Hour, want: MaxBackoff},
		{backoff: 40 * time.Second, interval: time.Minute, want: time.Minute},
		{backoff: 0, interval: time.Second, want: MinBackoff},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, nextBackoff(tt.backoff, tt.interval), "%s / %s", tt.backoff, tt.interval)
	}
}

func TestJitter(t *testing.T) {
	assert.Zero(t, jitter(0))
	for range 100 {
		j := jitter(5 * time.Second)
		assert.True(t, j >= 0 && j <= 5*time.Second, j)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		value  string
		want   time.Duration
		wantOK bool
	}{
		{name: "absent"},
		{name: "seconds", value: "20", want: 20 * time.Second, wantOK: true},
		{name: "seconds with spaces", value: " 20 ", want: 20 * time.Second, wantOK: true},
		{name: "huge seconds", value: "99999999999", want: MaxBackoff, wantOK: true},
		{name: "zero", value: "0"},
		{name: "negative", value: "-5"},
		{name: "http date", value: "Tue, 27 Oct 2026 12:01:30 GMT", want: 90 * time.Second, wantOK: true},
		{name: "past http date", value: "Tue, 27 Oct 2026 11:00:00 GMT"},
		{name: "invalid", value: "soon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tt.value, now)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRetryDelay(t *testing.T) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		err     error
		backoff time.Duration
		want    time.Duration
	}{
		{name: "network error", err: errNetwork, want: MinBackoff},
		{name: "no Retry-After", err: errUnavailable, backoff: MinBackoff, want: 2 * MinBackoff},
		{name: "Retry-After honoured", err: &client.Error{Status: 429, RetryAfter: "20"}, backoff: 2 * time.Minute, want: 20 * time.Second},
		{name: "Retry-After capped", err: &client.Error{Status: 429, RetryAfter: "3600"}, want: MaxBackoff},
		{name: "Retry-After date", err: &client.Error{Status: 503, RetryAfter: "Tue, 27 Oct 2026 12:00:45 GMT"}, want: 45 * time.Second},
		{name: "invalid Retry-After", err: &client.Error{Status: 429, RetryAfter: "later"}, want: MinBackoff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, retryDelay(tt.err, tt.backoff, time.Hour, now))
		})
	}
}

func TestRun_TooSoon(t *testing.T) {
	stdCtx, ctx, clk := runContext(t, true, 2)
	c := mockClient.NewMockClient(gomock.NewController(t))
	c.EXPECT().SendSnapshot(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(api.SnapshotStatus(""), &client.Error{Status: 429, Code: api.ErrCodeTooSoon, RetryAfter: "20"})
	c.EXPECT().SendSnapshot(gomock.Any(), gomock.Any(), gomock.Any()).Return(api.SnapshotStatus("accepted"), nil)
	require.NoError(t, Run(stdCtx, ctx, c))
	assert.Equal(t, []time.Duration{20 * time.Second, time.Hour + testJitter}, clk.waits)
}
