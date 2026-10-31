package docker

import (
	stdContext "context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
)

// maxBody bounds what is read of an answer of the Docker API.
const maxBody = 64 << 20

// apiContainer is what GET /containers/json gives of a running container.
type apiContainer struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	Labels map[string]string
	Ports  []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
	HostConfig struct {
		NetworkMode string `json:"NetworkMode"`
	} `json:"HostConfig"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

// apiInspect is what GET /containers/{id}/json gives of its image config.
type apiInspect struct {
	Config struct {
		Env []string `json:"Env"`
	} `json:"Config"`
}

// apiNetwork is a network of GET /networks.
type apiNetwork struct {
	ID      string            `json:"Id"`
	Name    string            `json:"Name"`
	Driver  string            `json:"Driver"`
	Options map[string]string `json:"Options"`
}

// client is a minimal, read-only client of the Docker API on its socket.
type client struct {
	http *http.Client
}

func newClient(socket string) *client {
	return &client{http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx stdContext.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}}}
}

// get decodes the answer of a GET on path into out.
func (c *client) get(stdCtx stdContext.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(stdCtx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("GET %s: %s: %s", path, resp.Status, body)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	return nil
}
