---
sidebar_position: 5
title: Linking
---

# Linking

Links are the edges of the graph: what each app connects to, and where each
webserver sends its traffic. The server computes them from the inventory of a
namespace, never from the network.

## When links are computed

The links of a namespace are computed again **whole** each time one of its hosts
changes, so that an app enrolled before its database gets its link once the
database comes. The snapshots normalized within a **30-second window** share one
computation, run at the end of the window (`recompute_links` job).
Metadata changes that affect links (splitting a network, moving hosts, deleting a
host) also trigger it. `lacis-server snapshot normalize` computes the links of
every namespace in the process.

Each computation writes the links added and removed to the history of the
namespace (none for the very first computation of a namespace).

## Sources of links

| Source        | From → to |
| ------------- | --------- |
| app dependency (`env`) | an app → what a [dependency](./collectors.md#envfiles) read in its configuration targets |
| vhost (`webserver`)    | a webserver → the apps of its host whose vhost names it serves |
| proxy (`webserver`, `traefik`) | a webserver → the servers of its nginx/Apache upstreams, Traefik services and HAProxy backends |

## Resolution rules

A target is resolved **as seen from the host** of the app or webserver, by the
first rule that applies:

| Rule         | Target | Result |
| ------------ | ------ | ------ |
| `socket`     | a unix socket | the service or webserver of the same host listening on that path |
| `loopback`   | `127.0.0.1`, `::1`, `localhost`, `0.0.0.0` | what listens on that port on the same host, Docker published ports included |
| `route`      | an HTTP call to a name a webserver routes (vhost `server_name`, Traefik `Host()`) | that webserver: the reverse proxy in front |
| `hostname`   | the hostname of a host of the namespace | what listens on that port on that host |
| `private_ip` | a private IP held by a host sharing the network | what listens on that IP and port |
| `public_ip`  | a public IP held by a host of the inventory | what listens on that IP and port |
| `vhost`      | (webserver → app) | the app a vhost of the webserver serves |
| `external`   | anything else | an external node, with a reason |

Details of the rules:

- **Loopback.** `127.0.0.1:3306` only reaches what listens on that loopback
  address or on every address (`0.0.0.0`, `::`), not a MySQL bound to `10.0.0.5`;
  `0.0.0.0` as a target reaches any address of the host.
- **Route.** Only an HTTP call is routed by name: a scheme `http`, `https`, `ws` or
  `wss`, or no scheme with no port or a port of
  [`linking.http_ports`](./configuration/server.md#linking) (80 and 443 by default).
  `mysql://db.internal` is never routed, even when a webserver serves `*.internal`.
- **IPs of the calling host.** An IP the calling host holds is the host itself,
  whoever else holds it: the `172.17.0.1` of `docker0` on every Docker host.
- **nginx upstreams** without a port take port 80 and the scheme of the
  `proxy_pass` naming the upstream block.

Then, on the host found:

- the service, webserver or app listening on the port (on that IP, or on every
  address) is the target;
- for a port of `linking.http_ports` with nobody known to listen, the webserver of
  the host whose listen addresses are unknown (such as a Traefik whose entrypoints
  could not be read) is the target (`inferred`), when there is exactly one;
- otherwise the host itself is the target (`inferred`).

A name is matched against the routed domains, in lower case and without a trailing
dot:

| Pattern           | Serves |
| ----------------- | ------ |
| `www.example.com` | that name |
| `*.example.com`   | any name under `example.com`, at any depth, not `example.com` itself |
| `.example.com`    | `example.com` and any name under it |
| `~regex`          | names matching the regex (nginx regex server names, Traefik `HostRegexp`) |

When the name resolves to a host of the inventory, only the webservers of that host
are kept: the proxy in front, not the backends behind that know the name too.
Otherwise (a CDN in front, a name the DNS cannot resolve), a webserver another one
routes the name to is left out the same way: the nginx behind a Traefik. A
name the routes do not know is resolved through the IPs the agent resolved it to.

### Confidence

| Confidence  | Meaning |
| ----------- | ------- |
| `exact`     | one target, proven by an address listened on or a route on the host reached |
| `inferred`  | deduced: a route whose name resolves elsewhere (a CDN) or not at all, or a host with nobody listening on the port |
| `ambiguous` | several candidates: an IP held by several hosts, a name routed by several webservers |

The front draws the confidence as the dash pattern of the edge.

### External targets

| Reason               | Case |
| -------------------- | ---- |
| `unparsed`           | the value could not be parsed; only its key is known |
| `socket_not_found`   | no process of the host listens on that socket |
| `port_not_listened`  | a loopback address with nobody listening on the port |
| `dns_failed`         | a name the agent could not resolve |
| `cdn`                | an IP of a CDN of `linking.cdns` or `linking.cdn_ranges` |
| `public_ip`          | a public IP no host of the inventory holds (S3, Sentry, a SaaS…) |
| `unknown_private_ip` | a private IP no host of the namespace holds: a host without agent |

A socket or loopback target is named after its host (`acme-site-prod-1:unix:/run/redis.sock`):
two hosts with the same socket path are two targets.

## Effective links

A link reaching a webserver is how the request travels, not what the app needs:
`shop-front` calling `https://api.shop.example` reaches a Traefik, but uses the
`shop-api` behind it. The **effective links** go on through the webservers, along
the routes of the name called, to the app or the service at the end; the links
crossed are their **path**.

Through a webserver:

- only the routes reached on the port called are taken: the HAProxy frontend
  bound to it, the nginx vhosts listening on it (a route whose ports are unknown,
  such as a Traefik router, is kept);
- the routes of the name called are taken (`Host()`, `server_name`, the domains a
  link routes); without any, the routes of no name (a default vhost, an HAProxy
  backend);
- a call by IP takes the routes of no name, or the routes of names when they all
  lead to the same place: anything else would be a guess;
- when the routes reach an app, those reaching a service (the PHP-FPM running that
  app) are left out;
- with no route left, the webserver is the end of the path, a **dead end**;
- of several routes of one dependency to the same end, the shortest is kept.

The confidence of an effective link is the weakest of its path. The effective links
are given whatever the hosts, an app being its name in its project and env: one per
consumer, dependency and thing used, the hosts of each step of their routes kept as an
information. The front lists them in *Uses* and *Used by*, the proxies crossed in a
`via` chip and, unfolded, step by step; the page of a webserver keeps its own links.
In GraphQL, `App.uses` and `App.usedBy` (all the deployments of the app together),
`Service.usedBy` and `Webserver.usedBy` (the routes ending at it) give them as
`AppLink`s; `linkRoutes(ids)` gives their routes, step by step;
`impact(throughProxies: true)` crosses the webservers without counting them as a
level.

## CDN

Name the CDNs in front of your sites in [`linking.cdns`](./configuration/server.md#linking);
Lacis ships their IP ranges, updated with its releases. For a CDN it does not
know, list its ranges in `linking.cdn_ranges`:

```yaml
linking:
  cdns: [cloudflare]             # known: cloudflare
  cdn_ranges: [198.51.100.0/24]  # another CDN
```

An IP inside these ranges is never linked to a host by its IP. A name resolving
into a CDN range can still be linked through a **route**: the webservers routing
that name, with the confidence `inferred`. The detail of the link names the CDN
(`"cdn": "cloudflare"`, or `"cdn": "cdn"` for `linking.cdn_ranges`), and the
links of an app show it next to the rule.

## Networks

The server groups the IPs of a namespace into networks:

- a **public** IP goes to the `public` network of the namespace;
- a **private** IP goes to the known network whose CIDR holds it (the most specific
  CIDR wins), else to a new network created for its subnet, named after it
  (`10.0.1.0/24` → `10-0-1-0-24`).

The IPs of the bridges local to a host (`docker0`, `docker_gwbridge`, `br-*`,
`cni*`, `virbr*`) never make a network: every Docker host holds `172.17.0.1`, and
it links no host to another.

A private IP is only linked to the hosts **sharing the network** of the source.
Since the network of an IP is found by CIDR, two separate private networks on the
same range (two datacenters both on `10.0.1.0/24`) are one network, and an IP held
by a host in each is `ambiguous`.

### Split networks

To tell such networks apart, split the network in the front (namespace settings,
`metadata` `write` permission):

- **`splitNetwork`** moves hosts into a new network sharing the range;
- **`moveHostsToNetwork`** moves hosts between the networks of the range;
- **`deleteNetwork`** removes a split network, its hosts going back.

The hosts stay in their split network across their snapshots, and the links are
computed again: an IP held by a host of each network is no longer ambiguous. When
no host of the source's network holds the IP, the link falls back to every holder
elsewhere, `ambiguous`.

## Example data

The repository ships 25 snapshots of a fictitious namespace `acme` in
`testdata/snapshots`, covering every case above: a local Redis on a unix socket
and one in Docker on the loopback, a host reached by its hostname, a public IP
without route, a private IP without agent, a name the DNS cannot resolve, media
behind a CDN (set `linking.cdns: [cloudflare]`), and an unparsable
value.
It also ships 4 snapshots of a smaller namespace `globex` (a Laravel project in
two envs, a NestJS API and a shared MariaDB), to have a second namespace.

```bash
lacis-server snapshot import -c /etc/lacis testdata/snapshots
lacis-server snapshot normalize -c /etc/lacis
lacis-server graph export -c /etc/lacis --namespace acme --labels --layout elk
```
