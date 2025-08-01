package controller

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	ricobergerdev1alpha1 "github.com/ricoberger/slo-operator/api/v1alpha1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var sloOperatorMode = strings.ToLower(os.Getenv("SLO_OPERATOR_MODE"))

// ServiceLevelObjectiveReconciler reconciles a ServiceLevelObjective object
type ServiceLevelObjectiveReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ricoberger.de,resources=servicelevelobjectives,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ricoberger.de,resources=servicelevelobjectives/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ricoberger.de,resources=servicelevelobjectives/finalizers,verbs=update
// +kubebuilder:rbac:groups=operator.victoriametrics.com,resources=vmrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=prometheusrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.4/pkg/reconcile
func (r *ServiceLevelObjectiveReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	reqLogger := log.FromContext(ctx)
	reqLogger.Info("Reconcile ServiceLevelObjective.")

	serviceLevelObjective := &ricobergerdev1alpha1.ServiceLevelObjective{}
	err := r.Get(ctx, req.NamespacedName, serviceLevelObjective)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile
			// request. Owned objects are automatically garbage collected. For
			// additional cleanup logic use finalizers. Return and don't
			// requeue.
			reqLogger.Info("ServiceLevelObjective resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		reqLogger.Error(err, "Failed to get ServiceLevelObjective.")
		return ctrl.Result{}, err
	}

	// Define the labels, which should be added to the generated Prometheus
	// rules. A user can define custom labels for the metrics via the
	// "slo-operator.ricoberger.de/<NAME>: <VALUE>" labels.
	labels := make(map[string]string)
	if serviceLevelObjective.Labels != nil {
		for k, v := range serviceLevelObjective.Labels {
			if strings.HasPrefix(k, "slo-operator.ricoberger.de/") {
				labels[strings.TrimPrefix(k, "slo-operator.ricoberger.de/")] = v
			}
		}
	}
	labels["name"] = serviceLevelObjective.Name
	labels["namespace"] = serviceLevelObjective.Namespace

	// If the CR doesn't contain a list of SLOs, we can return at this point. We
	// do not return a error, because it would trigger a reconciliation, which
	// is useless in this case. But we are passing an error to the
	// updateConditions function, so that this mistake is reflected in the CR.
	if len(serviceLevelObjective.Spec.SLOs) == 0 {
		reqLogger.Info("No SLOs defined, skip reconciliation.")
		r.updateConditions(ctx, serviceLevelObjective, fmt.Errorf("no slos defined, skip reconciliation"))
		return ctrl.Result{}, nil
	}

	// For each of the specified SLO we generate two Prometheus rule groups. One
	// contains the generic metrics and the other one the burn rates and
	// corresponding alerts.
	var groups []monitoringv1.RuleGroup

	for _, slo := range serviceLevelObjective.Spec.SLOs {
		sloGroups, err := generatePrometheusRuleGroup(slo, labels)
		if err != nil {
			reqLogger.Error(err, "Failed to generate PrometheusRuleGroup for SLO.", "slo", slo.Name)
			r.updateConditions(ctx, serviceLevelObjective, err)
			return ctrl.Result{}, err
		}
		groups = append(groups, sloGroups...)
	}

	// The operator can work in different modes. The mode can be set via the \
	// "SLO_OPERATOR_MODE" environment variable and defines the resource which
	// should be created by the operator.
	//
	// By default we create a "PrometheusRule" for the Prometheus Operator, but
	// the operator can also create a "VMRule" for the VictoriaMetrics Operator,
	// by converting the PrometheusRule, when the mode is set to
	// "victoriametrics".
	if sloOperatorMode == "victoriametrics" {
		err = r.reconcileVMRule(ctx, serviceLevelObjective, groups)
		if err != nil {
			reqLogger.Error(err, "Failed to reconcile VMRule.")
			r.updateConditions(ctx, serviceLevelObjective, err)
			return ctrl.Result{}, err
		}
	} else {
		err = r.reconcilePrometheusRule(ctx, serviceLevelObjective, groups)
		if err != nil {
			reqLogger.Error(err, "Failed to reconcile PrometheusRule.")
			r.updateConditions(ctx, serviceLevelObjective, err)
			return ctrl.Result{}, err
		}
	}

	r.updateConditions(ctx, serviceLevelObjective, nil)
	return ctrl.Result{}, nil
}

// reconcilePrometheusRule creates / updates the PrometheusRule for a
// ServiceLevelObjective resource. If we found an existing PrometheusRule we
// will update it. If we are not able to find a PrometheusRule for the
// ServiceLevelObjective, we will create a new one.
func (r *ServiceLevelObjectiveReconciler) reconcilePrometheusRule(ctx context.Context, slo *ricobergerdev1alpha1.ServiceLevelObjective, groups []monitoringv1.RuleGroup) error {
	reqLogger := log.FromContext(ctx)

	prometheusRule := &monitoringv1.PrometheusRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      slo.Name,
			Namespace: slo.Namespace,
		},
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: groups,
		},
	}

	err := ctrl.SetControllerReference(slo, prometheusRule, r.Scheme)
	if err != nil {
		return err
	}

	found := &monitoringv1.PrometheusRule{}
	err = r.Get(ctx, types.NamespacedName{Name: slo.Name, Namespace: slo.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new PrometheusRule.")
		err = r.Create(ctx, prometheusRule)
		if err != nil {
			reqLogger.Error(err, "Failed to create PrometheusRule.")
			return err
		}

		return nil
	} else if err != nil {
		return err
	}

	reqLogger.Info("Updating an existing PrometheusRule.")
	prometheusRule.ResourceVersion = found.ResourceVersion

	err = r.Update(ctx, prometheusRule)
	if err != nil {
		reqLogger.Error(err, "Failed to update PrometheusRule.")
		return err
	}

	return nil
}

// reconcileVMRule creates / updates the VMRule for a ServiceLevelObjective
// resource.
func (r *ServiceLevelObjectiveReconciler) reconcileVMRule(ctx context.Context, slo *ricobergerdev1alpha1.ServiceLevelObjective, groups []monitoringv1.RuleGroup) error {
	reqLogger := log.FromContext(ctx)

	// Since the operator creates a PrometheusRule by default, we have to
	// convert the groups to VictoriaMetrics rule groups first. The convert
	// logic is heavily inspired by the logic used by the VictoriaMetrics
	// Operator.
	//
	// See https://github.com/VictoriaMetrics/operator/blob/a6729aa4a430b4bc5d1d061e8e9ce3af3f884120/internal/controller/operator/converter/apis.go#L24
	vmGroups := make([]vmv1beta1.RuleGroup, 0, len(groups))

	for _, group := range groups {
		vmRules := make([]vmv1beta1.Rule, 0, len(group.Rules))
		for _, rule := range group.Rules {
			trule := vmv1beta1.Rule{
				Labels:      rule.Labels,
				Annotations: rule.Annotations,
				Expr:        rule.Expr.String(),
				Record:      rule.Record,
				Alert:       rule.Alert,
			}

			if rule.For != nil {
				trule.For = string(*rule.For)
			}

			vmRules = append(vmRules, trule)
		}

		tgroup := vmv1beta1.RuleGroup{
			Name:  group.Name,
			Rules: vmRules,
		}

		if group.Interval != nil {
			tgroup.Interval = string(*group.Interval)
		}

		vmGroups = append(vmGroups, tgroup)
	}

	// At this point we can use the convert groups and create / update a VMRule.
	// If no VMRule exists we will create a new one. If we found an existing
	// VMRule we will update it. This is the same logic as used for the
	// PrometheusRule in the reconcilePrometheusRule function.
	vmRule := &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      slo.Name,
			Namespace: slo.Namespace,
		},
		Spec: vmv1beta1.VMRuleSpec{
			Groups: vmGroups,
		},
	}

	err := ctrl.SetControllerReference(slo, vmRule, r.Scheme)
	if err != nil {
		return err
	}

	found := &vmv1beta1.VMRule{}
	err = r.Get(ctx, types.NamespacedName{Name: slo.Name, Namespace: slo.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating a new VMRule.")
		err = r.Create(ctx, vmRule)
		if err != nil {
			reqLogger.Error(err, "Failed to create VMRule.")
			return err
		}

		return nil
	} else if err != nil {
		return err
	}

	reqLogger.Info("Updating an existing VMRule.")
	vmRule.ResourceVersion = found.ResourceVersion

	err = r.Update(ctx, vmRule)
	if err != nil {
		reqLogger.Error(err, "Failed to update VMRule.")
		return err
	}

	return nil
}

// generatePrometheusRuleGroup generates the Prometheus rule group for a SLO in
// the ServiceLevelObjective resource.
//
// Each Prometheus rule group for a SLO concsists of multiple Prometheus rules:
//   - "slo:windows": We always use a window of 28 days for the SLOs, because it
//     always captures the same number of weekends, no matter what day of the
//     week it is. This accounts better for traffic variation over weekends than
//     a 30 day SLO.
//   - "slo:objective": The user configured target objective of the SLO.
//   - "slo:total": A recording rule of the configured total metric. This is
//     only used for the Grafana dashboard.
//   - "slo:errors_total: A recording rule for the configured error metric. This
//     is only used for the Grafana dashboard.
//   - "slo:availability: The actual value for the SLO, calculated via the
//     provided total and error metric. This metric can also be used to
//     calculated the error budget via
//     "((slo:availability - slo:objective)) / (1 - slo:objective)"
//   - "slo:burnrate": The current burn rate for the SLO. This metric is
//     available for multiple windows. The window is specified in the "window"
//     label of the metric.
//   - "SLOMetricAbsent": An alerting rule which fires when the user specified
//     total metric is absent.
//   - "SLOErrorBudgetBurn": Multiple alerting rules which are fired when the
//     error budget is burning to fast / to statically over the SLO window, see
//     https://sre.google/workbook/alerting-on-slos/.
func generatePrometheusRuleGroup(slo ricobergerdev1alpha1.SLO, labels map[string]string) ([]monitoringv1.RuleGroup, error) {
	// Validate the SLO specified by the user via the ServiceLevelObjective
	// resource. Each SLO must contain a name, objective, total query and error
	// query. The total and error query must also contain a "${window}"
	// placeholder, which is replaced by the operator to generate the metrics
	// mentioned above.
	if slo.Name == "" || slo.Objective == "" || slo.SLI.TotalQuery == "" || slo.SLI.ErrorQuery == "" {
		return nil, fmt.Errorf("required field name, objective, total query or error query is missing")
	}

	if !strings.Contains(slo.SLI.TotalQuery, "${window}") || !strings.Contains(slo.SLI.ErrorQuery, "${window}") {
		return nil, fmt.Errorf("SLI queries must contain the ${window} placeholder")
	}

	// Generate a unique id for each SLO. so that the resulting metrics are
	// always having a unique label set. The id and name of the SLO are then
	// added to the labels.
	id := fmt.Sprintf("%s-%s-%s", labels["name"], labels["namespace"], slo.Name)

	sloLabels := make(map[string]string)
	for k, v := range labels {
		sloLabels[k] = v
	}
	sloLabels["id"] = id
	sloLabels["slo"] = slo.Name

	// Since the objective must be specified as string in the
	// ServiceLevelObjective resource, we have to parse the value here. We also
	// divided it by 100 so that it is always in the range of 0 and 1, which
	// makes the following calculations easier.
	objective, err := strconv.ParseFloat(slo.Objective, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SLO objective: %w", err)
	}
	objective = objective / 100.0

	// Generate the generic and errors Prometheus rules. The errors group
	// contains the burn rate metrics and alerts. All other metrics and alerts
	// are added to the generic group.
	genericRules := []monitoringv1.Rule{
		{
			Record: "slo:window",
			Expr:   intstr.FromInt(2419200),
			Labels: sloLabels,
		},
		{
			Record: "slo:objective",
			Expr:   intstr.FromString(strconv.FormatFloat(objective, 'f', -1, 64)),
			Labels: sloLabels,
		},
		{
			Record: "slo:total",
			Expr:   intstr.FromString(strings.ReplaceAll(slo.SLI.TotalQuery, "${window}", "2m")),
			Labels: sloLabels,
		},
		{
			Record: "slo:errors_total",
			Expr:   intstr.FromString(strings.ReplaceAll(fmt.Sprintf("(%s) or vector(0)", slo.SLI.ErrorQuery), "${window}", "2m")),
			Labels: sloLabels,
		},
		{
			Record: "slo:availability",
			Expr:   intstr.FromString(strings.ReplaceAll(fmt.Sprintf(`1 - ((%s) or vector(0)) / (%s)`, slo.SLI.ErrorQuery, slo.SLI.TotalQuery), "${window}", "28d")),
			Labels: sloLabels,
		},
	}

	errorsRules := []monitoringv1.Rule{
		generatePrometheusRuleBurnRateRecording(slo.SLI, sloLabels, "5m"),
		generatePrometheusRuleBurnRateRecording(slo.SLI, sloLabels, "30m"),
		generatePrometheusRuleBurnRateRecording(slo.SLI, sloLabels, "1h"),
		generatePrometheusRuleBurnRateRecording(slo.SLI, sloLabels, "2h"),
		generatePrometheusRuleBurnRateRecording(slo.SLI, sloLabels, "6h"),
		generatePrometheusRuleBurnRateRecording(slo.SLI, sloLabels, "1d"),
		generatePrometheusRuleBurnRateRecording(slo.SLI, sloLabels, "4d"),
	}

	// If the alerting isn't disabled by the user, we add the alerting rules
	// to the total and errors group in the following. We also check if the user
	// provided a list of severieties for the alerts. If not, we use a default
	// list of severities.
	if !slo.Alerting.Disabled {
		severities := []string{"critical", "error", "error", "warning", "warning"}
		if len(slo.Alerting.Severities) == 5 {
			severities = slo.Alerting.Severities
		}

		genericRules = append(genericRules, []monitoringv1.Rule{
			generatePrometheusRuleAbsentAlerting(slo.SLI.TotalQuery, sloLabels, severities[0]),
		}...)

		errorsRules = append(errorsRules, []monitoringv1.Rule{
			generatePrometheusRuleBurnRateAlerting(id, sloLabels, "5m", "1h", "14", objective, "2m", severities[1]),
			generatePrometheusRuleBurnRateAlerting(id, sloLabels, "30m", "6h", "7", objective, "15m", severities[2]),
			generatePrometheusRuleBurnRateAlerting(id, sloLabels, "2h", "1d", "2", objective, "1h", severities[3]),
			generatePrometheusRuleBurnRateAlerting(id, sloLabels, "6h", "4d", "1", objective, "3h", severities[4]),
		}...)
	}

	return []monitoringv1.RuleGroup{
		{
			Name:     fmt.Sprintf("slo-generic-%s", id),
			Interval: monitoringv1.DurationPointer("30s"),
			Rules:    genericRules,
		},
		{
			Name:     fmt.Sprintf("slo-errors-%s", id),
			Interval: monitoringv1.DurationPointer("30s"),
			Rules:    errorsRules,
		},
	}, nil
}

// generatePrometheusRuleAbsentAlerting generates a sinlge Prometheus alert
// rule, which is used to alert with the provided severity, when the provided
// metric is absent.
func generatePrometheusRuleAbsentAlerting(query string, labels map[string]string, severity string) monitoringv1.Rule {
	alertLabels := make(map[string]string)
	for k, v := range labels {
		alertLabels[k] = v
	}
	alertLabels["severity"] = severity

	return monitoringv1.Rule{
		Alert:  "SLOMetricAbsent",
		Expr:   intstr.FromString(strings.ReplaceAll(fmt.Sprintf("absent(%s) == 1", query), "${window}", "2m")),
		For:    monitoringv1.DurationPointer("10m"),
		Labels: alertLabels,
	}
}

// generatePrometheusRuleBurnRateRecording generates a single Prometheus
// recording rule, which is used as burn rate for the specified window.
//
// The recording rule is named "slo:burnrate" and contains the specified window
// as label. The burn rate is calculated by dividing the error metric by the
// total metric and replacing the "${window}" placeholder within the metric.
func generatePrometheusRuleBurnRateRecording(sli ricobergerdev1alpha1.SLI, labels map[string]string, window string) monitoringv1.Rule {
	recordLabels := make(map[string]string)
	for k, v := range labels {
		recordLabels[k] = v
	}
	recordLabels["window"] = window

	return monitoringv1.Rule{
		Record: "slo:burnrate",
		Expr:   intstr.FromString(strings.ReplaceAll(fmt.Sprintf("(%s) / (%s)", sli.ErrorQuery, sli.TotalQuery), "${window}", window)),
		Labels: recordLabels,
	}
}

// generatePrometheusRuleBurnRateAlerting generates a single Prometheus alert
// rule for the specified burn rates.
//
// This function generates an alert that fires when burn rates for two different
// time windows both exceed. The alert is named "SLOErrorBudgetBurn".
func generatePrometheusRuleBurnRateAlerting(id string, labels map[string]string, burnrate1 string, burnrate2 string, factor string, objective float64, forDuration string, severity string) monitoringv1.Rule {
	alertLabels := make(map[string]string)
	for k, v := range labels {
		alertLabels[k] = v
	}
	alertLabels["severity"] = severity

	return monitoringv1.Rule{
		Alert:  "SLOErrorBudgetBurn",
		Expr:   intstr.FromString(fmt.Sprintf(`slo:burnrate{window="%s", id="%s"} > (%s * (1-%s)) and slo:burnrate{window="%s", id="%s"} > (%s * (1-%s))`, burnrate1, id, factor, strconv.FormatFloat(objective, 'f', -1, 64), burnrate2, id, factor, strconv.FormatFloat(objective, 'f', -1, 64))),
		For:    monitoringv1.DurationPointer(forDuration),
		Labels: alertLabels,
	}
}

// updateConditions updates the conditions of the ServiceLevelObjective
// resource. If the "reconcileError" is not nil, the condition will be set to
// "Failed" with the error as message. Otherwise the condition will be set to
// "Succeeded".
func (r *ServiceLevelObjectiveReconciler) updateConditions(ctx context.Context, slo *ricobergerdev1alpha1.ServiceLevelObjective, reconcileError error) {
	reqLogger := log.FromContext(ctx)

	reason := "Succeeded"
	if reconcileError != nil {
		reason = "Failed"
	}

	message := "Reconciliation succeeded"
	if reconcileError != nil {
		message = fmt.Sprintf("Reconciliation failed: %s", reconcileError.Error())
	}

	slo.Status.Conditions = []metav1.Condition{{
		Type:               "ServiceLevelObjectiveReconciled",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: slo.GetGeneration(),
		LastTransitionTime: metav1.NewTime(time.Now()),
		Reason:             reason,
		Message:            message,
	}}

	err := r.Status().Update(ctx, slo)
	if err != nil {
		reqLogger.Error(err, "Failed to update status.")
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceLevelObjectiveReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ricobergerdev1alpha1.ServiceLevelObjective{}).
		WithEventFilter(ignorePredicate()).
		Named("servicelevelobjective").
		Complete(r)
}

// ignorePredicate is used to ignore updates to CR status in which case
// metadata.Generation does not change.
func ignorePredicate() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
	}
}
