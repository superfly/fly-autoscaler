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
	"slices"
	"strconv"
	"strings"
	"time"

	fas "github.com/superfly/fly-autoscaler"
	fasprom "github.com/superfly/fly-autoscaler/prometheus"
	"github.com/superfly/fly-autoscaler/temporal"
	fly "github.com/superfly/fly-go"
	"github.com/superfly/fly-go/flaps"
	"github.com/superfly/fly-go/tokens"
	"gopkg.in/yaml.v3"
)

// Build information.
var (
	Version = ""
	Commit  = ""
)

const (
	APIBaseURL = "https://api.fly.io"
)

func main() {
	fly.SetBaseURL(APIBaseURL)

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
		return fmt.Errorf("fly-autoscaler %s: unknown command", cmd)
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
	AppName                string        `yaml:"app-name"`
	Org                    string        `yaml:"org"`
	Regions                []string      `yaml:"regions"`
	ProcessGroup           string        `yaml:"process-group"`
	CreatedMachineN        string        `yaml:"created-machine-count"`
	MinCreatedMachineN     string        `yaml:"min-created-machine-count"`
	MaxCreatedMachineN     string        `yaml:"max-created-machine-count"`
	InitialMachineState    string        `yaml:"initial-machine-state"`
	StartedMachineN        string        `yaml:"started-machine-count"`
	MinStartedMachineN     string        `yaml:"min-started-machine-count"`
	MaxStartedMachineN     string        `yaml:"max-started-machine-count"`
	Concurrency            int           `yaml:"concurrency"`
	Interval               time.Duration `yaml:"interval"`
	Timeout                time.Duration `yaml:"timeout"`
	AppListRefreshInterval time.Duration `yaml:"app-list-refresh-interval"`
	APIToken               string        `yaml:"api-token"`
	Verbose                bool          `yaml:"verbose"`

	MetricCollectors []*MetricCollectorConfig `yaml:"metric-collectors"`
}

func NewConfig() *Config {
	return &Config{
		Concurrency:            fas.DefaultConcurrency,
		Interval:               fas.DefaultReconcileInterval,
		Timeout:                fas.DefaultReconcileTimeout,
		AppListRefreshInterval: fas.DefaultAppListRefreshInterval,
		ProcessGroup:           fas.DefaultProcessGroup,
	}
}

func NewConfigFromEnv() (_ *Config, err error) {
	c := NewConfig()
	c.AppName = os.Getenv("FAS_APP_NAME")
	c.Org = os.Getenv("FAS_ORG")
	c.CreatedMachineN = os.Getenv("FAS_CREATED_MACHINE_COUNT")
	c.MinCreatedMachineN = os.Getenv("FAS_MIN_CREATED_MACHINE_COUNT")
	c.MaxCreatedMachineN = os.Getenv("FAS_MAX_CREATED_MACHINE_COUNT")
	c.InitialMachineState = os.Getenv("FAS_INITIAL_MACHINE_STATE")
	c.StartedMachineN = os.Getenv("FAS_STARTED_MACHINE_COUNT")
	c.MinStartedMachineN = os.Getenv("FAS_MIN_STARTED_MACHINE_COUNT")
	c.MaxStartedMachineN = os.Getenv("FAS_MAX_STARTED_MACHINE_COUNT")
	c.APIToken = os.Getenv("FAS_API_TOKEN")

	if s := os.Getenv("FAS_PROCESS_GROUP"); s != "" {
		c.ProcessGroup = s
	}

	if s := os.Getenv("FAS_REGIONS"); s != "" {
		c.Regions = strings.Split(s, ",")
	}

	if c.InitialMachineState == "" {
		c.InitialMachineState = fly.MachineStateStarted
	}

	if s := os.Getenv("FAS_CONCURRENCY"); s != "" {
		if c.Concurrency, err = strconv.Atoi(s); err != nil {
			return nil, fmt.Errorf("cannot parse FAS_CONCURRENCY as integer: %q", s)
		}
	}

	if s := os.Getenv("FAS_INTERVAL"); s != "" {
		if c.Interval, err = time.ParseDuration(s); err != nil {
			return nil, fmt.Errorf("cannot parse FAS_INTERVAL as duration: %q", s)
		}
	}
	if s := os.Getenv("FAS_TIMEOUT"); s != "" {
		if c.Timeout, err = time.ParseDuration(s); err != nil {
			return nil, fmt.Errorf("cannot parse FAS_TIMEOUT as duration: %q", s)
		}
	}
	if s := os.Getenv("FAS_APP_LIST_REFRESH_INTERVAL"); s != "" {
		if c.AppListRefreshInterval, err = time.ParseDuration(s); err != nil {
			return nil, fmt.Errorf("cannot parse FAS_APP_LIST_REFRESH_INTERVAL as duration: %q", s)
		}
	}

	if addr := os.Getenv("FAS_PROMETHEUS_ADDRESS"); addr != "" {
		c.MetricCollectors = append(c.MetricCollectors, &MetricCollectorConfig{
			Type:       "prometheus",
			Address:    addr,
			MetricName: os.Getenv("FAS_PROMETHEUS_METRIC_NAME"),
			Query:      os.Getenv("FAS_PROMETHEUS_QUERY"),
			Token:      os.Getenv("FAS_PROMETHEUS_TOKEN"),
		})
	}

	if addr := os.Getenv("FAS_TEMPORAL_ADDRESS"); addr != "" {
		certData := os.Getenv("TEMPORAL_TLS_CERT_DATA")
		if certData == "" {
			certData = os.Getenv("FAS_TEMPORAL_CERT_DATA")
		}

		keyData := os.Getenv("TEMPORAL_TLS_KEY_DATA")
		if keyData == "" {
			keyData = os.Getenv("FAS_TEMPORAL_KEY_DATA")
		}

		c.MetricCollectors = append(c.MetricCollectors, &MetricCollectorConfig{
			Type:       "temporal",
			Address:    addr,
			Namespace:  os.Getenv("FAS_TEMPORAL_NAMESPACE"),
			MetricName: os.Getenv("FAS_TEMPORAL_METRIC_NAME"),
			CertData:   certData,
			KeyData:    keyData,
			Query:      os.Getenv("FAS_TEMPORAL_QUERY"),
		})
	}

	return c, nil
}

func (c *Config) IsCreatedMachineCountDefined() bool {
	return c.CreatedMachineN != "" || c.MinCreatedMachineN != "" || c.MaxCreatedMachineN != ""
}

func (c *Config) GetMinCreatedMachineN() string {
	if v := c.CreatedMachineN; v != "" {
		return v
	}
	return c.MinCreatedMachineN
}

func (c *Config) GetMaxCreatedMachineN() string {
	if v := c.CreatedMachineN; v != "" {
		return v
	}
	return c.MaxCreatedMachineN
}

func (c *Config) IsStartedMachineCountDefined() bool {
	return c.StartedMachineN != "" || c.MinStartedMachineN != "" || c.MaxStartedMachineN != ""
}

func (c *Config) GetMinStartedMachineN() string {
	if v := c.StartedMachineN; v != "" {
		return v
	}
	return c.MinStartedMachineN
}

func (c *Config) GetMaxStartedMachineN() string {
	if v := c.StartedMachineN; v != "" {
		return v
	}
	return c.MaxStartedMachineN
}

func (c *Config) Validate() error {
	if c.AppName == "" {
		return fmt.Errorf("app name required")
	}

	if !c.IsCreatedMachineCountDefined() && !c.IsStartedMachineCountDefined() {
		return fmt.Errorf("must define either created machine count or started machine count")
	}
	if err := c.validateCreatedMachineCount(); err != nil {
		return err
	}
	if err := c.validateStartedMachineCount(); err != nil {
		return err
	}

	if !slices.Contains([]string{fly.MachineStateStarted, fly.MachineStateStopped}, c.InitialMachineState) {
		return fmt.Errorf("initial machine state must be either 'started' or 'stopped'")
	}

	for i, collectorConfig := range c.MetricCollectors {
		if err := collectorConfig.Validate(); err != nil {
			return fmt.Errorf("metric-collectors[%d]: %w", i, err)
		}
	}
	return nil
}

func (c *Config) validateCreatedMachineCount() error {
	if !c.IsCreatedMachineCountDefined() {
		return nil
	}

	if c.CreatedMachineN != "" && (c.MinCreatedMachineN != "" || c.MaxCreatedMachineN != "") {
		return fmt.Errorf("cannot define created machine count and min/max created machine count")
	}
	if c.MinCreatedMachineN != "" && c.MaxCreatedMachineN == "" {
		return fmt.Errorf("max created machine count required if min created machine count is defined")
	}
	if c.MinCreatedMachineN == "" && c.MaxCreatedMachineN != "" {
		return fmt.Errorf("min created machine count required if max created machine count is defined")
	}
	return nil
}

func (c *Config) validateStartedMachineCount() error {
	if !c.IsStartedMachineCountDefined() {
		return nil
	}

	if c.StartedMachineN != "" && (c.MinStartedMachineN != "" || c.MaxStartedMachineN != "") {
		return fmt.Errorf("cannot define started machine count and min/max started machine count")
	}
	if c.MinStartedMachineN != "" && c.MaxStartedMachineN == "" {
		return fmt.Errorf("max started machine count required if min started machine count is defined")
	}
	if c.MinStartedMachineN == "" && c.MaxStartedMachineN != "" {
		return fmt.Errorf("min started machine count required if max started machine count is defined")
	}
	return nil
}

func (c *Config) NewFlyClient(ctx context.Context) (*fly.Client, error) {
	if c.APIToken == "" {
		return nil, fmt.Errorf("api token required")
	}

	return fly.NewClientFromOptions(fly.ClientOptions{
		Tokens: tokens.Parse(c.APIToken),
	}), nil
}

func (c *Config) NewFlapsClient() (fas.NewFlapsClientFunc, error) {
	if c.APIToken == "" {
		return nil, fmt.Errorf("api token required")
	}
	tok := tokens.Parse(c.APIToken)

	return func(ctx context.Context, appName string) (fas.FlapsClient, error) {
		return flaps.NewWithOptions(ctx, flaps.NewClientOpts{
			AppName: appName,
			Tokens:  tok,
		})
	}, nil
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
	Query      string `yaml:"query"`   // Prometheus & Temporal
	Address    string `yaml:"address"` // Prometheus & Temporal

	// Prometheus fields
	Token string `yaml:"token"`

	// Temporal fields
	Namespace string `yaml:"namespace"`
	CertData  string `yaml:"cert-data"`
	KeyData   string `yaml:"key-data"`
}

func (c *MetricCollectorConfig) Validate() error {
	if c.MetricName == "" {
		return fmt.Errorf("metric name required")
	}

	switch typ := c.Type; typ {
	case "prometheus":
		return c.validatePrometheus()
	case "temporal":
		return c.validateTemporal()
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

func (c *MetricCollectorConfig) validateTemporal() error {
	return nil
}

func (c *MetricCollectorConfig) NewMetricCollector() (fas.MetricCollector, error) {
	switch typ := c.Type; typ {
	case "prometheus":
		return c.newPrometheusMetricCollector()
	case "temporal":
		return c.newTemporalMetricCollector()
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

func (c *MetricCollectorConfig) newTemporalMetricCollector() (*temporal.MetricCollector, error) {
	collector := temporal.NewMetricCollector(c.MetricName)

	collector.Address = c.Address
	collector.Namespace = c.Namespace
	collector.Cert = []byte(c.CertData)
	collector.Key = []byte(c.KeyData)
	collector.Query = c.Query

	if err := collector.Open(); err != nil {
		return nil, err
	}
	return collector, nil
}
