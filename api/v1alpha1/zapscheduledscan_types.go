package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ZapScheduledScanSpec defines the desired state of ZapScheduledScan.
type ZapScheduledScanSpec struct {
	// Schedule is a cron expression in standard 5-field format (min hour dom mon dow).
	Schedule string `json:"schedule"`

	// Template defines the ZapScan spec that will be used to create ZapScan objects.
	Template ZapScanSpec `json:"template"`

	// Suspend stops creating new Scan objects.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// ConcurrencyPolicy determines what happens if a previous run is still active.
	// Allowed values: Allow, Forbid, Replace.
	// +optional
	ConcurrencyPolicy *string `json:"concurrencyPolicy,omitempty"`
}

// ZapScheduledScanStatus defines the observed state of ZapScheduledScan.
type ZapScheduledScanStatus struct {
	// LastScheduleTime is the last time a Scan was created from this schedule.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// Active holds the names of currently active Scans.
	// +optional
	Active []string `json:"active,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=zapsched

type ZapScheduledScan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ZapScheduledScanSpec   `json:"spec,omitempty"`
	Status ZapScheduledScanStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type ZapScheduledScanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ZapScheduledScan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ZapScheduledScan{}, &ZapScheduledScanList{})
}
