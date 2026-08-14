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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// InstanceMode defines how challenge instances are scoped.
// +kubebuilder:validation:Enum=shared;perTeam;perPlayer
type InstanceMode string

const (
	InstanceModeShared    InstanceMode = "shared"
	InstanceModePerTeam   InstanceMode = "perTeam"
	InstanceModePerPlayer InstanceMode = "perPlayer"
)

// PoWSpec configures Proof of Work for a challenge.
type PoWSpec struct {
	Enabled    bool   `json:"enabled"`
	Difficulty int    `json:"difficulty"`          // leading zero bits
	Algorithm  string `json:"algorithm,omitempty"` // sha256 (default)
	TTL        string `json:"ttl,omitempty"`       // puzzle expiry, e.g. "5m"
}

// ContainerPort defines an exposed port.
type ContainerPort struct {
	Name          string `json:"name"`
	ContainerPort int32  `json:"containerPort"`
	Expose        bool   `json:"expose,omitempty"`
}

// ResourceRequirements mirrors k8s resource requests/limits.
type ResourceRequirements struct {
	Requests map[corev1.ResourceName]resource.Quantity `json:"requests,omitempty"`
	Limits   map[corev1.ResourceName]resource.Quantity `json:"limits,omitempty"`
}

// ReadinessType selects how the Instance Controller checks container readiness.
// +kubebuilder:validation:Enum=tcp;http;none
type ReadinessType string

const (
	ReadinessTCP  ReadinessType = "tcp"
	ReadinessHTTP ReadinessType = "http"
	ReadinessNone ReadinessType = "none"
)

// ReadinessCheck configures the Pending/Creating -> Running transition.
// Defaults to a TCP check on the first exposed port when omitted.
type ReadinessCheck struct {
	Type ReadinessType `json:"type,omitempty"`
	Port int32         `json:"port,omitempty"`
	Path string        `json:"path,omitempty"` // http only
}

// RestartPolicy mirrors a subset of corev1.RestartPolicy.
// +kubebuilder:validation:Enum=OnFailure;Always;Never
type RestartPolicy string

const (
	RestartOnFailure RestartPolicy = "OnFailure"
	RestartAlways    RestartPolicy = "Always"
	RestartNever     RestartPolicy = "Never"
)

// ContainerSpec defines the container runtime configuration.
type ContainerSpec struct {
	Image              string               `json:"image"`
	Ports              []ContainerPort      `json:"ports,omitempty"`
	Resources          ResourceRequirements `json:"resources,omitempty"`
	Env                []corev1.EnvVar      `json:"env,omitempty"`
	Readiness          *ReadinessCheck      `json:"readiness,omitempty"`
	RestartPolicy      RestartPolicy        `json:"restartPolicy,omitempty"`      // default OnFailure
	UnhealthyThreshold int32                `json:"unhealthyThreshold,omitempty"` // default 3
}

// ChallengeTemplateSpec defines the desired state of ChallengeTemplate.
type ChallengeTemplateSpec struct {
	Category      string                      `json:"category,omitempty"`
	Difficulty    string                      `json:"difficulty,omitempty"`
	Points        int                         `json:"points,omitempty"`
	FlagSecretRef corev1.LocalObjectReference `json:"flagSecretRef"`
	InstanceMode  InstanceMode                `json:"instanceMode"`
	TTL           string                      `json:"ttl"` // e.g. "30m"
	MaxInstances  int                         `json:"maxInstances"`
	PoW           *PoWSpec                    `json:"pow,omitempty"`
	Container     ContainerSpec               `json:"container"`
}

// ChallengeTemplateStatus defines the observed state of ChallengeTemplate.
type ChallengeTemplateStatus struct {
	Ready         bool   `json:"ready"`
	InstanceCount int    `json:"instanceCount"`
	Message       string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.instanceMode`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Instances",type=integer,JSONPath=`.status.instanceCount`

// ChallengeTemplate is the Schema for the challengetemplates API
type ChallengeTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of ChallengeTemplate
	// +required
	Spec ChallengeTemplateSpec `json:"spec"`

	// status defines the observed state of ChallengeTemplate
	// +optional
	Status ChallengeTemplateStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ChallengeTemplateList contains a list of ChallengeTemplate
type ChallengeTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ChallengeTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &ChallengeTemplate{}, &ChallengeTemplateList{})
		return nil
	})
}
