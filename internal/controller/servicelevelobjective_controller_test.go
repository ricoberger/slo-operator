package controller

import (
	"context"

	ricobergerdev1alpha1 "github.com/ricoberger/slo-operator/api/v1alpha1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("ServiceLevelObjective Controller", func() {
	Context("When reconciling a resource", func() {
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      "test",
			Namespace: "default",
		}
		servicelevelobjective := &ricobergerdev1alpha1.ServiceLevelObjective{}

		BeforeEach(func() {
			By("Creating the custom resource for the Kind ServiceLevelObjective")
			err := k8sClient.Get(ctx, typeNamespacedName, servicelevelobjective)
			if err != nil && errors.IsNotFound(err) {
				resource := &ricobergerdev1alpha1.ServiceLevelObjective{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "default",
						Labels: map[string]string{
							"slo-operator.ricoberger.de/team": "myteam",
						},
					},
					Spec: ricobergerdev1alpha1.ServiceLevelObjectiveSpec{
						SLOs: []ricobergerdev1alpha1.SLO{
							{
								Name:      "availability",
								Objective: "90",
								SLI: ricobergerdev1alpha1.SLI{
									TotalQuery: `sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[${window}]))`,
									ErrorQuery: `sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[${window}]))`,
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &ricobergerdev1alpha1.ServiceLevelObjective{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ServiceLevelObjective")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("Should successfully reconcile the resource (PrometheusRule)", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ServiceLevelObjectiveReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Check if PrometheusRule was created")
			prometheusRule := &monitoringv1.PrometheusRule{}
			err = k8sClient.Get(ctx, typeNamespacedName, prometheusRule)
			Expect(err).NotTo(HaveOccurred())
			Expect(prometheusRule.Spec).To(Equal(monitoringv1.PrometheusRuleSpec{
				Groups: []monitoringv1.RuleGroup{
					{
						Name:     "slo-generic-test-default-availability",
						Interval: monitoringv1.DurationPointer("30s"),
						Rules: []monitoringv1.Rule{
							{
								Record: "slo:window",
								Expr:   intstr.FromInt(2419200),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
								},
							},
							{
								Record: "slo:objective",
								Expr:   intstr.FromString("0.9"),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
								},
							},
							{
								Record: "slo:total",
								Expr:   intstr.FromString(`sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[2m]))`),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
								},
							},
							{
								Record: "slo:errors_total",
								Expr:   intstr.FromString(`(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[2m]))) or vector(0)`),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
								},
							},
							{
								Record: "slo:availability",
								Expr:   intstr.FromString(`1 - ((sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[28d]))) or vector(0)) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[28d])))`),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
								},
							},
							{
								Alert: "SLOMetricAbsent",
								Expr:  intstr.FromString(`absent(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[2m]))) == 1`),
								For:   monitoringv1.DurationPointer("10m"),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"severity":  "critical",
								},
							},
						},
					},
					{
						Name:     "slo-errors-test-default-availability",
						Interval: monitoringv1.DurationPointer("30s"),
						Rules: []monitoringv1.Rule{
							{
								Record: "slo:burnrate",
								Expr:   intstr.FromString(`(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[5m]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[5m])))`),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "5m",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   intstr.FromString(`(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[30m]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[30m])))`),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "30m",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   intstr.FromString(`(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[1h]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[1h])))`),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "1h",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   intstr.FromString(`(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[2h]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[2h])))`),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "2h",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   intstr.FromString(`(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[6h]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[6h])))`),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "6h",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   intstr.FromString(`(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[1d]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[1d])))`),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "1d",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   intstr.FromString(`(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[4d]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[4d])))`),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "4d",
								},
							},
							{
								Alert: "SLOErrorBudgetBurn",
								Expr:  intstr.FromString(`slo:burnrate{window="5m", id="test-default-availability"} > (14 * (1-0.9)) and slo:burnrate{window="1h", id="test-default-availability"} > (14 * (1-0.9))`),
								For:   monitoringv1.DurationPointer("2m"),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"severity":  "error",
								},
							},
							{
								Alert: "SLOErrorBudgetBurn",
								Expr:  intstr.FromString(`slo:burnrate{window="30m", id="test-default-availability"} > (7 * (1-0.9)) and slo:burnrate{window="6h", id="test-default-availability"} > (7 * (1-0.9))`),
								For:   monitoringv1.DurationPointer("15m"),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"severity":  "error",
								},
							},
							{
								Alert: "SLOErrorBudgetBurn",
								Expr:  intstr.FromString(`slo:burnrate{window="2h", id="test-default-availability"} > (2 * (1-0.9)) and slo:burnrate{window="1d", id="test-default-availability"} > (2 * (1-0.9))`),
								For:   monitoringv1.DurationPointer("1h"),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"severity":  "warning",
								},
							},
							{
								Alert: "SLOErrorBudgetBurn",
								Expr:  intstr.FromString(`slo:burnrate{window="6h", id="test-default-availability"} > (1 * (1-0.9)) and slo:burnrate{window="4d", id="test-default-availability"} > (1 * (1-0.9))`),
								For:   monitoringv1.DurationPointer("3h"),
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"severity":  "warning",
								},
							},
						},
					},
				},
			}))
		})

		It("Should successfully reconcile the resource (VMRule)", func() {
			sloOperatorMode = "victoriametrics"

			By("Reconciling the created resource")
			controllerReconciler := &ServiceLevelObjectiveReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Check if VMRule was created")
			vmRule := &vmv1beta1.VMRule{}
			err = k8sClient.Get(ctx, typeNamespacedName, vmRule)
			Expect(err).NotTo(HaveOccurred())
			Expect(vmRule.Spec).To(Equal(vmv1beta1.VMRuleSpec{
				Groups: []vmv1beta1.RuleGroup{
					{
						Name:     "slo-generic-test-default-availability",
						Interval: "30s",
						Rules: []vmv1beta1.Rule{
							{
								Record: "slo:window",
								Expr:   "2419200",
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
								},
							},
							{
								Record: "slo:objective",
								Expr:   "0.9",
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
								},
							},
							{
								Record: "slo:total",
								Expr:   `sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[2m]))`,
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
								},
							},
							{
								Record: "slo:errors_total",
								Expr:   `(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[2m]))) or vector(0)`,
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
								},
							},
							{
								Record: "slo:availability",
								Expr:   `1 - ((sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[28d]))) or vector(0)) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[28d])))`,
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
								},
							},
							{
								Alert: "SLOMetricAbsent",
								Expr:  `absent(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[2m]))) == 1`,
								For:   "10m",
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"severity":  "critical",
								},
							},
						},
					},
					{
						Name:     "slo-errors-test-default-availability",
						Interval: "30s",
						Rules: []vmv1beta1.Rule{
							{
								Record: "slo:burnrate",
								Expr:   `(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[5m]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[5m])))`,
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "5m",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   `(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[30m]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[30m])))`,
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "30m",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   `(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[1h]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[1h])))`,
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "1h",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   `(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[2h]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[2h])))`,
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "2h",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   `(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[6h]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[6h])))`,
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "6h",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   `(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[1d]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[1d])))`,
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "1d",
								},
							},
							{
								Record: "slo:burnrate",
								Expr:   `(sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana",response_code=~"5.*"}[4d]))) / (sum(rate(istio_requests_total{destination_workload_namespace=~"monitoring",destination_workload=~"grafana"}[4d])))`,
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"window":    "4d",
								},
							},
							{
								Alert: "SLOErrorBudgetBurn",
								Expr:  `slo:burnrate{window="5m", id="test-default-availability"} > (14 * (1-0.9)) and slo:burnrate{window="1h", id="test-default-availability"} > (14 * (1-0.9))`,
								For:   "2m",
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"severity":  "error",
								},
							},
							{
								Alert: "SLOErrorBudgetBurn",
								Expr:  `slo:burnrate{window="30m", id="test-default-availability"} > (7 * (1-0.9)) and slo:burnrate{window="6h", id="test-default-availability"} > (7 * (1-0.9))`,
								For:   "15m",
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"severity":  "error",
								},
							},
							{
								Alert: "SLOErrorBudgetBurn",
								Expr:  `slo:burnrate{window="2h", id="test-default-availability"} > (2 * (1-0.9)) and slo:burnrate{window="1d", id="test-default-availability"} > (2 * (1-0.9))`,
								For:   "1h",
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"severity":  "warning",
								},
							},
							{
								Alert: "SLOErrorBudgetBurn",
								Expr:  `slo:burnrate{window="6h", id="test-default-availability"} > (1 * (1-0.9)) and slo:burnrate{window="4d", id="test-default-availability"} > (1 * (1-0.9))`,
								For:   "3h",
								Labels: map[string]string{
									"namespace": "default",
									"name":      "test",
									"team":      "myteam",
									"id":        "test-default-availability",
									"slo":       "availability",
									"severity":  "warning",
								},
							},
						},
					},
				},
			}))
		})
	})
})
