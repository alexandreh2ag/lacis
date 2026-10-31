// Package docker collects the containers of Docker: a container whose image
// is a supported service (postgres, redis, mariadb…) is reported as an
// instance of that service, with its published ports and networks. The
// bridges and veth of Docker are also removed from the host interfaces.
package docker

import (
	stdContext "context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/alexandreh2ag/lacis/agent/collector"
	"github.com/alexandreh2ag/lacis/agent/context"
	"github.com/alexandreh2ag/lacis/agent/system"
	"github.com/alexandreh2ag/lacis/common/snapshot"
)

const Name = "docker"

// imageServices map the image names, registry and tag removed, to the
// service types; bitnami images are named like the official ones.
var imageServices = map[string]string{
	"postgres": "postgresql", "postgresql": "postgresql", "postgis": "postgresql",
	"mysql": "mysql", "percona-server": "mysql", "mariadb": "mariadb",
	"redis": "redis", "redis-stack-server": "redis", "valkey": "redis",
	"mongo": "mongodb", "mongodb": "mongodb", "mongodb-community-server": "mongodb",
	"rabbitmq": "rabbitmq", "elasticsearch": "elasticsearch", "opensearch": "elasticsearch",
	"clickhouse-server": "clickhouse", "clickhouse": "clickhouse", "keycloak": "keycloak",
	"memcached": "memcached",
}

// versionEnv are the image variables holding the version, by service type.
var versionEnv = map[string][]string{
	"postgresql":    {"PG_VERSION", "PG_MAJOR"},
	"mysql":         {"MYSQL_VERSION", "MYSQL_MAJOR"},
	"mariadb":       {"MARIADB_VERSION", "MARIADB_MAJOR"},
	"redis":         {"REDIS_VERSION"},
	"mongodb":       {"MONGO_VERSION", "MONGO_MAJOR"},
	"rabbitmq":      {"RABBITMQ_VERSION"},
	"elasticsearch": {"ELASTIC_VERSION", "ELASTICSEARCH_VERSION"},
	"keycloak":      {"KC_VERSION", "KEYCLOAK_VERSION"},
	"memcached":     {"MEMCACHED_VERSION"},
}

// OCIVersionLabel is the version label of the OCI image annotations.
const OCIVersionLabel = "org.opencontainers.image.version"

func init() {
	collector.Register(Name, func(*context.Context) (collector.Collector, error) { return &Collector{}, nil })
}

type Collector struct{}

func (*Collector) Name() string { return Name }

// DependsOn: the docker interfaces are removed from those of the host,
// and the containers are added to the services, never concurrently.
func (*Collector) DependsOn() []string { return []string{"host", "services"} }

// Applicable when the Docker socket exists.
func (*Collector) Applicable(ctx *context.Context, _ *system.System) bool {
	socket := ctx.Config.Collectors.Docker.Socket
	if socket == "" {
		return false
	}
	_, err := ctx.Fs.Stat(socket)
	return err == nil
}

func (*Collector) Collect(stdCtx stdContext.Context, ctx *context.Context, _ *system.System, out *snapshot.Snapshot) error {
	c := newClient(ctx.Config.Collectors.Docker.Socket)
	var errs []error

	var networks []apiNetwork
	if err := c.get(stdCtx, "/networks", &networks); err != nil {
		errs = append(errs, err)
	} else {
		out.Host.Interfaces = withoutDockerInterfaces(out.Host.Interfaces, networks)
	}

	var containers []apiContainer
	if err := c.get(stdCtx, "/containers/json", &containers); err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, ct := range containers {
		typ := serviceType(ct.Image)
		if typ == "" {
			continue
		}
		svc := snapshot.Service{Type: typ, Container: container(ct)}
		svc.Instance = svc.Container.Name
		svc.Listen = published(ct)
		var inspect apiInspect
		if err := c.get(stdCtx, "/containers/"+ct.ID+"/json", &inspect); err != nil {
			errs = append(errs, err)
		}
		svc.Version = version(tagVersion(ct.Image), envVersion(typ, inspect.Config.Env), ct.Labels[OCIVersionLabel])
		out.Services = append(out.Services, svc)
	}
	return errors.Join(errs...)
}

// imageName returns the name of an image without its registry, path, tag
// and digest: docker.elastic.co/elasticsearch/elasticsearch:8.15.0 is
// elasticsearch.
func imageName(image string) string {
	image, _, _ = strings.Cut(image, "@")
	if i := strings.LastIndex(image, "/"); i >= 0 {
		image = image[i+1:]
	}
	name, _, _ := strings.Cut(image, ":")
	return name
}

func serviceType(image string) string {
	return imageServices[imageName(image)]
}

var tagNumber = regexp.MustCompile(`^v?(\d+(\.\d+)*)`)

// tagVersion reads the version of a tag: 16.4-bookworm is 16.4, 7-alpine
// is 7; latest or a digest give none.
func tagVersion(image string) string {
	image, _, _ = strings.Cut(image, "@")
	if i := strings.LastIndex(image, "/"); i >= 0 {
		image = image[i+1:]
	}
	_, tag, ok := strings.Cut(image, ":")
	if !ok {
		return ""
	}
	if m := tagNumber.FindStringSubmatch(tag); m != nil {
		return m[1]
	}
	return ""
}

// version picks the version of a container: the one of the image
// variables when it refines the tag (tag 17, PG_VERSION 17.2), else the
// tag, else the variables, else the OCI label.
func version(tag, env, label string) string {
	switch {
	case env != "" && (tag == "" || env == tag || strings.HasPrefix(env, tag+".")):
		return env
	case tag != "":
		return tag
	}
	return label
}

// envVersion reads the version in the variables of the image, without
// their packaging revision: PG_VERSION=16.4-1.pgdg120+1 is 16.4.
func envVersion(typ string, env []string) string {
	for _, name := range versionEnv[typ] {
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, name+"="); ok && v != "" {
				return system.UpstreamVersion(v)
			}
		}
	}
	return ""
}

func container(ct apiContainer) *snapshot.Container {
	name := ct.ID
	if len(ct.Names) > 0 {
		name = strings.TrimPrefix(ct.Names[0], "/")
	}
	c := &snapshot.Container{ID: shortID(ct.ID), Name: name, Image: ct.Image}
	for _, p := range ct.Ports {
		spec := fmt.Sprintf("%d/%s", p.PrivatePort, p.Type)
		if p.PublicPort != 0 {
			spec = hostAddr(p.IP, p.PublicPort) + "->" + spec
		}
		if !slices.Contains(c.Ports, spec) {
			c.Ports = append(c.Ports, spec)
		}
	}
	for name := range ct.NetworkSettings.Networks {
		c.Networks = append(c.Networks, name)
	}
	if len(c.Networks) == 0 && ct.HostConfig.NetworkMode != "" {
		c.Networks = []string{ct.HostConfig.NetworkMode}
	}
	slices.Sort(c.Ports)
	slices.Sort(c.Networks)
	return c
}

// published returns the host addresses a container publishes: what the
// other hosts connect to.
func published(ct apiContainer) []string {
	var out []string
	for _, p := range ct.Ports {
		if p.PublicPort == 0 || (p.Type != "" && p.Type != "tcp") {
			continue
		}
		if addr := hostAddr(p.IP, p.PublicPort); !slices.Contains(out, addr) {
			out = append(out, addr)
		}
	}
	slices.Sort(out)
	return out
}

func hostAddr(ip string, port int) string {
	switch {
	case ip == "":
		ip = "0.0.0.0"
	case strings.Contains(ip, ":"):
		ip = "[" + ip + "]"
	}
	return ip + ":" + strconv.Itoa(port)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// withoutDockerInterfaces removes the bridges of the Docker networks and the
// veth of the containers: their subnets are private to the host.
func withoutDockerInterfaces(interfaces []snapshot.Interface, networks []apiNetwork) []snapshot.Interface {
	bridges := map[string]bool{"docker0": true}
	for _, n := range networks {
		if n.Driver != "bridge" {
			continue
		}
		if name := n.Options["com.docker.network.bridge.name"]; name != "" {
			bridges[name] = true
		} else {
			bridges["br-"+shortID(n.ID)] = true
		}
	}
	return slices.DeleteFunc(slices.Clone(interfaces), func(i snapshot.Interface) bool {
		return bridges[i.Name] || strings.HasPrefix(i.Name, "veth")
	})
}
