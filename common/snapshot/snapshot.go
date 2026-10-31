// Package snapshot is the contract between the agent and the server: the
// document an agent sends after each collection.
//
// A snapshot is complete and self-contained. The agent builds and serialises
// it; the server decodes it, validates it and computes the same Hash to skip
// a snapshot identical to the previous one.
package snapshot

import (
	"time"

	"github.com/alexandreh2ag/lacis/common/labels"
)

// SchemaVersion is the version of the snapshot format built by this code.
const SchemaVersion = 1

// AcceptedSchemaVersions are the versions the server accepts: the current one
// and the previous one, so that the server can be upgraded before the agents.
var AcceptedSchemaVersions = []int{SchemaVersion}

// IsAccepted reports whether a schema version is accepted.
func IsAccepted(version int) bool {
	for _, v := range AcceptedSchemaVersions {
		if v == version {
			return true
		}
	}
	return false
}

type Snapshot struct {
	SchemaVersion   int              `json:"schema_version"`
	Agent           AgentInfo        `json:"agent"`
	CollectedAt     time.Time        `json:"collected_at"`
	DurationMs      int64            `json:"duration_ms"`
	Namespace       string           `json:"namespace"`
	Host            Host             `json:"host"`
	Runtimes        []Runtime        `json:"runtimes"`
	Services        []Service        `json:"services"`
	Webservers      []Webserver      `json:"webservers"`
	Apps            []App            `json:"apps"`
	Collectors      []CollectorRun   `json:"collectors"`
	CollectorErrors []CollectorError `json:"collector_errors"`
}

// New returns an empty snapshot for a host, with non-nil lists so that the
// JSON always carries arrays.
func New(agent AgentInfo, l labels.Labels, hostname string) *Snapshot {
	return &Snapshot{
		SchemaVersion:   SchemaVersion,
		Agent:           agent,
		Namespace:       l.Namespace,
		Host:            Host{Hostname: hostname, Labels: l},
		Runtimes:        []Runtime{},
		Services:        []Service{},
		Webservers:      []Webserver{},
		Apps:            []App{},
		Collectors:      []CollectorRun{},
		CollectorErrors: []CollectorError{},
	}
}

type AgentInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

type Host struct {
	Hostname string        `json:"hostname"`
	Labels   labels.Labels `json:"labels"`
	OS       *OS           `json:"os,omitempty"`
	Kernel   string        `json:"kernel,omitempty"`
	// Arch is the machine architecture as uname -m gives it, e.g. x86_64.
	Arch           string      `json:"arch,omitempty"`
	Virtualization string      `json:"virtualization,omitempty"`
	CPU            *CPU        `json:"cpu,omitempty"`
	MemoryMB       *Memory     `json:"memory_mb,omitempty"`
	Disks          []Disk      `json:"disks,omitempty"`
	Interfaces     []Interface `json:"interfaces,omitempty"`
	// VirtualIPs are the keepalived virtual_ipaddress entries of the host.
	VirtualIPs []string `json:"virtual_ips,omitempty"`
}

type OS struct {
	ID      string   `json:"id"`
	IDLike  []string `json:"id_like,omitempty"`
	Version string   `json:"version,omitempty"`
	Pretty  string   `json:"pretty,omitempty"`
}

// ResourceSource tells where an effective resource value comes from.
type ResourceSource string

const (
	ResourceSourceProc   ResourceSource = "proc"
	ResourceSourceCgroup ResourceSource = "cgroup"
	ResourceSourceDocker ResourceSource = "docker"
)

type CPU struct {
	Model          string         `json:"model,omitempty"`
	VisibleCores   float64        `json:"visible_cores"`
	EffectiveCores float64        `json:"effective_cores"`
	Source         ResourceSource `json:"source"`
}

type Memory struct {
	Visible   int64          `json:"visible"`
	Effective int64          `json:"effective"`
	Source    ResourceSource `json:"source"`
}

// Disk is a mounted disk and its size; its usage is not collected, Lacis
// is not a monitoring tool.
type Disk struct {
	Mount  string `json:"mount"`
	SizeGB int64  `json:"size_gb"`
}

type Interface struct {
	Name string `json:"name"`
	// IPs are CIDR notations: the address with the prefix of its subnet.
	IPs          []string `json:"ips"`
	DefaultRoute bool     `json:"default_route,omitempty"`
}

type Runtime struct {
	Type    string `json:"type"`
	Version string `json:"version"`
	Path    string `json:"path"`
	Manager string `json:"manager,omitempty"`
	// Alias is the requested version when it is not a version, e.g. "lts/*"
	// in a .nvmrc.
	Alias string `json:"alias,omitempty"`
}

type Service struct {
	Type    string `json:"type"`
	Version string `json:"version,omitempty"`
	// Instance tells apart several instances of a type on one host, such as
	// PostgreSQL clusters. Empty means the default instance.
	Instance   string     `json:"instance,omitempty"`
	Listen     []string   `json:"listen,omitempty"`
	ConfigPath string     `json:"config_path,omitempty"`
	Cluster    string     `json:"cluster,omitempty"`
	Pools      []FPMPool  `json:"pools,omitempty"`
	Container  *Container `json:"container,omitempty"`
}

type FPMPool struct {
	Name   string `json:"name"`
	Listen string `json:"listen"`
	User   string `json:"user,omitempty"`
}

type Container struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Image    string   `json:"image"`
	Ports    []string `json:"ports,omitempty"`
	Networks []string `json:"networks,omitempty"`
}

type Webserver struct {
	Type    string `json:"type"`
	Version string `json:"version,omitempty"`
	// Vhosts are nginx server blocks and Apache virtual hosts.
	Vhosts []Vhost `json:"vhosts,omitempty"`
	// Upstreams are nginx upstream blocks.
	Upstreams []Upstream `json:"upstreams,omitempty"`
	// Routers and Services come from the Traefik API.
	Routers  []Router       `json:"routers,omitempty"`
	Services []ProxyService `json:"services,omitempty"`
	// Frontends and Backends come from haproxy.cfg.
	Frontends []Frontend `json:"frontends,omitempty"`
	Backends  []Backend  `json:"backends,omitempty"`
}

type Vhost struct {
	ServerNames []string `json:"server_names,omitempty"`
	Listen      []string `json:"listen,omitempty"`
	Root        string   `json:"root,omitempty"`
	// Upstreams are the fastcgi_pass, proxy_pass or ProxyPassMatch targets.
	Upstreams  []string `json:"upstreams,omitempty"`
	ConfigFile string   `json:"config_file,omitempty"`
}

type Upstream struct {
	Name    string   `json:"name"`
	Servers []string `json:"servers"`
}

type Router struct {
	Name        string   `json:"name"`
	Rule        string   `json:"rule"`
	Entrypoints []string `json:"entrypoints,omitempty"`
	Service     string   `json:"service"`
	TLS         bool     `json:"tls,omitempty"`
}

type ProxyService struct {
	Name    string   `json:"name"`
	Servers []string `json:"servers"`
}

type Frontend struct {
	Name           string   `json:"name"`
	Mode           string   `json:"mode,omitempty"`
	Bind           []string `json:"bind,omitempty"`
	DefaultBackend string   `json:"default_backend,omitempty"`
	Backends       []string `json:"backends,omitempty"`
}

type Backend struct {
	Name    string          `json:"name"`
	Mode    string          `json:"mode,omitempty"`
	Servers []BackendServer `json:"servers"`
}

type BackendServer struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// AppRole is the role of an app.
type AppRole string

const (
	AppRoleWeb    AppRole = "web"
	AppRoleWorker AppRole = "worker"
)

type App struct {
	// Path is the app root as seen by the webserver or launcher, e.g. a
	// "current" symlink; ResolvedPath is its target.
	Path         string      `json:"path"`
	ResolvedPath string      `json:"resolved_path,omitempty"`
	Name         string      `json:"name"`
	Role         AppRole     `json:"role"`
	Project      string      `json:"project"`
	Env          string      `json:"env"`
	Framework    *Framework  `json:"framework,omitempty"`
	Runtime      *AppRuntime `json:"runtime,omitempty"`
	Code         *Code       `json:"code,omitempty"`
	Vhosts       []string    `json:"vhosts,omitempty"`
	// Launchers are the programs started by a process manager in the app:
	// the server of a Node app, its cron, its queue consumers.
	Launchers    []Launcher   `json:"launchers"`
	Dependencies []Dependency `json:"dependencies"`
	// Info holds informative values such as APP_ENV and NODE_ENV. They are
	// never used to determine the env.
	Info map[string]string `json:"info,omitempty"`
}

type Framework struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type AppRuntime struct {
	Type    string `json:"type"`
	Version string `json:"version"`
	Path    string `json:"path,omitempty"`
	// Source tells how the runtime was found: fpm_pool, process, nvmrc.
	Source string `json:"source"`
}

// CodeSource tells where the code version of an app comes from.
type CodeSource string

const (
	CodeSourceDeployInfo CodeSource = "deploy_info"
	CodeSourceGit        CodeSource = "git"
)

type Code struct {
	Source      CodeSource `json:"source"`
	Version     string     `json:"version,omitempty"`
	Commit      string     `json:"commit,omitempty"`
	Branch      string     `json:"branch,omitempty"`
	Tag         string     `json:"tag,omitempty"`
	Repository  string     `json:"repository,omitempty"`
	PipelineID  string     `json:"pipeline_id,omitempty"`
	PipelineURL string     `json:"pipeline_url,omitempty"`
	DeployedAt  *time.Time `json:"deployed_at,omitempty"`
	DeployedBy  string     `json:"deployed_by,omitempty"`
}

type Launcher struct {
	Type string `json:"type"`
	Name string `json:"name"`
	// Role is web when the program listens on a TCP port, such as the
	// server of a Node app, and worker otherwise.
	Role      AppRole `json:"role"`
	Command   string  `json:"command,omitempty"`
	Directory string  `json:"directory,omitempty"`
	User      string  `json:"user,omitempty"`
}

// Dependency is a link from an app to something it connects to, read from its
// configuration and already stripped of any credential.
type Dependency struct {
	Key    string           `json:"key"`
	Source DependencySource `json:"source"`
	Scheme string           `json:"scheme,omitempty"`
	Port   int              `json:"port,omitempty"`
	Path   string           `json:"path,omitempty"`
	Target *Target          `json:"target,omitempty"`
	// Unparsed is true when the value could not be parsed: only the key is
	// sent.
	Unparsed bool `json:"unparsed,omitempty"`
}

// DependencySource is the proof of a dependency on the agent side.
type DependencySource struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	// KeyPath is the full key in a structured config, e.g.
	// "doctrine.connection.orm_default.params".
	KeyPath string `json:"key_path,omitempty"`
}

// Resolver tells how a name target was resolved on the host of the app.
type Resolver string

const (
	ResolverDNS    Resolver = "dns"
	ResolverHosts  Resolver = "hosts"
	ResolverFailed Resolver = "failed"
)

type Target struct {
	// Kind is one of dsn.KindIP, dsn.KindName, dsn.KindSocket.
	Kind     string   `json:"kind"`
	Value    string   `json:"value"`
	Resolved []string `json:"resolved,omitempty"`
	Resolver Resolver `json:"resolver,omitempty"`
}

// CollectorStatus is the outcome of a collector.
type CollectorStatus string

const (
	CollectorStatusOK       CollectorStatus = "ok"
	CollectorStatusSkipped  CollectorStatus = "skipped"
	CollectorStatusDisabled CollectorStatus = "disabled"
	CollectorStatusError    CollectorStatus = "error"
)

type CollectorRun struct {
	Name   string          `json:"name"`
	Status CollectorStatus `json:"status"`
	// DurationMs changes at every collection and is left out of the hash.
	DurationMs int64 `json:"duration_ms,omitempty"`
}

type CollectorError struct {
	Collector string `json:"collector"`
	Error     string `json:"error"`
}
