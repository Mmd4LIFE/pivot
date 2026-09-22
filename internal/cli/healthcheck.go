package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

// newHealthcheckCmd builds `pivot healthcheck`.
//
// It exists for the container image. The runtime base is distroless: no shell,
// no curl, nothing to write a HEALTHCHECK with — so the binary probes itself,
// which is the usual answer and a better one than adding a shell back for the
// sake of one line.
//
// Exit status is the whole interface. Docker reads 0 as healthy and anything
// else as unhealthy, so the output is for a human reading `docker inspect` and
// the status is for the orchestrator.
func newHealthcheckCmd(env Env, flags *globalFlags) *cobra.Command {
	var (
		url     string
		timeout time.Duration
		ready   bool
	)

	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Probe a running Pivot and exit 0 if it is healthy",
		Long: `Probe a running Pivot server and exit 0 if it answers.

Intended for a container HEALTHCHECK, where there is no shell to run curl
from. The address comes from the same configuration the server uses, so
inside an image it needs no arguments at all.

By default it probes /healthz, which asks only whether the process is
serving. --ready probes /readyz instead, which also checks the database.
Liveness is the right default for a container: a database blip should not
make an orchestrator destroy an otherwise healthy instance.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			target := url

			if target == "" {
				res, err := loadConfig(cmd, env, flags)
				if err != nil {
					return err
				}

				target = localProbeURL(res.Config.Server.Host, res.Config.Server.Port, ready)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()

			if err := probe(ctx, target); err != nil {
				return err
			}

			_, err := fmt.Fprintf(env.Stdout, "healthy: %s\n", target)

			return err
		},
	}

	cmd.Flags().StringVar(&url, "url", "",
		"Probe this URL instead of the configured address")
	cmd.Flags().DurationVar(&timeout, "timeout", 3*time.Second,
		"Give up after this long")
	cmd.Flags().BoolVar(&ready, "ready", false,
		"Probe readiness (/readyz, includes the database) instead of liveness")

	return cmd
}

// localProbeURL builds the address to probe from the server's own settings.
//
// An empty or wildcard host means "bind every interface", which is not an
// address anything can connect to. Loopback is what a probe running beside the
// server should use, and inside a container it is the only thing that works.
func localProbeURL(host string, port int, ready bool) string {
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}

	path := "/healthz"
	if ready {
		path = "/readyz"
	}

	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + path
}

// probe performs the request and reports why it failed, if it did.
func probe(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}

	// A fresh client with no keep-alives: this process exits immediately, so a
	// pooled connection would only be a file descriptor nobody closes.
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("healthcheck: %s is not answering: %w", url, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: %s returned %d", url, resp.StatusCode)
	}

	return nil
}
