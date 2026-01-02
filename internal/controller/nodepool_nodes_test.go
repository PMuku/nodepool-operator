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

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	nodepoolv1 "github.com/PMuku/nodepool-operator/api/v1"
)

// =============================================================================
// Test: isNodeReady
// =============================================================================
// Tests whether a node's Ready condition is correctly detected.
// The function checks node.Status.Conditions for NodeReady=True.

func TestIsNodeReady(t *testing.T) {
	tests := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "node is ready",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			},
			expected: true,
		},
		{
			name: "node is not ready",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
					},
				},
			},
			expected: false,
		},
		{
			name: "node ready condition unknown",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionUnknown},
					},
				},
			},
			expected: false,
		},
		{
			name: "node has no conditions",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{},
				},
			},
			expected: false,
		},
		{
			name: "node has other conditions but not ready",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
						{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNodeReady(tt.node)
			if result != tt.expected {
				t.Errorf("isNodeReady() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Test: isGpuNode
// =============================================================================
// Tests GPU node detection via:
// 1. node.Status.Capacity["nvidia.com/gpu"] > 0
// 2. node.Labels["nvidia.com/gpu.present"] == "true"

func TestIsGpuNode(t *testing.T) {
	tests := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "node has GPU capacity",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
				},
			},
			expected: true,
		},
		{
			name: "node has GPU label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"nvidia.com/gpu.present": "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "node has GPU label set to false",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"nvidia.com/gpu.present": "false",
					},
				},
			},
			expected: false,
		},
		{
			name: "node has zero GPU capacity",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("0"),
					},
				},
			},
			expected: false,
		},
		{
			name: "node has no GPU indicators",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isGpuNode(tt.node)
			if result != tt.expected {
				t.Errorf("isGpuNode() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Test: matchesNodeSelector
// =============================================================================
// Tests if node labels match all key-value pairs in the selector.
// All selector entries must be present and match exactly.

func TestMatchesNodeSelector(t *testing.T) {
	tests := []struct {
		name     string
		node     *corev1.Node
		selector map[string]string
		expected bool
	}{
		{
			name: "exact match single label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"zone": "us-east-1a",
					},
				},
			},
			selector: map[string]string{"zone": "us-east-1a"},
			expected: true,
		},
		{
			name: "exact match multiple labels",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"zone":        "us-east-1a",
						"instance":    "gpu",
						"environment": "production",
					},
				},
			},
			selector: map[string]string{
				"zone":     "us-east-1a",
				"instance": "gpu",
			},
			expected: true,
		},
		{
			name: "selector key missing from node",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"zone": "us-east-1a",
					},
				},
			},
			selector: map[string]string{
				"zone":     "us-east-1a",
				"instance": "gpu",
			},
			expected: false,
		},
		{
			name: "selector value mismatch",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"zone": "us-west-2a",
					},
				},
			},
			selector: map[string]string{"zone": "us-east-1a"},
			expected: false,
		},
		{
			name: "empty selector matches any node",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"zone": "us-east-1a",
					},
				},
			},
			selector: map[string]string{},
			expected: true,
		},
		{
			name: "nil selector matches any node",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"zone": "us-east-1a",
					},
				},
			},
			selector: nil,
			expected: true,
		},
		{
			name: "node with no labels fails non-empty selector",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			selector: map[string]string{"zone": "us-east-1a"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesNodeSelector(tt.node, tt.selector)
			if result != tt.expected {
				t.Errorf("matchesNodeSelector() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Test: isInMaintenance
// =============================================================================
// Tests if node has the maintenance label: nodepool.k8s.local/maintenance

func TestIsInMaintenance(t *testing.T) {
	tests := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "node has maintenance label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"nodepool.k8s.local/maintenance": "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "node has maintenance label with empty value",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"nodepool.k8s.local/maintenance": "",
					},
				},
			},
			expected: true, // Label exists, value doesn't matter
		},
		{
			name: "node without maintenance label",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"other-label": "value",
					},
				},
			},
			expected: false,
		},
		{
			name: "node with no labels",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isInMaintenance(tt.node)
			if result != tt.expected {
				t.Errorf("isInMaintenance() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Test: parseTaint
// =============================================================================
// Tests taint string parsing: "key=value:effect"
// Valid effects: NoSchedule, PreferNoSchedule, NoExecute

func TestParseTaint(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    corev1.Taint
		expectError bool
	}{
		{
			name:  "valid taint with NoSchedule",
			input: "gpu=true:NoSchedule",
			expected: corev1.Taint{
				Key:    "gpu",
				Value:  "true",
				Effect: corev1.TaintEffectNoSchedule,
			},
			expectError: false,
		},
		{
			name:  "valid taint with NoExecute",
			input: "dedicated=ml-workload:NoExecute",
			expected: corev1.Taint{
				Key:    "dedicated",
				Value:  "ml-workload",
				Effect: corev1.TaintEffectNoExecute,
			},
			expectError: false,
		},
		{
			name:  "valid taint with PreferNoSchedule",
			input: "spot=true:PreferNoSchedule",
			expected: corev1.Taint{
				Key:    "spot",
				Value:  "true",
				Effect: corev1.TaintEffectPreferNoSchedule,
			},
			expectError: false,
		},
		{
			name:        "invalid taint - missing effect",
			input:       "gpu=true",
			expected:    corev1.Taint{},
			expectError: true,
		},
		{
			name:        "invalid taint - missing value",
			input:       "gpu:NoSchedule",
			expected:    corev1.Taint{},
			expectError: true,
		},
		{
			name:        "invalid taint - empty string",
			input:       "",
			expected:    corev1.Taint{},
			expectError: true,
		},
		{
			name:        "invalid taint - only key",
			input:       "gpu",
			expected:    corev1.Taint{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseTaint(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("parseTaint() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("parseTaint() unexpected error: %v", err)
				return
			}

			if result.Key != tt.expected.Key ||
				result.Value != tt.expected.Value ||
				result.Effect != tt.expected.Effect {
				t.Errorf("parseTaint() = %+v, expected %+v", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Test: isNodeEligible
// =============================================================================
// Tests comprehensive node eligibility for a NodePool:
// - Must be Ready
// - Must be schedulable (not cordoned)
// - Must not be control-plane/master
// - Must not already belong to a pool
// - Must match NodeSelector (if specified)
// - Must not be a GPU node (if no NodeSelector specified)

func TestIsNodeEligible(t *testing.T) {
	tests := []struct {
		name     string
		node     *corev1.Node
		nodePool *nodepoolv1.NodePool
		expected bool
	}{
		{
			name: "eligible node - ready, schedulable, matches selector",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-1",
					Labels: map[string]string{
						"zone": "us-east-1a",
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			},
			nodePool: &nodepoolv1.NodePool{
				Spec: nodepoolv1.NodePoolSpec{
					Size:         2,
					Label:        "pool=test",
					NodeSelector: map[string]string{"zone": "us-east-1a"},
				},
			},
			expected: true,
		},
		{
			name: "ineligible - node not ready",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "worker-1",
					Labels: map[string]string{},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
					},
				},
			},
			nodePool: &nodepoolv1.NodePool{
				Spec: nodepoolv1.NodePoolSpec{
					Size:  2,
					Label: "pool=test",
				},
			},
			expected: false,
		},
		{
			name: "ineligible - node is cordoned",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "worker-1",
					Labels: map[string]string{},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: true, // Cordoned
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			},
			nodePool: &nodepoolv1.NodePool{
				Spec: nodepoolv1.NodePoolSpec{
					Size:  2,
					Label: "pool=test",
				},
			},
			expected: false,
		},
		{
			name: "ineligible - control-plane node",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "control-plane-1",
					Labels: map[string]string{
						"node-role.kubernetes.io/control-plane": "",
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			},
			nodePool: &nodepoolv1.NodePool{
				Spec: nodepoolv1.NodePoolSpec{
					Size:  2,
					Label: "pool=test",
				},
			},
			expected: false,
		},
		{
			name: "ineligible - master node (legacy label)",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "master-1",
					Labels: map[string]string{
						"node-role.kubernetes.io/master": "",
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			},
			nodePool: &nodepoolv1.NodePool{
				Spec: nodepoolv1.NodePoolSpec{
					Size:  2,
					Label: "pool=test",
				},
			},
			expected: false,
		},
		{
			name: "ineligible - already assigned to a pool",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-1",
					Labels: map[string]string{
						"nodepool.k8s.local/name": "other-pool",
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			},
			nodePool: &nodepoolv1.NodePool{
				Spec: nodepoolv1.NodePoolSpec{
					Size:  2,
					Label: "pool=test",
				},
			},
			expected: false,
		},
		{
			name: "ineligible - does not match NodeSelector",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "worker-1",
					Labels: map[string]string{
						"zone": "us-west-2a", // Different zone
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			},
			nodePool: &nodepoolv1.NodePool{
				Spec: nodepoolv1.NodePoolSpec{
					Size:         2,
					Label:        "pool=test",
					NodeSelector: map[string]string{"zone": "us-east-1a"},
				},
			},
			expected: false,
		},
		{
			name: "ineligible - GPU node without NodeSelector",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-worker-1",
					Labels: map[string]string{
						"nvidia.com/gpu.present": "true",
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			},
			nodePool: &nodepoolv1.NodePool{
				Spec: nodepoolv1.NodePoolSpec{
					Size:         2,
					Label:        "pool=test",
					NodeSelector: nil, // No selector - should exclude GPU nodes
				},
			},
			expected: false,
		},
		{
			name: "eligible - GPU node WITH matching NodeSelector",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gpu-worker-1",
					Labels: map[string]string{
						"nvidia.com/gpu.present": "true",
						"gpu-type":               "a100",
					},
				},
				Spec: corev1.NodeSpec{
					Unschedulable: false,
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
					},
				},
			},
			nodePool: &nodepoolv1.NodePool{
				Spec: nodepoolv1.NodePoolSpec{
					Size:         2,
					Label:        "pool=gpu",
					NodeSelector: map[string]string{"gpu-type": "a100"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isNodeEligible(tt.node, tt.nodePool)
			if result != tt.expected {
				t.Errorf("isNodeEligible() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
