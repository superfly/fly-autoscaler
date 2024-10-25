# Fly Autoscaler

The project is a metrics-based autoscaler for Fly.io. The autoscaler supports
polling for metrics from a Prometheus instance and then computing the number of
machines based on those metrics.

## How it works

The Fly Autoscaler works by performing a reconciliation loop on a regular
interval. By default, it runs every 15 seconds.

1. Collect metrics from external systems (e.g. Prometheus)

2. Compute the target number of machines based on a user-provided expression.

3. Fetch a list of all Fly Machines for your application.

4. If the target number of machines is less than the number of `started`
   machines, use the Fly Machines API to start new machines.

```
                                     ┌────────────────────┐
fly-autoscaler ──────────┐           │                    │
│ ┌────────────────────┐ │    ┌──────│     Prometheus     │
│ │                    │ │    │      │                    │
│ │  Metric Collector  │◀┼────┘      └────────────────────┘
│ │                    │ │
│ └──────┬─────────────┘ │
│        │     △         │
│        ▽     │         │
│ ┌────────────┴───────┐ │
│ │                    │ │
│ │     Reconciler     │◀┼────┐
│ │                    │ │    │      ┌────────────────────┐
│ └────────────────────┘ │    │      │                    │
└────────────────────────┘    └─────▶│  Fly Machines API  │
                                     │                    │
                                     └────────────────────┘
```

### Expressions

The autoscaler uses the [Expr][] language to define the target number of
machines. See the [Expr Language Definition][] for syntax and a full list of
built-in functions. The expression can utilize any named metrics that you
collect and it should always return a number.

For example, if you poll for queue depth and each machine can handle 10 queue
items at a time, you can compute the number of machines as:

```expr
ceil(queue_depth / 10)
```

The autoscaler can only start machines so it will never exceed the number of
machines available for a Fly app.

[Expr]: https://expr-lang.org/
[Expr Language Definition]: https://expr-lang.org/docs/language-definition

## Usage

### Create an app for your autoscaler

First, create an app for your autoscaler:

```sh
$ fly apps create my-autoscaler
```

Then create a `fly.toml` for the deployment. Update the `TARGET_APP_NAME` with
the name of the app that you want to scale and update `MY_ORG` to the
organization where your Prometheus metrics live.

```toml
app = "my-autoscaler"

[build]
image = "flyio/fly-autoscaler:0.2"

[env]
FAS_APP_NAME = "TARGET_APP_NAME"
FAS_STARTED_MACHINE_COUNT = "ceil(queue_depth / 10)"
FAS_PROMETHEUS_ADDRESS = "https://api.fly.io/prometheus/MY_ORG"
FAS_PROMETHEUS_METRIC_NAME = "queue_depth"
FAS_PROMETHEUS_QUERY = "sum(queue_depth)"

[metrics]
port = 9090
path = "/metrics"
```

### Create a deploy token

Next, set up a new deploy token for the application you want to scale:

```sh
$ fly tokens create deploy -a TARGET_APP_NAME
```

Set the token as a secret on your application:

```
$ fly secrets set FAS_API_TOKEN="FlyV1 ..."
```

### Create a read-only token

Create a token for reading your Prometheus data:

```sh
$ fly tokens create readonly
```

Set the token as a secret on your application:

```
$ fly secrets set FAS_PROMETHEUS_TOKEN="FlyV1 ..."
```

### Deploy the server

Finally, deploy your autoscaler application:

```sh
$ fly deploy
```

This should create a new machine and start it with the `fly-autoscaler` server
running.

### Testing your metrics & expression

You can perform a one-time run of metrics collection & expression evaluation for
testing or debugging purposes by using the `eval` command. This command does not
perform any scaling of Fly Machines. It will only print the evaluated expression
based on current metrics numbers.

```sh
$ fly-autoscaler eval
```

You can change the evaluated expression by setting an environment variable:

```sh
$ FAS_STARTED_MACHINE_COUNT=queue_depth fly-autoscaler eval
```

## Configuration

You can also configure `fly-autoscaler` with a YAML config file if you don't
want to use environment variables or if you want to configure more than one
metric collector.

Please see the reference [fly-autoscaler.yml][] for more details.

[fly-autoscaler.yml]: ./etc/fly-autoscaler.yml
