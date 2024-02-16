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

	serve        runs the autoscaler server process
	version      prints the version
`[1:])
}
