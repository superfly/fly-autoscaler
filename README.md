Fly Autoscaler
==============

A metrics-based autoscaler for Fly.io. The autoscaler supports polling for
metrics from a Prometheus instance and then computing the number of machines
based on those metrics.

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

### Create a deploy token

First, set up a new deploy token for your application so that the autoscaler
and fetch metrics and start machines with it:

```sh
$ fly tokens create deploy -a MYAPP -n "fly-autoscaler"
```

You can use this token by exporting it to an environment variable:

```
$ export FLY_ACCESS_TOKEN="FlyV1 ..."
```

### Running the server

To run the autoscaler as a server process, use the `serve` command. For example,
this will ensure that there are always 5 instances running at a time:

```sh
$ fly-autoscaler serve \
  -org MYORG \
  -app MYAPP \
  -prometheus.address "https://api.fly.io/prometheus/MYORG"
  -prometheus.query "sum(fly_instance_up{app='MYAPP'})" \
  -prometheus.metric-name instance_up
  -expr "max(instance_up, 5)"
```

### Testing your metrics & expression

You can perform a one-time run of metrics collection & expression evaluation for
testing or debugging purposes by using the `eval` command. This command does not
perform any scaling of Fly Machines. It will only print the evaluated expression
based on current metrics numbers.

```sh
$ fly-autoscaler eval \
  -org MYORG \
  -app MYAPP \
  -prometheus.address "https://api.fly.io/prometheus/MYORG"
  -prometheus.query "sum(fly_instance_up{app='MYAPP'})" \
  -prometheus.metric-name instance_up
  -expr "max(instance_up, 5)"
```
