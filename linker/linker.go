// Package linker computes the links of a namespace from its inventory: what
// each app connects to, and where each webserver sends its traffic. It is a
// pure function of the inventory, recomputed whole for a namespace when one
// of its hosts changes, so that an app enrolled before its database gets
// its link once the database comes.
package linker

import (
	"cmp"
	"encoding/json"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alexandreh2ag/lacis/cdn"
	"github.com/alexandreh2ag/lacis/common/snapshot"
	"github.com/alexandreh2ag/lacis/domainmatch"
	"github.com/alexandreh2ag/lacis/linkpath"
	"github.com/alexandreh2ag/lacis/model"
	"github.com/alexandreh2ag/lacis/pathmatch"
	"github.com/google/uuid"
)

// The rules a link was found by, in the order they are tried.
const (
	// RuleSocket is a unix socket of the same host.
	RuleSocket = "socket"
	// RuleLoopback is a loopback address, a port of the same host:
	// published docker ports included.
	RuleLoopback = "loopback"
	// RuleRoute is a name a webserver routes: Traefik Host() or vhost
	// server name.
	RuleRoute = "route"
	// RuleHostname is the name of a host.
	RuleHostname = "hostname"
	// RulePrivateIP is a private IP of a host sharing a network.
	RulePrivateIP = "private_ip"
	// RulePublicIP is an IP of the public network.
	RulePublicIP = "public_ip"
	// RuleVhost ties a webserver to the app its vhost serves.
	RuleVhost = "vhost"
	// RuleExternal is a target outside the inventory; Reason says why.
	RuleExternal = "external"
)

// The reasons of an external target.
const (
	ReasonUnparsed       = "unparsed"
	ReasonSocketNotFound = "socket_not_found"
	ReasonPortNotFound   = "port_not_listened"
	ReasonDNSFailed      = "dns_failed"
	ReasonCDN            = "cdn"
	ReasonPublic         = "public_ip"
	ReasonUnknownPrivate = "unknown_private_ip"
)

// Detail is what links.detail holds: how the link was found, and its
// proof.
type Detail struct {
	Rule string `json:"rule"`
	// Reason tells why a target is external.
	Reason string `json:"reason,omitempty"`
	// Key and Proof are the dependency of an app, and where it was read.
	Key   string          `json:"key,omitempty"`
	Proof json.RawMessage `json:"proof,omitempty"`
	// Target is the address as written: name, IP or socket, and port.
	Target string `json:"target,omitempty"`
	Port   int    `json:"port,omitempty"`
	// Path is the path of the HTTP URL called: the route it takes.
	Path string `json:"path,omitempty"`
	// Via is the IP a name resolved to.
	Via string `json:"via,omitempty"`
	// CDN is the CDN in front of the target: its name in linking.cdns, or
	// cdn for linking.cdn_ranges.
	CDN string `json:"cdn,omitempty"`
	// Routes are the routers, vhosts or frontends → backends sending the
	// traffic of a webserver; Domains the names they route.
	Routes  []string `json:"routes,omitempty"`
	Domains []string `json:"domains,omitempty"`
	// Matches are the names and paths each route serves, the paths
	// written as nginx locations ("/api" a prefix, "=/health" exact,
	// "~^/(a|b)" a regexp); none when every route serves any path.
	Matches []pathmatch.Match `json:"matches,omitempty"`
	// Listen are the ports these routes are reached on: the binds of an
	// HAProxy frontend, the listen of an nginx vhost; none when unknown.
	Listen []int `json:"listen,omitempty"`
}

// DefaultHTTPPorts are the HTTP ports when Options gives none.
var DefaultHTTPPorts = []int{80, 443}

// Options are the settings of a resolution.
type Options struct {
	// CDN are the CDNs and their IP ranges: a link through one of them
	// names it in its Via.
	CDN []cdn.CDN
	// HTTPPorts are the ports HTTP is served on, DefaultHTTPPorts when
	// empty: a name without scheme is routed by the webservers only on
	// them, and a port of a host nobody is known to listen on leads to its
	// webserver only on them.
	HTTPPorts []int
}

// Resolve computes the links of the inventory. The links have no id yet.
func Resolve(inv *model.Inventory, opts Options, now time.Time) []model.Link {
	r := newResolver(inv, opts)
	// The routes of the webservers first: a name several of them route
	// leads to the one in front, known by its routes to the others.
	r.vhosts()
	r.proxies()
	r.indexRoutes()
	r.dependencies()
	out := make([]model.Link, 0, len(r.links))
	for _, l := range r.links {
		l.link.NamespaceID, l.link.ComputedAt = inv.Namespace.ID, now
		if !slices.ContainsFunc(l.detail.Matches, func(m pathmatch.Match) bool { return len(m.Paths) > 0 }) {
			l.detail.Matches = nil // every route serves any path: the domains say it all
		}
		l.link.Detail, _ = json.Marshal(l.detail)
		out = append(out, l.link)
	}
	return out
}

// node is an end of a link.
type node struct {
	Type string
	ID   uuid.UUID
	// External is the target of an external node.
	External string
}

// target is where an address leads.
type target struct {
	node
	rule, confidence, reason, via, cdn string
}

type pending struct {
	link   model.Link
	detail Detail
}

// addMatches adds the routes of a link merged, once each. The vhost of an
// app adds none: the locations passing it the name tell its paths.
func (p *pending) addMatches(matches []pathmatch.Match) {
	for _, m := range matches {
		if !slices.ContainsFunc(p.detail.Matches, func(x pathmatch.Match) bool {
			return slices.Equal(x.Domains, m.Domains) && slices.Equal(x.Paths, m.Paths)
		}) {
			p.detail.Matches = append(p.detail.Matches, m)
		}
	}
}

type resolver struct {
	inv       *model.Inventory
	cdn       []cdn.CDN
	hosts     map[uuid.UUID]*model.Host
	hostNames map[string]*model.Host
	// ipHosts are the hosts holding an IP, ipNetworks the network of an IP
	// by its CIDR, hostNetworks the networks a host is in: by the network
	// of the CIDR, the one it is in, which differs for a host moved into a
	// network split from it.
	ipHosts      map[netip.Addr][]uuid.UUID
	ipNetworks   map[netip.Addr]uuid.UUID
	hostNetworks map[uuid.UUID]map[uuid.UUID]uuid.UUID
	// localIPs are the hosts holding an IP on a local interface: only
	// these hosts reach it.
	localIPs  map[netip.Addr][]uuid.UUID
	endpoints map[uuid.UUID][]model.Endpoint
	// domains finds the webservers serving a name.
	domains    *domainmatch.Index[uuid.UUID]
	webservers map[uuid.UUID][]*model.Webserver
	// webserverHosts is the host of each webserver; listening the
	// webservers with a known TCP endpoint.
	webserverHosts map[uuid.UUID]uuid.UUID
	listening      map[uuid.UUID]bool
	httpPorts      map[int]bool
	links          []*pending
	merged         map[string]*pending
	// routed indexes the links from each webserver once their routes are
	// known; endsOf keeps where a webserver leads a name.
	routed map[uuid.UUID][]*pending
	endsOf map[string]string
	// ids name the ends whatever their hosts: the deployments of an app
	// served by each replica are one end.
	ids *linkpath.Identities
}

func newResolver(inv *model.Inventory, opts Options) *resolver {
	r := &resolver{inv: inv, cdn: opts.CDN, hosts: map[uuid.UUID]*model.Host{}, hostNames: map[string]*model.Host{},
		ipHosts: map[netip.Addr][]uuid.UUID{}, ipNetworks: map[netip.Addr]uuid.UUID{}, localIPs: map[netip.Addr][]uuid.UUID{},
		hostNetworks: map[uuid.UUID]map[uuid.UUID]uuid.UUID{}, endpoints: map[uuid.UUID][]model.Endpoint{},
		domains: domainmatch.NewIndex[uuid.UUID](), webservers: map[uuid.UUID][]*model.Webserver{},
		webserverHosts: map[uuid.UUID]uuid.UUID{}, listening: map[uuid.UUID]bool{}, httpPorts: map[int]bool{},
		merged: map[string]*pending{}, ids: linkpath.NewIdentities(inv)}
	ports := opts.HTTPPorts
	if len(ports) == 0 {
		ports = DefaultHTTPPorts
	}
	for _, p := range ports {
		r.httpPorts[p] = true
	}
	for i := range inv.Hosts {
		h := &inv.Hosts[i]
		r.hosts[h.ID] = h
		r.hostNames[strings.ToLower(h.Hostname)] = h
	}
	networks := NewNetworks(inv)
	for _, ip := range inv.IPs {
		if IsLocalInterface(deref(ip.Interface)) {
			// Every Docker host holds 172.17.0.1: no network is shared
			// through it.
			r.localIPs[ip.IP.Addr] = append(r.localIPs[ip.IP.Addr], ip.HostID)
			continue
		}
		r.ipHosts[ip.IP.Addr] = append(r.ipHosts[ip.IP.Addr], ip.HostID)
		if byCIDR, ok := networks.ByCIDR(ip.IP.Addr); ok {
			r.ipNetworks[ip.IP.Addr] = byCIDR
			if r.hostNetworks[ip.HostID] == nil {
				r.hostNetworks[ip.HostID] = map[uuid.UUID]uuid.UUID{}
			}
			r.hostNetworks[ip.HostID][byCIDR], _ = networks.Of(ip.HostID, ip.IP.Addr)
		}
	}
	for _, e := range inv.Endpoints {
		if e.Kind == model.EndpointKindDomain {
			r.domains.Add(e.Address, e.OwnerID)
			continue
		}
		r.endpoints[e.HostID] = append(r.endpoints[e.HostID], e)
		if e.Kind == model.EndpointKindTCP && e.OwnerType == model.EndpointOwnerWebserver {
			r.listening[e.OwnerID] = true
		}
	}
	for i := range inv.Webservers {
		w := &inv.Webservers[i]
		r.webservers[w.HostID] = append(r.webservers[w.HostID], w)
		r.webserverHosts[w.ID] = w.HostID
	}
	return r
}

// add records a link; with a merge key, a link of the same ends and source
// gets its routes and domains instead.
func (r *resolver) add(from node, to target, source string, detail Detail, merge bool) {
	if from.Type == to.Type && from.ID == to.ID && to.Type != model.LinkNodeExternal {
		return // a webserver proxying to itself
	}
	detail.Rule, detail.Reason = to.rule, to.reason
	if detail.Via == "" {
		detail.Via = to.via
	}
	detail.CDN = to.cdn
	key := strings.Join([]string{from.Type, from.ID.String(), to.Type, to.ID.String(), to.External, source}, "\x00")
	if p, ok := r.merged[key]; merge && ok {
		p.detail.Routes = appendNew(p.detail.Routes, detail.Routes...)
		p.detail.Domains = appendNew(p.detail.Domains, detail.Domains...)
		p.addMatches(detail.Matches)
		for _, port := range detail.Listen {
			if !slices.Contains(p.detail.Listen, port) {
				p.detail.Listen = append(p.detail.Listen, port)
			}
		}
		return
	}
	link := model.Link{FromType: from.Type, FromID: from.ID, ToType: to.Type, Source: source, Confidence: to.confidence}
	if to.Type == model.LinkNodeExternal {
		external := to.External
		link.ExternalTarget = &external
	} else {
		id := to.ID
		link.ToID = &id
	}
	p := &pending{link: link, detail: detail}
	r.links = append(r.links, p)
	if merge {
		r.merged[key] = p
	}
}

// indexRoutes indexes the links from the webservers, once they are all
// known.
func (r *resolver) indexRoutes() {
	r.routed, r.endsOf = map[uuid.UUID][]*pending{}, map[string]string{}
	for _, p := range r.links {
		if p.link.FromType == model.LinkNodeWebserver {
			r.routed[p.link.FromID] = append(r.routed[p.link.FromID], p)
		}
	}
}

func appendNew(list []string, values ...string) []string {
	for _, v := range values {
		if !slices.Contains(list, v) {
			list = append(list, v)
		}
	}
	return list
}

// dependencies links each app to what its dependencies target.
func (r *resolver) dependencies() {
	apps := map[uuid.UUID]*model.App{}
	for i := range r.inv.Apps {
		apps[r.inv.Apps[i].ID] = &r.inv.Apps[i]
	}
	for _, d := range r.inv.Dependencies {
		app, ok := apps[d.AppID]
		if !ok {
			continue
		}
		from := node{Type: model.LinkNodeApp, ID: app.ID}
		detail := Detail{Key: d.Key, Proof: d.Source}
		var t snapshot.Target
		if d.Unparsed || len(d.Target) == 0 || json.Unmarshal(d.Target, &t) != nil || t.Value == "" {
			r.add(from, r.external(r.local(app.HostID, d.Key), ReasonUnparsed), model.LinkSourceEnv, detail, false)
			continue
		}
		scheme, port := deref(d.Scheme), deref(d.Port)
		if port == 0 {
			port = DefaultPort(scheme)
		}
		detail.Target, detail.Port, detail.Path = t.Value, port, deref(d.URLPath)
		for _, to := range r.address(app.HostID, t, port, scheme, detail.Path) {
			r.add(from, to, model.LinkSourceEnv, detail, false)
		}
	}
}

// vhosts links each webserver to the apps its vhosts serve.
func (r *resolver) vhosts() {
	for _, app := range r.inv.Apps {
		for _, w := range r.webservers[app.HostID] {
			var served []string
			for _, name := range app.Vhosts {
				if r.serves(w.ID, strings.ToLower(name)) {
					served = append(served, strings.ToLower(name))
				}
			}
			if len(served) > 0 {
				to := target{node: node{Type: model.LinkNodeApp, ID: app.ID}, rule: RuleVhost, confidence: model.LinkConfidenceExact}
				r.add(node{Type: model.LinkNodeWebserver, ID: w.ID}, to, model.LinkSourceWebserver, Detail{Domains: served}, true)
			}
		}
	}
}

func (r *resolver) serves(webserverID uuid.UUID, name string) bool {
	return slices.Contains(r.domains.Lookup(name), webserverID)
}

// routing is the part of webservers.routing the links need.
type routing struct {
	Vhosts    []snapshot.Vhost        `json:"vhosts"`
	Upstreams []snapshot.Upstream     `json:"upstreams"`
	Routers   []snapshot.Router       `json:"routers"`
	Services  []snapshot.ProxyService `json:"services"`
	Frontends []snapshot.Frontend     `json:"frontends"`
	Backends  []snapshot.Backend      `json:"backends"`
}

// proxies links each webserver to what it proxies to: the upstreams of
// its vhosts, the services of its Traefik routers, the servers of its
// HAProxy backends.
func (r *resolver) proxies() {
	for i := range r.inv.Webservers {
		w := &r.inv.Webservers[i]
		var rt routing
		if json.Unmarshal(w.Routing, &rt) != nil {
			continue
		}
		from := node{Type: model.LinkNodeWebserver, ID: w.ID}
		source := model.LinkSourceWebserver
		if w.Type == "traefik" {
			source = model.LinkSourceTraefik
		}
		for _, v := range rt.Vhosts {
			locations := v.Locations
			if len(locations) == 0 {
				// An older agent: the paths are unknown.
				locations = []snapshot.Location{{Upstreams: v.Upstreams}}
			}
			for _, loc := range locations {
				var paths []string
				if loc.Path != "" {
					paths = []string{loc.Path}
				}
				for _, u := range loc.Upstreams {
					servers, via := rt.upstreamServers(u)
					r.proxy(w.HostID, from, source, servers, via, Detail{Routes: []string{v.ConfigFile}, Domains: v.ServerNames, Listen: listenPorts(v.Listen),
						Matches: []pathmatch.Match{{Domains: v.ServerNames, Paths: paths}}}, true)
				}
			}
		}
		for _, router := range rt.Routers {
			servers := rt.serviceServers(router.Service, map[string]bool{})
			domains := domainsOfRule(router.Rule)
			r.proxy(w.HostID, from, source, servers, server{}, Detail{Routes: []string{router.Name}, Domains: domains,
				Matches: []pathmatch.Match{{Domains: domains, Paths: pathsOfRule(router.Rule)}}}, false)
		}
		for _, f := range rt.Frontends {
			for _, name := range appendNew(nil, append([]string{f.DefaultBackend}, f.Backends...)...) {
				for _, b := range rt.Backends {
					if b.Name != name {
						continue
					}
					var servers []string
					for _, s := range b.Servers {
						servers = append(servers, s.Address)
					}
					r.proxy(w.HostID, from, source, servers, server{}, Detail{Routes: []string{f.Name + " → " + b.Name}, Listen: listenPorts(f.Bind)}, false)
				}
			}
		}
	}
}

// listenPorts reads the ports of nginx listen directives (80, 443 ssl,
// [::]:443, 10.0.0.1:8080) and HAProxy binds (0.0.0.0:3306, :80); a
// socket has none.
func listenPorts(listen []string) []int {
	var out []int
	for _, l := range listen {
		addr, _, _ := strings.Cut(strings.TrimSpace(l), " ")
		if strings.HasPrefix(addr, "unix:") || strings.HasPrefix(addr, "/") {
			continue
		}
		if i := strings.LastIndex(addr, ":"); i >= 0 {
			addr = addr[i+1:]
		}
		if port, err := strconv.Atoi(addr); err == nil && port > 0 && !slices.Contains(out, port) {
			out = append(out, port)
		}
	}
	return out
}

// proxy links a webserver to the servers it sends traffic to. With local
// set, a server of the host nobody listens on is left out: the vhost
// already ties the webserver to its app. via gives the scheme and the port
// of a server that has none: those of the proxy_pass naming its upstream
// block.
func (r *resolver) proxy(hostID uuid.UUID, from node, source string, servers []string, via server, detail Detail, local bool) {
	for _, s := range servers {
		srv, ok := parseServer(s)
		if !ok {
			continue
		}
		srv.Scheme = cmp.Or(srv.Scheme, via.Scheme)
		srv.Port = cmp.Or(srv.Port, via.Port)
		d := detail
		d.Target, d.Port = srv.Target.Value, srv.Port
		for _, to := range r.address(hostID, srv.Target, srv.Port, srv.Scheme, "") {
			if local && to.reason == ReasonPortNotFound {
				continue
			}
			r.add(from, to, source, d, true)
		}
	}
}

// nginxDefaultPort is the port of an nginx upstream server without one.
const nginxDefaultPort = 80

// upstreamServers gives the servers of a vhost upstream: those of the
// nginx upstream block it names, with the scheme of the upstream and the
// default port of nginx, else itself.
func (rt routing) upstreamServers(upstream string) ([]string, server) {
	name, scheme := upstream, ""
	if u, err := url.Parse(upstream); err == nil && u.Host != "" {
		name, scheme = u.Host, u.Scheme
	}
	for _, u := range rt.Upstreams {
		if u.Name == name {
			return u.Servers, server{Scheme: scheme, Port: nginxDefaultPort}
		}
	}
	return []string{upstream}, server{}
}

// serviceServers gives the servers of a Traefik service; a weighted or
// failover service lists the names of other services as servers.
func (rt routing) serviceServers(name string, seen map[string]bool) []string {
	if seen[name] {
		return nil
	}
	seen[name] = true
	var out []string
	for _, s := range rt.Services {
		if !sameService(s.Name, name) {
			continue
		}
		for _, server := range s.Servers {
			if rt.hasService(server) {
				out = append(out, rt.serviceServers(server, seen)...)
			} else {
				out = append(out, server)
			}
		}
	}
	return out
}

func (rt routing) hasService(name string) bool {
	for _, s := range rt.Services {
		if sameService(s.Name, name) {
			return true
		}
	}
	return false
}

// sameService compares Traefik service names, a name without provider
// matching any provider.
func sameService(a, b string) bool {
	if a == b {
		return true
	}
	an, _, aok := strings.Cut(a, "@")
	bn, _, bok := strings.Cut(b, "@")
	return an == bn && (!aok || !bok)
}

// server is a proxied server: its target, port and scheme, the last two
// zero when not written.
type server struct {
	Target snapshot.Target
	Port   int
	Scheme string
}

// parseServer reads a proxied server: "unix:/run/php/x.sock",
// "http://10.0.1.21", "http://[::1]", "127.0.0.1:9000", "10.0.1.41:3306".
func parseServer(s string) (server, bool) {
	if socket, ok := strings.CutPrefix(s, "unix:"); ok {
		return server{Target: snapshot.Target{Kind: "socket", Value: strings.TrimSuffix(socket, ":")}}, true
	}
	scheme := ""
	if u, err := url.Parse(s); err == nil && u.Scheme != "" && u.Host != "" {
		if u.Scheme == "unix" {
			return server{Target: snapshot.Target{Kind: "socket", Value: u.Path}}, true
		}
		scheme, s = strings.ToLower(u.Scheme), u.Host
	}
	host, portStr, err := net.SplitHostPort(s)
	port := 0
	if err != nil {
		host, port = s, DefaultPort(scheme)
		if inner, ok := strings.CutPrefix(host, "["); ok {
			host, ok = strings.CutSuffix(inner, "]")
			if !ok {
				return server{}, false
			}
		}
	} else if port, err = strconv.Atoi(portStr); err != nil {
		return server{}, false
	}
	if host == "" {
		return server{}, false
	}
	kind := "name"
	if _, err := netip.ParseAddr(host); err == nil {
		kind = "ip"
	}
	return server{Target: snapshot.Target{Kind: kind, Value: host}, Port: port, Scheme: scheme}, true
}

// httpSchemes are the schemes a webserver routes.
var httpSchemes = map[string]bool{"http": true, "https": true, "ws": true, "wss": true}

// isHTTP tells a call a webserver may route by its name: an HTTP scheme,
// or no scheme and an HTTP port or none. mysql://db.internal is never
// routed, even when a webserver serves *.internal.
func (r *resolver) isHTTP(scheme string, port int) bool {
	if scheme != "" {
		return httpSchemes[strings.ToLower(scheme)]
	}
	return port == 0 || r.httpPorts[port]
}

// address resolves a target seen from a host, by the rules in order; path
// is the path of the HTTP URL called.
func (r *resolver) address(fromHost uuid.UUID, t snapshot.Target, port int, scheme, path string) []target {
	switch t.Kind {
	case "socket":
		for _, e := range r.endpoints[fromHost] {
			if e.Kind == model.EndpointKindUnix && e.Address == t.Value {
				return []target{owner(e, RuleSocket, model.LinkConfidenceExact)}
			}
		}
		return []target{r.external(r.local(fromHost, "unix:"+t.Value), ReasonSocketNotFound)}
	case "ip":
		ip, err := netip.ParseAddr(t.Value)
		if err != nil {
			return []target{r.external(t.Value, ReasonUnknownPrivate)}
		}
		return r.ip(fromHost, ip.Unmap(), port)
	default:
		return r.name(fromHost, t, port, scheme, path)
	}
}

// localhost are the addresses localhost may resolve to.
var localhost = []netip.Addr{netip.AddrFrom4([4]byte{127, 0, 0, 1}), netip.IPv6Loopback()}

func (r *resolver) name(fromHost uuid.UUID, t snapshot.Target, port int, scheme, path string) []target {
	name := domainmatch.Normalize(t.Value)
	if name == "localhost" {
		return r.onHost(fromHost, localhost, port, RuleLoopback, model.LinkConfidenceExact, localhost[0].String())
	}
	var resolved []netip.Addr
	for _, s := range t.Resolved {
		if ip, err := netip.ParseAddr(s); err == nil {
			resolved = append(resolved, ip.Unmap())
		}
	}
	if r.isHTTP(scheme, port) {
		if out := r.route(name, path, resolved); len(out) > 0 {
			return out
		}
	}
	if h, ok := r.hostNames[name]; ok {
		return r.onHost(h.ID, nil, port, RuleHostname, model.LinkConfidenceExact, name)
	}
	if len(resolved) == 0 {
		return []target{r.external(name, ReasonDNSFailed)}
	}
	var out []target
	for _, ip := range resolved {
		for _, to := range r.ip(fromHost, ip, port) {
			if to.Type == model.LinkNodeExternal {
				// The name, not its IPs, is the external target.
				to.External, to.via = name, cmp.Or(to.via, ip.String())
			} else if to.via == "" {
				to.via = ip.String()
			}
			out = appendTarget(out, to)
		}
	}
	return out
}

func appendTarget(list []target, t target) []target {
	for _, x := range list {
		if x.node == t.node {
			return list
		}
	}
	return append(list, t)
}

// route returns the webservers routing a name. The configuration of a
// webserver tells it receives the name: when the name resolves to a host of
// the inventory, its webservers are kept, with their replicas, the
// webservers routing the name to the same ends; else, behind a CDN or not
// resolved, all of them. Only the webservers in front are kept, not the
// backends behind that also know the name. Webservers leading the name, on
// the path called, to different ends are ambiguous.
func (r *resolver) route(name, path string, resolved []netip.Addr) []target {
	var all []uuid.UUID
	for _, id := range r.domains.Lookup(name) {
		if !slices.Contains(all, id) {
			all = append(all, id)
		}
	}
	if len(all) == 0 {
		return nil
	}
	var reached []uuid.UUID
	for _, id := range all {
		for _, ip := range resolved {
			if slices.Contains(r.ipHosts[ip], r.webserverHosts[id]) && !slices.Contains(reached, id) {
				reached = append(reached, id)
			}
		}
	}
	front := r.inFront(all, name)
	candidates, confidence, behind := r.inFront(reached, name), model.LinkConfidenceExact, ""
	if len(reached) == 0 {
		candidates = front
		for _, ip := range resolved {
			if name := r.inCDN(ip); name != "" {
				behind = name
			}
		}
		if behind == "" {
			// Resolved outside the inventory, or not resolved: the
			// request may never reach the webservers.
			confidence = model.LinkConfidenceInferred
		}
	}
	ends := map[uuid.UUID]string{}
	for _, id := range front {
		ends[id] = r.ends(id, name, path)
	}
	if len(reached) > 0 {
		for _, id := range front {
			if !slices.Contains(candidates, id) && slices.ContainsFunc(candidates, func(c uuid.UUID) bool { return ends[c] == ends[id] }) {
				candidates = append(candidates, id)
			}
		}
	}
	for _, id := range candidates {
		if ends[id] != ends[candidates[0]] {
			confidence = model.LinkConfidenceAmbiguous
		}
	}
	out := make([]target, 0, len(candidates))
	for _, id := range candidates {
		out = append(out, target{node: node{Type: model.LinkNodeWebserver, ID: id}, rule: RuleRoute, confidence: confidence, cdn: behind})
	}
	return out
}

// ends tells where a webserver leads a name and a path, through the
// webservers it routes them to: the same ends make replicas, such as two nginx each serving
// their deployment of an app. A webserver routing the name nowhere known is
// its own end.
func (r *resolver) ends(webserverID uuid.UUID, name, path string) string {
	key := strings.Join([]string{webserverID.String(), name, path}, "\x00")
	if e, ok := r.endsOf[key]; ok && r.routed != nil {
		return e
	}
	var out []string
	seen := map[uuid.UUID]bool{webserverID: true}
	queue := []uuid.UUID{webserverID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		var routes []*pending
		var paths [][]string
		for _, p := range r.outgoing(cur) {
			if len(p.detail.Domains) == 0 || len(domainmatch.Routed([]string{name}, p.detail.Domains)) > 0 {
				routes, paths = append(routes, p), append(paths, pathmatch.For(p.detail.Matches, []string{name}))
			}
		}
		leaf := len(routes) == 0
		for _, i := range pathmatch.Select(path, paths) {
			l := routes[i].link
			if l.ToType == model.LinkNodeWebserver && l.ToID != nil {
				if !seen[*l.ToID] {
					seen[*l.ToID] = true
					queue = append(queue, *l.ToID)
				}
				continue
			}
			out = appendNew(out, r.ids.Of(l.ToType, l.ToID, l.ExternalTarget))
		}
		if leaf {
			out = appendNew(out, r.ids.Of(model.LinkNodeWebserver, &cur, nil))
		}
	}
	slices.Sort(out)
	e := strings.Join(out, "\x00")
	if r.routed != nil {
		r.endsOf[key] = e
	}
	return e
}

// outgoing gives the links from a webserver: from the index once the routes
// of the webservers are known, else from the links found so far.
func (r *resolver) outgoing(webserverID uuid.UUID) []*pending {
	if r.routed != nil {
		return r.routed[webserverID]
	}
	var out []*pending
	for _, p := range r.links {
		if p.link.FromType == model.LinkNodeWebserver && p.link.FromID == webserverID {
			out = append(out, p)
		}
	}
	return out
}

// inFront leaves out the webservers another of them routes the name to:
// the nginx behind a Traefik knows the name too, the request reaches the
// Traefik first.
func (r *resolver) inFront(ids []uuid.UUID, name string) []uuid.UUID {
	if len(ids) < 2 {
		return ids
	}
	var out []uuid.UUID
	for _, id := range ids {
		if !slices.ContainsFunc(ids, func(front uuid.UUID) bool { return front != id && r.routesTo(front, id, name) }) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return ids // webservers routing the name to one another
	}
	return out
}

// routesTo tells whether a webserver routes a name to another, directly
// or through other webservers.
func (r *resolver) routesTo(front, back uuid.UUID, name string) bool {
	seen := map[uuid.UUID]bool{front: true}
	queue := []uuid.UUID{front}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, p := range r.outgoing(cur) {
			l := p.link
			if l.ToType != model.LinkNodeWebserver || l.ToID == nil {
				continue
			}
			if len(p.detail.Domains) > 0 && len(domainmatch.Routed([]string{name}, p.detail.Domains)) == 0 {
				continue
			}
			if *l.ToID == back {
				return true
			}
			if !seen[*l.ToID] {
				seen[*l.ToID] = true
				queue = append(queue, *l.ToID)
			}
		}
	}
	return false
}

// ip resolves an IP seen from a host.
func (r *resolver) ip(fromHost uuid.UUID, ip netip.Addr, port int) []target {
	if ip.IsLoopback() || ip.IsUnspecified() {
		// 127.0.0.1:3306 does not reach a MySQL bound to 10.0.0.5; 0.0.0.0
		// reaches any address.
		var on []netip.Addr
		if ip.IsLoopback() {
			on = []netip.Addr{ip}
		}
		return r.onHost(fromHost, on, port, RuleLoopback, model.LinkConfidenceExact, ip.String())
	}
	public := isPublic(ip)
	rule := RulePrivateIP
	if public {
		rule = RulePublicIP
	}
	if slices.Contains(r.ipHosts[ip], fromHost) || slices.Contains(r.localIPs[ip], fromHost) {
		// An IP of the calling host is the host itself, whoever else holds
		// it: the 172.17.0.1 of docker0 on every Docker host.
		return r.onHost(fromHost, []netip.Addr{ip}, port, rule, model.LinkConfidenceExact, ip.String())
	}
	if name := r.inCDN(ip); name != "" {
		t := r.external(ip.String(), ReasonCDN)
		t.cdn = name
		return []target{t}
	}
	holders := r.ipHosts[ip]
	if !public {
		if shared := r.sharing(fromHost, ip, holders); len(shared) > 0 {
			holders = shared
		}
	}
	switch len(holders) {
	case 0:
		if public {
			return []target{r.external(ip.String(), ReasonPublic)}
		}
		return []target{r.external(ip.String(), ReasonUnknownPrivate)}
	case 1:
		return r.onHost(holders[0], []netip.Addr{ip}, port, rule, model.LinkConfidenceExact, ip.String())
	}
	var out []target
	for _, h := range holders {
		out = append(out, r.onHost(h, []netip.Addr{ip}, port, rule, model.LinkConfidenceAmbiguous, ip.String())...)
	}
	return out
}

// sharing keeps the holders of a private IP whose network the host is in:
// the same private IP may exist in two networks, told apart by CIDR or
// split by hand.
func (r *resolver) sharing(fromHost uuid.UUID, ip netip.Addr, holders []uuid.UUID) []uuid.UUID {
	byCIDR, ok := r.ipNetworks[ip]
	network, in := r.hostNetworks[fromHost][byCIDR]
	if !ok || !in {
		return nil
	}
	var out []uuid.UUID
	for _, h := range holders {
		if n, in := r.hostNetworks[h][byCIDR]; in && n == network {
			out = append(out, h)
		}
	}
	return out
}

// onHost finds what listens on a port of a host: on one of the IPs, or on
// any address when on is empty. A port nobody listens on leads to the
// webserver of the host for HTTP, else to the host itself.
func (r *resolver) onHost(hostID uuid.UUID, on []netip.Addr, port int, rule, confidence, written string) []target {
	var out []target
	for _, e := range r.endpoints[hostID] {
		if e.Kind != model.EndpointKindTCP || deref(e.Port) != port || !listensOn(e.Address, on) {
			continue
		}
		out = appendTarget(out, owner(e, rule, confidence))
	}
	if len(out) > 0 {
		return out
	}
	if rule == RuleLoopback {
		return []target{r.external(r.local(hostID, net.JoinHostPort(written, strconv.Itoa(port))), ReasonPortNotFound)}
	}
	if r.httpPorts[port] {
		if ws := r.silentWebserver(hostID); ws != nil {
			return []target{{node: node{Type: model.LinkNodeWebserver, ID: ws.ID}, rule: rule, confidence: model.LinkConfidenceInferred}}
		}
	}
	return []target{{node: node{Type: model.LinkNodeHost, ID: hostID}, rule: rule, confidence: model.LinkConfidenceInferred}}
}

// silentWebserver is the webserver of a host taken to serve an HTTP port
// nobody is known to listen on: the only one whose TCP endpoints are
// unknown, such as a Traefik whose entrypoints were not read. A webserver
// with known endpoints is found by them.
func (r *resolver) silentWebserver(hostID uuid.UUID) *model.Webserver {
	var found *model.Webserver
	for _, w := range r.webservers[hostID] {
		if r.listening[w.ID] {
			continue
		}
		if found != nil {
			return nil
		}
		found = w
	}
	return found
}

// listensOn tells an endpoint address reachable on one of the IPs: the
// same IP, a wildcard, or any address when on is empty.
func listensOn(address string, on []netip.Addr) bool {
	if len(on) == 0 {
		return true
	}
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return false
	}
	return addr.IsUnspecified() || slices.Contains(on, addr.Unmap())
}

func owner(e model.Endpoint, rule, confidence string) target {
	return target{node: node{Type: e.OwnerType, ID: e.OwnerID}, rule: rule, confidence: confidence}
}

func (r *resolver) external(value, reason string) target {
	return target{node: node{Type: model.LinkNodeExternal, External: value}, rule: RuleExternal,
		confidence: model.LinkConfidenceExact, reason: reason}
}

// local names an external target that only means something on its host,
// such as a socket: two hosts with the same socket path are two targets.
func (r *resolver) local(hostID uuid.UUID, value string) string {
	if h, ok := r.hosts[hostID]; ok {
		return h.Hostname + ":" + value
	}
	return value
}

// inCDN returns the name of the CDN holding ip; empty when none does.
func (r *resolver) inCDN(ip netip.Addr) string {
	for _, c := range r.cdn {
		if c.Contains(ip) {
			return c.Name
		}
	}
	return ""
}

// isPublic tells an IP routed on the Internet.
func isPublic(ip netip.Addr) bool {
	return !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified()
}

var (
	ruleHost   = regexp.MustCompile(`\bHost(?:SNI)?\(([^)]*)\)`)
	backquoted = regexp.MustCompile("`([^`]*)`")
)

var (
	rulePath = regexp.MustCompile("(!\\s*)?\\b(PathRegexp|PathPrefix|Path)\\(((?:\\s*`[^`]*`\\s*,?)+)\\)")
	// pathVar is a variable of a Traefik v2 path, {id} or {id:[0-9]+}.
	pathVar = regexp.MustCompile(`\{[^}:]+(?::([^}]*))?\}`)
)

// pathsOfRule reads the paths of a Traefik rule as nginx locations: none
// when it serves any path. A negated matcher is left out.
func pathsOfRule(rule string) []string {
	var out []string
	for _, m := range rulePath.FindAllStringSubmatch(rule, -1) {
		if m[1] != "" {
			continue
		}
		for _, q := range backquoted.FindAllStringSubmatch(m[3], -1) {
			out = appendNew(out, pathMatcher(m[2], q[1]))
		}
	}
	return out
}

// pathMatcher writes a matcher of a Traefik rule as an nginx location; a
// v2 path with variables becomes a regexp.
func pathMatcher(kind, value string) string {
	if kind == "PathRegexp" {
		return "~" + value
	}
	if pathVar.MatchString(value) {
		var b strings.Builder
		b.WriteString("^")
		last := 0
		for _, loc := range pathVar.FindAllStringSubmatchIndex(value, -1) {
			b.WriteString(regexp.QuoteMeta(value[last:loc[0]]))
			if loc[2] >= 0 {
				b.WriteString("(" + value[loc[2]:loc[3]] + ")")
			} else {
				b.WriteString("[^/]+")
			}
			last = loc[1]
		}
		b.WriteString(regexp.QuoteMeta(value[last:]))
		if kind == "Path" {
			b.WriteString("$")
		}
		return "~" + b.String()
	}
	if kind == "Path" {
		return "=" + value
	}
	return value
}

// domainsOfRule reads the names of the Host() of a Traefik rule.
func domainsOfRule(rule string) []string {
	var out []string
	for _, m := range ruleHost.FindAllStringSubmatch(rule, -1) {
		for _, q := range backquoted.FindAllStringSubmatch(m[1], -1) {
			if q[1] != "*" {
				out = appendNew(out, strings.ToLower(q[1]))
			}
		}
	}
	return out
}

// defaultPorts are the ports of the schemes a dependency may omit it for.
var defaultPorts = map[string]int{
	"http": 80, "https": 443, "ws": 80, "wss": 443,
	"postgres": 5432, "postgresql": 5432, "pgsql": 5432,
	"mysql": 3306, "mariadb": 3306, "mysqli": 3306, "pdo-mysql": 3306,
	"redis": 6379, "rediss": 6379, "memcached": 11211,
	"mongodb": 27017, "amqp": 5672, "amqps": 5671,
	"smtp": 25, "smtps": 465, "elasticsearch": 9200,
}

// DefaultPort is the port of a scheme, 0 when unknown.
func DefaultPort(scheme string) int {
	return defaultPorts[strings.ToLower(scheme)]
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
