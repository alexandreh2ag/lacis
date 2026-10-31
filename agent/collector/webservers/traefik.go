package webservers

import (
	stdContext "context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
)

const (
	TypeTraefik = "traefik"
	// DefaultTraefikAPI is used when collectors.traefik.api_url is not set.
	DefaultTraefikAPI = "http://127.0.0.1:8080"
	// maxTraefikBody bounds the answers read from the API.
	maxTraefikBody = 32 << 20
)

// traefikRawData is what /api/rawdata gives of the dynamic configuration.
type traefikRawData struct {
	Routers     map[string]traefikRouter  `json:"routers"`
	Services    map[string]traefikService `json:"services"`
	TCPRouters  map[string]traefikRouter  `json:"tcpRouters"`
	TCPServices map[string]traefikService `json:"tcpServices"`
}

type traefikRouter struct {
	EntryPoints []string        `json:"entryPoints"`
	Service     string          `json:"service"`
	Rule        string          `json:"rule"`
	TLS         json.RawMessage `json:"tls"`
}

type traefikService struct {
	LoadBalancer *struct {
		Servers []struct {
			URL     string `json:"url"`
			Address string `json:"address"`
		} `json:"servers"`
	} `json:"loadBalancer"`
	// The other kinds route to services rather than servers.
	Weighted *struct {
		Services []struct {
			Name string `json:"name"`
		} `json:"services"`
	} `json:"weighted"`
	Mirroring *struct {
		Service string `json:"service"`
		Mirrors []struct {
			Name string `json:"name"`
		} `json:"mirrors"`
	} `json:"mirroring"`
	Failover *struct {
		Service  string `json:"service"`
		Fallback string `json:"fallback"`
	} `json:"failover"`
}

// children returns the services a weighted, mirroring or failover service
// routes to.
func (s traefikService) children() []string {
	var out []string
	if s.Weighted != nil {
		for _, c := range s.Weighted.Services {
			out = append(out, c.Name)
		}
	}
	if s.Mirroring != nil {
		out = append(out, s.Mirroring.Service)
		for _, m := range s.Mirroring.Mirrors {
			out = append(out, m.Name)
		}
	}
	if s.Failover != nil {
		out = append(out, s.Failover.Service, s.Failover.Fallback)
	}
	return out
}

// readTraefik reads the routers and services of Traefik from its API: its
// routes often come from Docker labels, which only the API knows.
func readTraefik(stdCtx stdContext.Context, ctx *context.Context, _ *system.System, _ system.Process) (snapshot.Webserver, error) {
	ws := snapshot.Webserver{}
	base := strings.TrimSuffix(ctx.Config.Collectors.Traefik.APIURL, "/")
	if base == "" {
		base = DefaultTraefikAPI
	}
	var version struct {
		Version string `json:"Version"`
	}
	if err := getJSON(stdCtx, base+"/api/version", &version); err != nil {
		return ws, err
	}
	ws.Version = version.Version

	var raw traefikRawData
	if err := getJSON(stdCtx, base+"/api/rawdata", &raw); err != nil {
		return ws, err
	}
	ws.Routers = append(traefikRouters(raw.Routers), traefikRouters(raw.TCPRouters)...)
	ws.Services = append(traefikServices(raw.Services), traefikServices(raw.TCPServices)...)
	return ws, nil
}

func traefikRouters(routers map[string]traefikRouter) []snapshot.Router {
	var out []snapshot.Router
	for _, name := range sortedKeys(routers) {
		r := routers[name]
		out = append(out, snapshot.Router{
			Name:        name,
			Rule:        r.Rule,
			Entrypoints: r.EntryPoints,
			Service:     qualify(r.Service, name),
			TLS:         len(r.TLS) > 0 && string(r.TLS) != "null",
		})
	}
	return out
}

func traefikServices(services map[string]traefikService) []snapshot.ProxyService {
	var out []snapshot.ProxyService
	for _, name := range sortedKeys(services) {
		s := snapshot.ProxyService{Name: name, Servers: []string{}}
		svc := services[name]
		if lb := svc.LoadBalancer; lb != nil {
			for _, server := range lb.Servers {
				if server.URL != "" {
					s.Servers = append(s.Servers, server.URL)
				} else if server.Address != "" {
					s.Servers = append(s.Servers, server.Address)
				}
			}
		}
		// The child services are listed by name: the server follows them.
		for _, child := range svc.children() {
			if child != "" {
				s.Servers = appendUnique(s.Servers, qualify(child, name))
			}
		}
		out = append(out, s)
	}
	return out
}

// qualify names a service with its provider: a service of the same
// provider as the router or service referring to it is named without it.
func qualify(service, referrer string) string {
	if _, provider, ok := strings.Cut(referrer, "@"); ok && !strings.Contains(service, "@") {
		return service + "@" + provider
	}
	return service
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// getJSON decodes the answer of a GET on url into out.
func getJSON(stdCtx stdContext.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(stdCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxTraefikBody)).Decode(out); err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	return nil
}
