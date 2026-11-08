// Command piworker-mcp lets a stdio MCP client such as Claude Desktop use
// PiWorker's /mcp endpoint (docs/design/mcp.md §12): it relays JSON-RPC
// messages between stdin/stdout and PiWorker over HTTPS, with an agent token
// read from a file and PiWorker's local CA pinned by fingerprint or file.
//
// It is pure Go (CGO_ENABLED=0) so it cross-compiles for macOS, Windows and
// Linux; it must not import PiWorker's storage or anything else needing cgo.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

const usage = `piworker-mcp relays the Model Context Protocol between a stdio MCP client
(such as Claude Desktop) and PiWorker's /mcp endpoint.

Usage:
  piworker-mcp --url https://raspberrypi.local:8443/mcp \
    --token-file ~/.piworker-token --ca-fingerprint SHA256:AB:CD:…

Flags:
  --url URL             PiWorker's MCP endpoint, https://HOST:8443/mcp. Plain
                        http is only accepted to 127.0.0.1 or [::1].
  --token-file PATH     File holding the agent token (pwk_…) created under
                        Agents in the PiWorker editor. On macOS and Linux it
                        must be private to you (chmod 600). A leading ~ is
                        your home directory.
  --ca-fingerprint FP   Trust only the CA with this SHA-256 fingerprint, as
                        shown under Settings → Trust this device.
  --ca-file PATH        Trust only the CA certificate(s) in this PEM or DER
                        file. With --ca-fingerprint too, only the matching one.
  --timeout DURATION    Give up on a request after this long (default 2m0s).
  --version             Print the version and exit.
  --help                Show this help.

Without --ca-fingerprint or --ca-file the system's trusted CAs are used. The
token is never accepted on the command line. Diagnostics go to stderr; stdout
carries only MCP messages.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is the whole command: it returns 0 when stdin ends (or on a signal), 1
// when the relay fails, 2 for invalid flags.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("piworker-mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	rawURL := fs.String("url", "", "")
	tokenFile := fs.String("token-file", "", "")
	caFile := fs.String("ca-file", "", "")
	caFingerprint := fs.String("ca-fingerprint", "", "")
	timeout := fs.String("timeout", defaultRequestTimeout.String(), "")
	showVersion := fs.Bool("version", false, "")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = io.WriteString(stdout, usage)
			return 0
		}
		return usageError(stderr, flagError(err))
	}
	// Checked before anything can echo a value: errors quote paths and URLs.
	var pasted []string
	fs.Visit(func(f *flag.Flag) {
		if looksLikeToken(f.Value.String()) {
			pasted = append(pasted, "--"+f.Name)
		}
	})
	if len(pasted) > 0 {
		return usageError(stderr, "the value of "+strings.Join(pasted, " and ")+" looks like an agent token; put the token in the file given with --token-file, never on the command line")
	}
	if *showVersion {
		_, _ = fmt.Fprintf(stdout, "piworker-mcp %s\n", version)
		return 0
	}
	if fs.NArg() > 0 {
		// Never echoed: a misplaced argument may well be the token.
		return usageError(stderr, "unexpected arguments; the token is read from the file given with --token-file, never from the command line")
	}
	endpoint, err := parseEndpoint(*rawURL)
	if err != nil {
		return usageError(stderr, err.Error())
	}
	token, err := readToken(*tokenFile)
	if err != nil {
		return usageError(stderr, err.Error())
	}
	conn, err := newConnector(endpoint, trustOptions{CAFile: *caFile, CAFingerprint: *caFingerprint})
	if err != nil {
		return usageError(stderr, err.Error())
	}
	requestTimeout, err := time.ParseDuration(*timeout)
	if err != nil || requestTimeout <= 0 {
		return usageError(stderr, "invalid --timeout: expected a positive duration such as 2m or 90s")
	}

	logger := log.New(stderr, "piworker-mcp: ", log.LstdFlags)
	logger.Printf("version %s relaying stdio to %s, trusting %s", version, endpoint, conn.description)
	err = newRelay(relayConfig{
		Endpoint:       endpoint,
		Token:          token,
		Connector:      conn,
		Log:            logger,
		RequestTimeout: requestTimeout,
	}).serve(ctx, stdin, stdout)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, context.Canceled):
		logger.Print("stopped")
		return 0
	default:
		logger.Print(err)
		return 1
	}
}

// flagError describes a flag parsing error without repeating a value that
// looks like a token.
func flagError(err error) string {
	msg := err.Error()
	if strings.Contains(msg, tokenPrefix) {
		msg = "a flag value looks like an agent token"
	}
	if strings.Contains(msg, "-token") || strings.Contains(msg, "agent token") {
		msg += "; the token is read from the file given with --token-file, never from the command line"
	}
	return msg
}

func usageError(stderr io.Writer, msg string) int {
	_, _ = fmt.Fprintf(stderr, "piworker-mcp: %s\nRun piworker-mcp --help for usage.\n", msg)
	return 2
}
