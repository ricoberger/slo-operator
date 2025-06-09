package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceLevelObjectiveSpec defines the desired state of ServiceLevelObjective
type ServiceLevelObjectiveSpec struct {
	// SLOs is a list of slos for the service
	SLOs []SLO `json:"slos,omitempty"`
}

type SLO struct {
	// Name is the name of the SLO, e.g. "errors", "latency", etc.
	Name string `json:"name,omitempty"`
	// Objective is the objective for the SLO, e.g. "99.9% uptime", "95%
	// requests in 200ms", etc. It must be a percentage value between 1 and 100
	// as string, e.g. "99.9".
	Objective string `json:"objective,omitempty"`
	// A description for the SLO.
	Description string `json:"description,omitempty"`
	// SLI contains the metrics to calculate the SLO. For example the total
	// metric is the number of all requests, while the error metric is only the
	// number of all 5xx requests.
	SLI SLI `json:"sli,omitempty"`
	// Alerting can be used to adjust the alerting configuration for the SLO.
	Alerting Alerting `json:"alerting,omitempty"`
}

type SLI struct {
	TotalQuery string `json:"totalQuery,omitempty"`
	ErrorQuery string `json:"errorQuery,omitempty"`
}

type Alerting struct {
	// Disabled can be used to disable the alerting. If the field is set to
	// "true" the operator will not generate alerting rules for Prometheus.
	Disabled bool `json:"disabled,omitempty"`
	// Severities is a list of severities for the alerting rules created by the
	// operator for the absent alert and the burn rate alerts. The list must
	// contain 5 entries. The first one is used for the absent alert and the
	// remaining 4 for the burn rate alerts ordered by criticality.
	//
	// The default list which is used, when the field is not set is ["critial",
	// "error", "error", "warning", "warning"]
	Severities []string `json:"severities,omitempty"`
}

// ServiceLevelObjectiveStatus defines the observed state of
// ServiceLevelObjective.
type ServiceLevelObjectiveStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ServiceLevelObjective is the Schema for the servicelevelobjectives API.
type ServiceLevelObjective struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceLevelObjectiveSpec   `json:"spec,omitempty"`
	Status ServiceLevelObjectiveStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceLevelObjectiveList contains a list of ServiceLevelObjective.
type ServiceLevelObjectiveList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceLevelObjective `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ServiceLevelObjective{}, &ServiceLevelObjectiveList{})
}
