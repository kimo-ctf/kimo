/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// InstancePhase represents the lifecycle phase of an instance.
// +kubebuilder:validation:Enum=Pending;Creating;Running;Unhealthy;Expiring;Expired;Terminating;Failed
type InstancePhase string

const (
	InstancePhasePending     InstancePhase = "Pending"
	InstancePhaseCreating    InstancePhase = "Creating"
	InstancePhaseRunning     InstancePhase = "Running"
	InstancePhaseUnhealthy   InstancePhase = "Unhealthy"
	InstancePhaseExpiring    InstancePhase = "Expiring"
	InstancePhaseExpired     InstancePhase = "Expired"
	InstancePhaseTerminating InstancePhase = "Terminating"
	InstancePhaseFailed      InstancePhase = "Failed"
)

// ChallengeInstanceSpec defines the desired state of ChallengeInstance
type ChallengeInstanceSpec struct {
	TemplateRef string `json:"templateRef"`
	Team        string `json:"team"`
	Player      string `json:"player,omitempty"`
	TTLOverride string `json:"ttlOverride,omitempty"` // e.g. "45m"
}

// ChallengeInstanceStatus defines the observed state of ChallengeInstance.
type ChallengeInstanceStatus struct {
	Phase          InstancePhase `json:"phase,omitempty"`
	Reason         string        `json:"reason,omitempty"`
	Endpoint       string        `json:"endpoint,omitempty"`
	StartedAt      *metav1.Time  `json:"startedAt,omitempty"`
	ExpiresAt      *metav1.Time  `json:"expiresAt,omitempty"`
	PodName        string        `json:"podName,omitempty"`
	UnhealthyCount int32         `json:"unhealthyCount,omitempty"` // consecutive failed readiness checks
	Message        string        `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Challenge",type=string,JSONPath=`.spec.templateRef`
// +kubebuilder:printcolumn:name="Team",type=string,JSONPath=`.spec.team`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.endpoint`
// +kubebuilder:printcolumn:name="Expires",type=date,JSONPath=`.status.expiresAt`

// ChallengeInstance is the Schema for the challengeinstances API
type ChallengeInstance struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ChallengeInstance
	// +required
	Spec ChallengeInstanceSpec `json:"spec"`

	// status defines the observed state of ChallengeInstance
	// +optional
	Status ChallengeInstanceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ChallengeInstanceList contains a list of ChallengeInstance
type ChallengeInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ChallengeInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ChallengeInstance{}, &ChallengeInstanceList{})
		return nil
	})
}
