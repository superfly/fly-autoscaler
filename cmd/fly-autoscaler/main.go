package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	fas "github.com/superfly/fly-autoscaler"
	fasprom "github.com/superfly/fly-autoscaler/prometheus"
	"github.com/superfly/fly-go/flaps"
	"gopkg.in/yaml.v3"
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
Fly Machines up. It continuously monitors external metrics to derive the
appropriate number of machines to run to handle the load.

To scale down, your Fly Machines should shut themselves down after some period
of inactivity.

Usage:

	fly-autoscaler <command> [arguments]

The commands are:

	eval         collects metrics once and evaluates server count
	serve        runs the autoscaler server process
	version      prints the version
`[1:])
}

func registerConfigPathFlag(fs *flag.FlagSet) *string {
	return fs.String("config", "", "Path to config file")
}

type Config struct {
	AppName  string        `yaml:"app-name"`
	Expr     string        `yaml:"expr"`
	Interval time.Duration `yaml:"interval"`
	Verbose  bool          `yaml:"verbose"`

	MetricCollectors []*MetricCollectorConfig `yaml:"metric-collectors"`
}

func NewConfig() *Config {
	return &Config{
		Interval: fas.DefaultReconcilerInterval,
	}
}

func NewConfigFromEnv() (*Config, error) {
	c := NewConfig()
	c.AppName = os.Getenv("FAS_APP_NAME")
	c.Expr = os.Getenv("FAS_EXPR")
	if s := os.Getenv("FAS_INTERVAL"); s != "" {
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("cannot parse FAS_INTERVAL as duration: %q", s)
		}
		c.Interval = d
	}

	if addr := os.Getenv("FAS_PROMETHEUS_ADDRESS"); addr != "" {
		c.MetricCollectors = append(c.MetricCollectors, &MetricCollectorConfig{
			Address:    addr,
			MetricName: os.Getenv("FAS_PROMETHEUS_METRIC_NAME"),
			Query:      os.Getenv("FAS_PROMETHEUS_QUERY"),
			Token:      os.Getenv("FAS_PROMETHEUS_TOKEN"),
		})
	}

	return c, nil
}

func (c *Config) Validate() error {
	if c.AppName == "" {
		return fmt.Errorf("app name required")
	}
	if c.Expr == "" {
		return fmt.Errorf("expression required")
	}

	for i, collectorConfig := range c.MetricCollectors {
		if err := collectorConfig.Validate(); err != nil {
			return fmt.Errorf("metric-collectors[%d]: %w", i, err)
		}
	}
	return nil
}

func (c *Config) NewFlapsClient(ctx context.Context) (*flaps.Client, error) {
	return flaps.NewWithOptions(ctx, flaps.NewClientOpts{
		AppName: c.AppName,
	})
}

func (c *Config) NewMetricCollectors() ([]fas.MetricCollector, error) {
	var a []fas.MetricCollector
	for i, collectorConfig := range c.MetricCollectors {
		collector, err := collectorConfig.NewMetricCollector()
		if err != nil {
			return nil, fmt.Errorf("metric collector[%d]: %w", i, err)
		}
		a = append(a, collector)
	}
	return a, nil
}

func ParseConfig(r io.Reader, config *Config) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	data = []byte(os.ExpandEnv(string(data)))

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return dec.Decode(&config)
}

func ParseConfigFromFile(filename string, config *Config) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return ParseConfig(f, config)
}

type MetricCollectorConfig struct {
	Type       string `yaml:"type"`
	MetricName string `yaml:"metric-name"`

	// Prometheus fields
	Address string `yaml:"address"`
	Query   string `yaml:"query"`
	Token   string `yaml:"token"`
}

func (c *MetricCollectorConfig) Validate() error {
	if c.MetricName == "" {
		return fmt.Errorf("metric name required")
	}

	switch typ := c.Type; typ {
	case "prometheus":
		return c.validatePrometheus()
	case "":
		return fmt.Errorf("type required")
	default:
		return fmt.Errorf("invalid type: %q", typ)
	}
}

func (c *MetricCollectorConfig) validatePrometheus() error {
	if c.Address == "" {
		return fmt.Errorf("prometheus address required")
	}
	if c.Query == "" {
		return fmt.Errorf("prometheus query required")
	}
	return nil
}

func (c *MetricCollectorConfig) NewMetricCollector() (fas.MetricCollector, error) {
	switch typ := c.Type; typ {
	case "prometheus":
		return c.newPrometheusMetricCollector()
	default:
		return nil, fmt.Errorf("invalid type: %q", typ)
	}
}

func (c *MetricCollectorConfig) newPrometheusMetricCollector() (*fasprom.MetricCollector, error) {
	return fasprom.NewMetricCollector(
		c.MetricName,
		c.Address,
		c.Query,
		c.Token,
	)
}
