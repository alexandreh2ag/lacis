package model

// Inventory is the current content of a namespace: its removed rows left
// out. The links are computed from it, and the graph drawn from it.
type Inventory struct {
	Namespace    Namespace
	Networks     []Network
	CIDRs        []NetworkCIDR
	HostNetworks []HostNetwork
	Projects     []Project
	Environments []Environment
	Hosts        []Host
	IPs          []HostIP
	Runtimes     []Runtime
	Services     []Service
	Webservers   []Webserver
	Apps         []App
	Dependencies []AppDependency
	Endpoints    []Endpoint
	Links        []Link
}
