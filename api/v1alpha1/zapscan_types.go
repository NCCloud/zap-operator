package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ZapScanSpec defines the desired state of ZapScan.
type ZapScanSpec struct {
	// Target is the URL to scan (e.g. https://example.com)
	Target string `json:"target"`

	// OpenAPI is an optional URL or in-cluster path for an OpenAPI spec.
	// If set, the zap-full-scan job will be asked to import it.
	// +optional
	OpenAPI *string `json:"openapi,omitempty"`

	// Namespace to run the scan job in. Defaults to the Scan's namespace.
	// +optional
	JobNamespace *string `json:"jobNamespace,omitempty"`

	// ServiceAccountName used by the scan job.
	// +optional
	ServiceAccountName *string `json:"serviceAccountName,omitempty"`

	// Image is the container image to run. Defaults to ghcr.io/zaproxy/zaproxy:stable
	// +optional
	Image *string `json:"image,omitempty"`

	// Args are extra args passed to zap-full-scan script.
	// +optional
	Args []string `json:"args,omitempty"`

	// Cleanup controls whether completed Jobs should be deleted.
	// +optional
	Cleanup *bool `json:"cleanup,omitempty"`

	// This is the address to send the zap scan output
	// +optional
	Notification NotificationSpec `json:"notification,omitempty"`
}

// ZapScanStatus defines the observed state of ZapScan.
type ZapScanStatus struct {
	// Phase is a high-level state indicator.
	// +optional
	Phase string `json:"phase,omitempty"`

	// JobName is the name of the Job created for this scan.
	// +optional
	JobName string `json:"jobName,omitempty"`

	// StartedAt is when the scan job was created.
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`

	// FinishedAt is when the scan completed.
	// +optional
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`

	// AlertsFound is the total number of alerts parsed from results.
	// +optional
	AlertsFound int64 `json:"alertsFound,omitempty"`

	// LastError is a human-readable error if any.
	// +optional
	LastError string `json:"lastError,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=zaps

type ZapScan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ZapScanSpec   `json:"spec,omitempty"`
	Status ZapScanStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type ZapScanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ZapScan `json:"items"`
}

type NotificationSpec struct {
	// Defines which protocol it needs like, slackWebhook, smtp(in the future)
	// +optionals
	Protocol string `json:"protocol,omitempty"`
	// Defines target url to push the outputs
	// +optionals
	Url string `json:"url,omitempty"`
	// Defines whether the notifications enabled or not
	// +optionals
	Enabled bool `json:"enabled,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ZapScan{}, &ZapScanList{})
}
