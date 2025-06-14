# SLO Operator

The SLO Operator is a Kubernetes Operator that can be used to manage Service
Level Objectives for services. The SLO Operator allows users to define
`ServiceLevelObjectives` CustomResources, to generate all Prometheus rules and
alerts required for an SLO.

## Installation

The operator can be installed via the Helm chart present in the `charts`
directory. The chart can be installed with the following command:

```sh
helm upgrade --install slo-operator oci://ghcr.io/ricoberger/charts/slo-operator --version <VERSION>
```

## Configuration

By default the operator creates a `PrometheusRule` for the
[Prometheus Operator](https://prometheus-operator.dev/). If you are using the
[VictoriaMetrics Operator](https://docs.victoriametrics.com/operator/) a
`VMRule` can be created by setting the `SLO_OPERATOR_MODE` environment variable
to `VictoriaMetrics`.

## API Specification

```yaml
apiVersion: ricoberger.de/v1alpha1
kind: ServiceLevelObjective
metadata:
  name:
  namespace:
  labels:
    # Labels with a "slo-operator.ricoberger.de/" prefix are added to the
    # generated Prometheus recording rules and alerts.
    slo-operator.ricoberger.de/team: myteam
spec:
  # A list of SLOs for the service.
  slos:
    - # The name of the SLO, e.g. "errors", "latency", etc.
      name:
      # The objective for the SLO, e.g. "99.9% uptime", "95% requests in 200ms",
      # etc. It must be a percentage value between 0 and 100 as string, e.g.
      # "99.9".
      objective:
      # A description for the SLO.
      description:
      # SLI contains the metrics to calculate the SLO. For example the total
      # metric is the number of all requests, while the error metric is only the
      # number of all 5xx requests.
      #
      # The total and error metric must contain a "${window}" placeholder, which
      # will be replaced by the operator with the actual required window for the
      # SLO (always 28 days) and the windows for the different burn rates.
      sli:
        totalQuery:
        errorQuery:
      alerting:
        # Disabled can be used to disable the alerting. If the field is set to
        # "true" the operator will not generate alerting rules for Prometheus.
        disabled:
        # Severities is a list of severities for the alerting rules created by
        # the operator for the absent alert and the burn rate alerts. The list
        # must contain 5 entries. The first one is used for the absent alert and
        # the remaining 4 for the burn rate alerts ordered by criticality.
        #
        # The default list which is used, when the field is not set is
        # ["critial", "error", "error", "warning", "warning"]
        severities:
```

## Example

```yaml
apiVersion: ricoberger.de/v1alpha1
kind: ServiceLevelObjective
metadata:
  name: grafana
  namespace: monitoring
  labels:
    slo-operator.ricoberger.de/team: myteam
spec:
  slos:
    - name: availability
      objective: "99.9"
      sli:
        totalQuery: sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[${window}]))
        errorQuery: sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[${window}]))
    - name: latency
      objective: "99.9"
      sli:
        totalQuery: sum(rate(istio_request_duration_milliseconds_count{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[${window}]))
        errorQuery: |
          (
            sum(rate(istio_request_duration_milliseconds_count{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[${window}]))
            -
            sum(rate(istio_request_duration_milliseconds_bucket{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",le="2500"}[${window}]))
          )
    - name: up
      objective: "99.9"
      sli:
        totalQuery: sum(count_over_time(up{job="grafana"}[${window}]))
        errorQuery: |
          (
            sum(count_over_time(up{job="grafana"}[${window}]))
            -
            sum(sum_over_time(up{job="grafana"}[${window}]))
          )
```

## Development

After modifying the `*_types.go` files in the `api/v1alpha1` folder always run
the following command to update the generated code for that resource type:

```sh
make generate
```

The above Makefile target will invoke the
[controller-gen](https://sigs.k8s.io/controller-tools) utility to update the
`api/v1alpha1/zz_generated.deepcopy.go` file to ensure our API's Go type
definitons implement the `runtime.Object` interface that all Kind types must
implement.

Once the API is defined with spec/status fields and CRD validation markers, the
CRD manifests can be generated and updated with the following command:

```sh
make manifests
```

This Makefile target will invoke controller-gen to generate the CRD manifest at
`charts/slo-operator/crds/ricoberger.de_servicelevelobjectives.yaml`.

Deploy the CRD and run the operator locally with the default Kubernetes config
file present at `$HOME/.kube/config`:

```sh
kubectl apply -f charts/slo-operator/crds/ricoberger.de_servicelevelobjectives.yaml

make run
```

## Acknowledgement

The SLO Opeartor is heavily inspirred by
[Pyrra](https://github.com/pyrra-dev/pyrra) and
[Sloth](https://github.com/slok/sloth). It creates a similar set of recording
and alerting rules like Pyrra does, but uses a pattern more similar to Sloth to
define the Service Level Objectives.
