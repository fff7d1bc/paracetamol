package cli

import (
	"cmp"
	"errors"
	"fmt"
	"net"
	"slices"

	"paracetamol/internal/config"
)

const gatewayLoopbackAddress = "127.0.0.1"

type gatewayListenBinding struct {
	network string
	host    string
}

type gatewayListenFunc func(network, address string) (net.Listener, error)

// gatewayListenBindings validates the complete publication set before sockets
// open, canonicalizes aliases, and keeps the IPv4 loopback control path.
// Wildcards subsume only their explicit socket family: tcp6 never depends on
// platform dual-stack policy to serve managed IPv4 clients.
func gatewayListenBindings(requested []string) ([]gatewayListenBinding, error) {
	local := gatewayListenBinding{network: "tcp4", host: gatewayLoopbackAddress}
	unique := map[gatewayListenBinding]bool{local: true}
	for _, address := range requested {
		if err := config.ValidateListenAddress(address); err != nil {
			return nil, fmt.Errorf("gateway listen address %q: %w", address, err)
		}
		ip := net.ParseIP(address)
		binding := gatewayListenBinding{network: "tcp6", host: ip.String()}
		if ipv4 := ip.To4(); ipv4 != nil {
			binding = gatewayListenBinding{network: "tcp4", host: ipv4.String()}
		}
		unique[binding] = true
	}
	wildcard4 := unique[gatewayListenBinding{network: "tcp4", host: "0.0.0.0"}]
	wildcard6 := unique[gatewayListenBinding{network: "tcp6", host: "::"}]
	bindings := make([]gatewayListenBinding, 0, len(unique))
	for binding := range unique {
		if binding.network == "tcp4" && binding.host != "0.0.0.0" && wildcard4 {
			continue
		}
		if binding.network == "tcp6" && binding.host != "::" && wildcard6 {
			continue
		}
		bindings = append(bindings, binding)
	}
	slices.SortFunc(bindings, func(a, b gatewayListenBinding) int {
		if a == local && b != local {
			return -1
		}
		if b == local && a != local {
			return 1
		}
		if order := cmp.Compare(a.network, b.network); order != 0 {
			return order
		}
		return cmp.Compare(a.host, b.host)
	})
	return bindings, nil
}

func openGatewayListeners(requested []string, port int, listen gatewayListenFunc) ([]net.Listener, error) {
	bindings, err := gatewayListenBindings(requested)
	if err != nil {
		return nil, err
	}
	listeners := make([]net.Listener, 0, len(bindings))
	for _, binding := range bindings {
		address := net.JoinHostPort(binding.host, fmt.Sprint(port))
		listener, err := listen(binding.network, address)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("listen for gateway on %s: %w", address, err), closeGatewayListeners(listeners))
		}
		listeners = append(listeners, listener)
	}
	return listeners, nil
}

func closeGatewayListeners(listeners []net.Listener) error {
	var result error
	for _, listener := range listeners {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func gatewayEndpoint(host string, port int) string {
	return "http://" + net.JoinHostPort(host, fmt.Sprint(port)) + "/v1"
}

func gatewayAdditionalEndpoint(requested string, port int) (string, string) {
	ip := net.ParseIP(requested)
	if ipv4 := ip.To4(); ipv4 != nil {
		host := ipv4.String()
		if host == gatewayLoopbackAddress {
			return "", ""
		}
		if ipv4.IsUnspecified() {
			return "Published on", net.JoinHostPort(host, fmt.Sprint(port)) + " · all IPv4 interfaces"
		}
		if ipv4.IsLoopback() {
			return "Additional endpoint", gatewayEndpoint(host, port)
		}
		return "Published endpoint", gatewayEndpoint(host, port)
	}
	host := ip.String()
	if ip.IsUnspecified() {
		return "Published on", net.JoinHostPort(host, fmt.Sprint(port)) + " · all IPv6 interfaces"
	}
	if ip.IsLoopback() {
		return "Additional endpoint", gatewayEndpoint(host, port)
	}
	return "Published endpoint", gatewayEndpoint(host, port)
}
