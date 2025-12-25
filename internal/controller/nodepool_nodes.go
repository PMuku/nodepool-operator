package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nodepoolv1 "github.com/PMuku/gpu-nodepool-operator/api/v1"
)

const (
	poolLabelKey = "nodepool.k8s.local/name" // to mark nodes as belonging to some pool
)

func (r *NodePoolReconciler) getAssignedNodes(ctx context.Context, nodePool *nodepoolv1.NodePool) ([]corev1.Node, error) {
	var nodeList corev1.NodeList

	labelSelector := client.MatchingLabels{
		poolLabelKey: nodePool.Name,
	}

	if err := r.List(ctx, &nodeList, labelSelector); err != nil {
		return nil, err
	}

	return nodeList.Items, nil
}

func (r *NodePoolReconciler) getUsableAssignedNodes(ctx context.Context, nodePool *nodepoolv1.NodePool) ([]corev1.Node, error) {
	assignedNodes, err := r.getAssignedNodes(ctx, nodePool)
	if err != nil {
		return nil, err
	}

	var usable []corev1.Node
	for _, node := range assignedNodes {
		if isNodeReady(&node) && !node.Spec.Unschedulable {
			usable = append(usable, node)
		}
	}

	return usable, nil
}

func (r *NodePoolReconciler) getEligibleUnassignedNodes(ctx context.Context, nodePool *nodepoolv1.NodePool) ([]corev1.Node, error) {
	var nodeList corev1.NodeList
	if err := r.List(ctx, &nodeList); err != nil {
		return nil, err
	}

	var eligible []corev1.Node
	for _, node := range nodeList.Items {
		if isNodeEligible(&node, nodePool) {
			eligible = append(eligible, node)
		}
	}

	return eligible, nil
}

func (r *NodePoolReconciler) getSafeToReleaseAssignedNodes(ctx context.Context, nodePool *nodepoolv1.NodePool) ([]corev1.Node, error) {
	assignedNodes, err := r.getAssignedNodes(ctx, nodePool)
	if err != nil {
		return nil, err
	}

	var safeToRelease []corev1.Node
	for _, node := range assignedNodes {
		// Safe to release if cordoned or has maintenance label
		if node.Spec.Unschedulable || isInMaintenance(&node) {
			safeToRelease = append(safeToRelease, node)
		}
	}

	return safeToRelease, nil
}

func isNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func isNodeEligible(node *corev1.Node, nodePool *nodepoolv1.NodePool) bool {
	if !isNodeReady(node) {
		return false
	}

	if node.Spec.Unschedulable {
		return false
	}

	if _, exists := node.Labels["node-role.kubernetes.io/control-plane"]; exists {
		return false
	}
	if _, exists := node.Labels["node-role.kubernetes.io/master"]; exists {
		return false
	}

	if _, exists := node.Labels[poolLabelKey]; exists {
		return false
	}

	// generic nodeselector matching
	if nodePool.Spec.NodeSelector == nil {
		return !isGpuNode(node)
	}

	return matchesNodeSelector(node, nodePool.Spec.NodeSelector)
}

func isGpuNode(node *corev1.Node) bool {
	if qty, exists := node.Status.Capacity[corev1.ResourceName("nvidia.com/gpu")]; exists && !qty.IsZero() {
		return true
	}
	if val, exists := node.Labels["nvidia.com/gpu.present"]; exists && val == "true" {
		return true
	}
	return false
}

func matchesNodeSelector(node *corev1.Node, selector map[string]string) bool {
	for key, val := range selector {
		nodeVal, exists := node.Labels[key]
		if !exists || nodeVal != val {
			return false
		}
	}
	return true
}

func isInMaintenance(node *corev1.Node) bool {
	_, exists := node.Labels["nodepool.k8s.local/maintenance"]
	return exists
}
