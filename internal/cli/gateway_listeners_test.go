package cli

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestGatewayListenBindingSets(t *testing.T) {
	for _, test := range []struct {
		name      string
		requested []string
		expected  string
	}{
		{"nil default", nil, "tcp4/127.0.0.1"},
		{"empty default", []string{}, "tcp4/127.0.0.1"},
		{"local first", []string{"192.0.2.10", "10.0.0.2"}, "tcp4/127.0.0.1,tcp4/10.0.0.2,tcp4/192.0.2.10"},
		{"second loopback", []string{"127.0.0.2"}, "tcp4/127.0.0.1,tcp4/127.0.0.2"},
		{"deduplicate IPv4 mapped", []string{"::ffff:192.0.2.10", "192.0.2.10", "::ffff:7f00:1", "127.0.0.1"}, "tcp4/127.0.0.1,tcp4/192.0.2.10"},
		{"canonical IPv6", []string{"2001:0db8:0:0:0:0:0:2", "2001:db8::2", "0:0:0:0:0:0:0:1"}, "tcp4/127.0.0.1,tcp6/2001:db8::2,tcp6/::1"},
		{"mixed order", []string{"2001:db8::2", "192.0.2.10", "::1", "192.0.2.2"}, "tcp4/127.0.0.1,tcp4/192.0.2.10,tcp4/192.0.2.2,tcp6/2001:db8::2,tcp6/::1"},
		{"IPv4 wildcard first", []string{"0.0.0.0", "192.0.2.10", "127.0.0.2"}, "tcp4/0.0.0.0"},
		{"IPv4 wildcard last", []string{"127.0.0.2", "192.0.2.10", "0.0.0.0"}, "tcp4/0.0.0.0"},
		{"mapped wildcard", []string{"192.0.2.10", "::ffff:0.0.0.0", "0.0.0.0"}, "tcp4/0.0.0.0"},
		{"IPv6 wildcard first", []string{"::", "::1", "2001:db8::2", "192.0.2.10"}, "tcp4/127.0.0.1,tcp4/192.0.2.10,tcp6/::"},
		{"IPv6 wildcard last", []string{"192.0.2.10", "2001:db8::2", "::1", "::"}, "tcp4/127.0.0.1,tcp4/192.0.2.10,tcp6/::"},
		{"both wildcards", []string{"2001:db8::2", "192.0.2.10", "::", "0.0.0.0", "::"}, "tcp4/0.0.0.0,tcp6/::"},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := slices.Clone(test.requested)
			bindings, err := gatewayListenBindings(test.requested)
			if err != nil {
				t.Fatal(err)
			}
			var values []string
			for _, binding := range bindings {
				values = append(values, binding.network+"/"+binding.host)
			}
			if got := strings.Join(values, ","); got != test.expected {
				t.Fatalf("bindings=%q, want %q", got, test.expected)
			}
			if !slices.Equal(test.requested, original) {
				t.Fatalf("requested addresses mutated: %v", test.requested)
			}
		})
	}
}

func TestOpenGatewayListenersValidatesEveryAddressBeforeBinding(t *testing.T) {
	for _, address := range []string{"", "localhost", "gateway.example", "192.0.2.10:8080", "[::1]", "fe80::1%eth0", " 127.0.0.1"} {
		t.Run(address, func(t *testing.T) {
			calls := 0
			listeners, err := openGatewayListeners([]string{"0.0.0.0", "::", address}, 8080, func(network, address string) (net.Listener, error) {
				calls++
				return nil, errors.New("unexpected bind")
			})
			if err == nil || !strings.Contains(err.Error(), "listen address must be an IPv4 or IPv6 address") || calls != 0 || listeners != nil {
				t.Fatalf("listeners=%v err=%v calls=%d", listeners, err, calls)
			}
		})
	}
}

func TestOpenGatewayListenersRollsBackEveryEarlierBinding(t *testing.T) {
	bindErr := errors.New("fixture bind failure")
	closeErr := errors.New("fixture close failure")
	lastCloseErr := errors.New("last fixture close failure")
	first := &gatewayBindingTestListener{closeErr: closeErr}
	second := &gatewayBindingTestListener{closeErr: net.ErrClosed}
	third := &gatewayBindingTestListener{closeErr: lastCloseErr}
	earlier := []*gatewayBindingTestListener{first, second, third}
	var calls []string
	listeners, err := openGatewayListeners([]string{"2001:db8::2", "192.0.2.2", "192.0.2.1"}, 8080, func(network, address string) (net.Listener, error) {
		calls = append(calls, network+"/"+address)
		if len(calls) <= len(earlier) {
			return earlier[len(calls)-1], nil
		}
		return nil, bindErr
	})
	if listeners != nil || !errors.Is(err, bindErr) || !errors.Is(err, closeErr) || !errors.Is(err, lastCloseErr) || errors.Is(err, net.ErrClosed) {
		t.Fatalf("listeners=%v err=%v", listeners, err)
	}
	if !strings.Contains(err.Error(), "[2001:db8::2]:8080") {
		t.Fatalf("missing failed endpoint: %v", err)
	}
	for i, listener := range earlier {
		if listener.closeCalls != 1 {
			t.Fatalf("listener %d closed %d times", i, listener.closeCalls)
		}
	}
	if got := strings.Join(calls, ","); got != "tcp4/127.0.0.1:8080,tcp4/192.0.2.1:8080,tcp4/192.0.2.2:8080,tcp6/[2001:db8::2]:8080" {
		t.Fatalf("calls=%s", got)
	}
}

func TestOpenGatewayListenersRetainsSuccessfulBindings(t *testing.T) {
	var opened []*gatewayBindingTestListener
	listeners, err := openGatewayListeners([]string{"127.0.0.2", "::ffff:127.0.0.2"}, 8080, func(network, address string) (net.Listener, error) {
		listener := &gatewayBindingTestListener{}
		opened = append(opened, listener)
		return listener, nil
	})
	if err != nil || len(listeners) != 2 {
		t.Fatalf("listeners=%v err=%v", listeners, err)
	}
	for _, listener := range opened {
		if listener.closeCalls != 0 {
			t.Fatal("successful listener closed during startup")
		}
	}
	if err := closeGatewayListeners(listeners); err != nil {
		t.Fatal(err)
	}
	for _, listener := range opened {
		if listener.closeCalls != 1 {
			t.Fatalf("listener closed %d times", listener.closeCalls)
		}
	}
}

func TestGatewayListenersServeBothLoopbacksOnSamePortAndShutDown(t *testing.T) {
	// Reserve the port on the first actual listener, keeping it open while the
	// planner opens the second. No remote interfaces or inference are involved.
	first, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	port := first.Addr().(*net.TCPAddr).Port
	listeners, err := openGatewayListeners([]string{"127.0.0.2"}, port, func(network, address string) (net.Listener, error) {
		if address == first.Addr().String() {
			return first, nil
		}
		return net.Listen(network, address)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeGatewayListeners(listeners) })
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, "fixture models")
	})}
	t.Cleanup(func() { _ = server.Close() })
	results := make(chan error, len(listeners))
	for _, listener := range listeners {
		go func() { results <- server.Serve(listener) }()
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	for _, host := range []string{"127.0.0.1", "127.0.0.2"} {
		response, err := client.Get(gatewayEndpoint(host, port) + "/models")
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK || string(body) != "fixture models" {
			t.Fatalf("host=%s status=%d body=%q readErr=%v closeErr=%v", host, response.StatusCode, body, readErr, closeErr)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	for range listeners {
		select {
		case err := <-results:
			if !errors.Is(err, http.ErrServerClosed) {
				t.Fatalf("serve error=%v", err)
			}
		case <-ctx.Done():
			t.Fatal("listener did not stop after shutdown")
		}
	}
	for _, listener := range listeners {
		conn, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
		if err == nil {
			_ = conn.Close()
			t.Fatalf("listener remains available after shutdown: %s", listener.Addr())
		}
	}
}

type gatewayBindingTestListener struct {
	closeCalls int
	closeErr   error
}

func (*gatewayBindingTestListener) Accept() (net.Conn, error) {
	return nil, errors.New("unused")
}

func (listener *gatewayBindingTestListener) Close() error {
	listener.closeCalls++
	return listener.closeErr
}

func (*gatewayBindingTestListener) Addr() net.Addr { return &net.TCPAddr{} }
