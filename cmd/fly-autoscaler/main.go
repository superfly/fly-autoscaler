package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
)

// Build information.
var (
	Version = ""
	Commit  = ""
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err == flag.ErrHelp {
		os.Exit(2)
	} else if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	var cmd string
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}

	switch cmd {
	case "eval":
		return NewEvalCommand().Run(ctx, args)

	case "serve":
		cmd := NewServeCommand()
		if err := cmd.Run(ctx, args); err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		<-ctx.Done()
		slog.Info("signal received, shutting down")

		return cmd.Close()

	case "version":
		fmt.Println(VersionString())
		return nil

	default:
		if cmd == "" || cmd == "help" || strings.HasPrefix(cmd, "-") {
			printUsage()
			return flag.ErrHelp
		}
		return fmt.Errorf("litefs %s: unknown command", cmd)
	}
}

func VersionString() string {
	if Version != "" {
		return fmt.Sprintf("fly-autoscaler %s, commit=%s", Version, Commit)
	} else if Commit != "" {
		return fmt.Sprintf("fly-autoscaler commit=%s", Commit)
	}
	return "fly-autoscaler development build"
}

func printUsage() {
	fmt.Println(`
fly-autoscaler is a metrics-based autoscaler for automatically scaling your
Fly Machines up or down. It continuously monitors external metrics to derive
the appropriate number of machines to run to handle the load.

Usage:

	fly-autoscaler <command> [arguments]

The commands are:

	eval         collects metrics once and evaluates server count
	serve        runs the autoscaler server process
	version      prints the version
`[1:])
}

func registerOrgNameFlag(fs *flag.FlagSet, appName *string) {
	defaultValue := os.Getenv("FLY_ORG")
	if defaultValue == "" {
		defaultValue = os.Getenv("FLY_ORG_NAME")
	}
	if defaultValue == "" {
		defaultValue = os.Getenv("FAS_ORG_NAME")
	}

	fs.StringVar(appName, "org", defaultValue, "Fly.io organization name.")
}

func registerAppNameFlag(fs *flag.FlagSet, appName *string) {
	defaultValue := os.Getenv("FLY_APP")
	if defaultValue == "" {
		defaultValue = os.Getenv("FLY_APP_NAME")
	}
	if defaultValue == "" {
		defaultValue = os.Getenv("FAS_APP_NAME")
	}

	fs.StringVar(appName, "app", defaultValue, "Fly.io app name.")
}

func registerPrometheusFlags(fs *flag.FlagSet, addr, metricName, query, token *string) {
	defaultToken := os.Getenv("FLY_ACCESS_TOKEN")
	if defaultToken == "" {
		defaultToken = os.Getenv("FAS_PROMETHEUS_TOKEN")
	}

	fs.StringVar(addr, "prometheus.address", os.Getenv("FAS_PROMETHEUS_ADDRESS"), "Prometheus server address.")
	fs.StringVar(metricName, "prometheus.metric-name", os.Getenv("FAS_PROMETHEUS_METRIC_NAME"), "Prometheus metric name.")
	fs.StringVar(query, "prometheus.query", os.Getenv("FAS_PROMETHEUS_QUERY"), "PromQL query.")
	fs.StringVar(token, "prometheus.token", defaultToken, "Prometheus auth token.")
}

func registerExprFlag(fs *flag.FlagSet, expr *string) {
	fs.StringVar(expr, "expr", os.Getenv("FAS_EXPR"), "Expression to calculate target machine count.")
}
