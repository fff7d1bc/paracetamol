package cli

import (
	"errors"
	"fmt"
	"net"
)

const gatewayLoopbackAddress = "127.0.0.1"

type gatewayListenBinding struct {
	network string
	host    string
}

type gatewayListenFunc func(network, address string) (net.Listener, error)

// gatewayListenBindings preserves the stable IPv4 loopback control path while
// adding exactly the requested publication address. IPv4 wildcard publication
// already contains loopback. IPv6 uses an explicit tcp6 socket so its
// dual-stack behavior never determines whether managed local clients work.
// runGateway validates requested as an IP address before reaching this plan.
func gatewayListenBindings(requested string) []gatewayListenBinding {
	ip := net.ParseIP(requested)
	local := gatewayListenBinding{network: "tcp4", host: gatewayLoopbackAddress}
	if ipv4 := ip.To4(); ipv4 != nil {
		host := ipv4.String()
		if ipv4.IsUnspecified() || host == gatewayLoopbackAddress {
			return []gatewayListenBinding{{network: "tcp4", host: host}}
		}
		return []gatewayListenBinding{local, {network: "tcp4", host: host}}
	}
	return []gatewayListenBinding{local, {network: "tcp6", host: ip.String()}}
}

func openGatewayListeners(requested string, port int, listen gatewayListenFunc) ([]net.Listener, error) {
	bindings := gatewayListenBindings(requested)
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
