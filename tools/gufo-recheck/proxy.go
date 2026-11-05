package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"paracetamol/internal/catalog"
	"paracetamol/internal/gateway"
	"paracetamol/internal/project"
)

// This lifecycle is transport only. The operator owns the experimental engine,
// its model identity, resource limits, and exact container cleanup.
type externalLifecycle struct{ upstream *url.URL }

func (l externalLifecycle) Start(context.Context, gateway.Allocation) (*url.URL, error) {
	return l.upstream, nil
}

func (externalLifecycle) Stop(context.Context, gateway.Allocation) error { return nil }

func proxyRegistry(managed catalog.Catalog, dataRoot string) (gateway.Registry, error) {
	if err := verifyFrozenPolicy(managed); err != nil {
		return gateway.Registry{}, err
	}
	preset, ok := managed.LlamaPresets[regular]
	if !ok {
		return gateway.Registry{}, fmt.Errorf("missing regular Flash-Next catalog identity")
	}
	// Keep the complete regular bundle's existing target/projector receipt gates.
	// The llama.cpp identity supplies policy, not the experimental engine identity;
	// in particular its DFlash2 preset must not be used to describe Gufo MTP.
	managed.LlamaPresets = map[string]catalog.LlamaPreset{regular: preset}
	r, diagnostics, err := gateway.BuildRegistry(managed, dataRoot, []string{"llama-cpp"}, "strix-halo", nil, "rocm")
	if err != nil {
		return gateway.Registry{}, fmt.Errorf("receipt-backed registry unavailable: %w; diagnostics=%v", err, diagnostics)
	}
	if len(r.Models) != 1 || r.Models[0].ID != regular || r.Models[0].Context != 262144 || r.Models[0].SamplingPolicy != "qwen3.8-flash-next" {
		return gateway.Registry{}, fmt.Errorf("unexpected regular catalog context or policy")
	}
	return r, nil
}

func proxyHandler(r gateway.Registry, upstream *url.URL, log io.Writer) (*gateway.Server, *gateway.Scheduler, http.Handler, error) {
	scheduler, err := gateway.NewScheduler(externalLifecycle{upstream}, gateway.QueueLimit, map[gateway.Allocation]int{gateway.AllocationLlamaCPP: 1})
	if err != nil {
		return nil, nil, nil, err
	}
	server, err := gateway.NewServer(r, scheduler, log, gateway.ServerOptions{LlamaSlots: 1})
	if err != nil {
		_ = scheduler.Shutdown(context.Background())
		return nil, nil, nil, err
	}
	// Do not expose production UI, client configuration, statistics, or health
	// routes with metadata that could misidentify an experimental backend.
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		allowed := req.Method == http.MethodPost && req.URL.Path == "/v1/chat/completions" ||
			req.Method == http.MethodGet && (req.URL.Path == "/v1/models" || strings.HasPrefix(req.URL.Path, "/paracetamol/v1/requests/"))
		if !allowed {
			http.NotFound(w, req)
			return
		}
		server.ServeHTTP(w, req)
	})
	return server, scheduler, handler, nil
}

func serveProxy(ctx context.Context, listener net.Listener, r gateway.Registry, upstream *url.URL, stdout, stderr io.Writer) (result error) {
	server, scheduler, handler, err := proxyHandler(r, upstream, stderr)
	if err != nil {
		_ = listener.Close()
		return err
	}
	serverContext, cancel := context.WithCancel(ctx)
	httpServer := &http.Server{Handler: handler, BaseContext: func(net.Listener) context.Context { return serverContext }}
	defer func() {
		// Cancel active handlers even after Serve fails, then close connections
		// before draining scheduler leases. No inference-duration deadline applies.
		server.BeginShutdown()
		cancel()
		result = errors.Join(result, httpServer.Close(), scheduler.Shutdown(context.Background()))
	}()
	fmt.Fprintln(stderr, "WARNING: EXPERIMENTAL unmanaged backend transport; not managed Gufo support or inference acceptance.")
	fmt.Fprintln(stderr, "metadata_application=llama-cpp is policy provenance, NOT the actual experimental engine identity.")
	fmt.Fprintln(stderr, "The operator owns backend startup, model/MTP verification, resource limits, and exact cleanup.")
	fmt.Fprintf(stdout, "Experimental single-request proxy: http://%s -> %s\nRegular policy model: %s\nReceipt inventory: %s\n", listener.Addr(), upstream, regular, r.Fingerprint)
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	return nil
}

func runProxy(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("gufo-recheck proxy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	upstreamFlag := flags.String("upstream", "", "required already-running backend numeric loopback HTTP origin, without /v1")
	listen := flags.String("listen", "127.0.0.1:18557", "numeric loopback listener with explicit port")
	data := flags.String("data-dir", "", "required existing managed data root; receipts are read-only")
	experimental := flags.Bool("experimental", false, "required acknowledgment of unmanaged experimental transport")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || !*experimental || *data == "" {
		return fmt.Errorf("proxy requires --experimental, --upstream, --data-dir and no positional arguments")
	}
	upstream, err := origin(*upstreamFlag)
	if err != nil {
		return err
	}
	listenerOrigin, err := origin("http://" + *listen)
	if err != nil || listenerOrigin.Host == upstream.Host {
		return fmt.Errorf("listener must be a distinct numeric loopback address with explicit port")
	}
	info, err := os.Stat(*data)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("data root must already exist and be a directory")
	}
	root, err := project.Root()
	if err != nil {
		return err
	}
	managed, err := catalog.Load(filepath.Join(root, "catalog", "catalog.json"))
	if err != nil {
		return err
	}
	r, err := proxyRegistry(managed, *data)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	listener, err := net.Listen("tcp", listenerOrigin.Host)
	if err != nil {
		return err
	}
	return serveProxy(ctx, listener, r, upstream, stdout, stderr)
}
