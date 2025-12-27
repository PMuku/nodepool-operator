/*
Copyright 2025.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NodePoolPhase (enum) represents the phase of a NodePool
type NodePoolPhase string

const (
	NodePoolPhaseReady    NodePoolPhase = "Ready"
	NodePoolPhasePending  NodePoolPhase = "Pending"
	NodePoolPhaseDegraded NodePoolPhase = "Degraded"
)

// NodePoolSpec defines the desired state of NodePool
type NodePoolSpec struct {
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// NodePool size
	// +kubebuilder:validation:Minimum=0
	Size int32 `json:"size"`

	// Label applied to nodes in the pool (key=value)
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9_.-]+=[a-zA-Z0-9_.-]+$`
	Label string `json:"label"`

	// NodeSelector defines required labels for eligible nodes
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Taint applied to nodes in the pool (key=value:effect)
	// +optional
	Taint string `json:"taint,omitempty"` // optional field
}

// NodePoolStatus defines the observed state of NodePool.
type NodePoolStatus struct {
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	CurrentSize int32 `json:"currentSize,omitempty"`
	DesiredSize int32 `json:"desiredSize,omitempty"`
	// Phase indicates the overall health of the NodePool
	// +kubebuilder:validation:Enum=Ready;Pending;Degraded
	Phase         NodePoolPhase `json:"phase,omitempty"`
	AssignedNodes []string      `json:"assignedNodes,omitempty"`
	Message       string        `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// NodePool is the Schema for the nodepools API
type NodePool struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of NodePool
	// +required
	Spec NodePoolSpec `json:"spec"`

	// status defines the observed state of NodePool
	// +optional
	Status NodePoolStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// NodePoolList contains a list of NodePool
type NodePoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NodePool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodePool{}, &NodePoolList{})
}
