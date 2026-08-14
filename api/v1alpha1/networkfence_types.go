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

// AllowRule permits egress to a CIDR/port.
type AllowRule struct {
	CIDR string `json:"cidr,omitempty"`
	Port int32  `json:"port,omitempty"`
}

// DenyRule blocks traffic to a namespace.
type DenyRule struct {
	To string `json:"to,omitempty"` // namespace name to block
}

// NetworkFenceSpec defines the desired state of NetworkFence
type NetworkFenceSpec struct {
	InstanceRef string      `json:"instanceRef"`
	AllowRules  []AllowRule `json:"allow,omitempty"`
	DenyRules   []DenyRule  `json:"deny,omitempty"`
	AllowEgress bool        `json:"allowEgress,omitempty"` // allow internet
}

// NetworkFenceStatus defines the observed state of NetworkFence.
type NetworkFenceStatus struct {
	Applied bool   `json:"applied"`
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Instance",type=string,JSONPath=`.spec.instanceRef`
// +kubebuilder:printcolumn:name="Applied",type=boolean,JSONPath=`.status.applied`

// NetworkFence is the Schema for the networkfences API
type NetworkFence struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of NetworkFence
	// +required
	Spec NetworkFenceSpec `json:"spec"`

	// status defines the observed state of NetworkFence
	// +optional
	Status NetworkFenceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// NetworkFenceList contains a list of NetworkFence
type NetworkFenceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NetworkFence `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &NetworkFence{}, &NetworkFenceList{})
		return nil
	})
}
