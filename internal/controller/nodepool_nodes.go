package controller

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nodepoolv1 "github.com/PMuku/nodepool-operator/api/v1"
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

func parseTaint(s string) (corev1.Taint, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return corev1.Taint{}, fmt.Errorf("invalid taint %q", s)
	}

	kv := strings.SplitN(parts[0], "=", 2)
	if len(kv) != 2 {
		return corev1.Taint{}, fmt.Errorf("invalid taint %q", s)
	}

	return corev1.Taint{
		Key:    kv[0],
		Value:  kv[1],
		Effect: corev1.TaintEffect(parts[1]),
	}, nil
}

func (r *NodePoolReconciler) assignNodeToPool(ctx context.Context, node *corev1.Node, nodePool *nodepoolv1.NodePool) error {
	labelParts := strings.SplitN(nodePool.Spec.Label, "=", 2)
	if len(labelParts) != 2 {
		return fmt.Errorf("invalid label %q, expected key=value", nodePool.Spec.Label)
	}
	labelKey, labelVal := labelParts[0], labelParts[1]

	var taint corev1.Taint
	if nodePool.Spec.Taint != "" {
		var err error
		taint, err = parseTaint(nodePool.Spec.Taint)
		if err != nil {
			return err
		}
	} else {
		taint = corev1.Taint{
			Key:    labelKey,
			Value:  labelVal,
			Effect: corev1.TaintEffectNoSchedule,
		}
	}

	original := node.DeepCopy()

	if node.Labels == nil {
		node.Labels = map[string]string{}
	}
	node.Labels[labelKey] = labelVal
	node.Labels[poolLabelKey] = nodePool.Name

	taintExists := false
	for _, t := range node.Spec.Taints {
		if t.Key == taint.Key && t.Value == taint.Value && t.Effect == taint.Effect {
			taintExists = true
			break
		}
	}
	if !taintExists {
		node.Spec.Taints = append(node.Spec.Taints, taint)
	}

	return r.Patch(ctx, node, client.MergeFrom(original))
}

func (r *NodePoolReconciler) releaseNodeFromPool(ctx context.Context, node *corev1.Node, nodePool *nodepoolv1.NodePool) error {
	original := node.DeepCopy()

	delete(node.Labels, poolLabelKey)

	if nodePool.Spec.Label != "" {
		parts := strings.SplitN(nodePool.Spec.Label, "=", 2)
		if len(parts) == 2 {
			delete(node.Labels, parts[0])
		}
	}

	var taintToRemove *corev1.Taint
	if nodePool.Spec.Taint != "" {
		taint, err := parseTaint(nodePool.Spec.Taint)
		if err != nil {
			return err
		}
		taintToRemove = &taint
	} else {
		parts := strings.SplitN(nodePool.Spec.Label, "=", 2)
		if len(parts) == 2 {
			taintToRemove = &corev1.Taint{
				Key:    parts[0],
				Value:  parts[1],
				Effect: corev1.TaintEffectNoSchedule,
			}
		}
	}
	if taintToRemove != nil {
		newTaints := node.Spec.Taints[:0]
		for _, t := range node.Spec.Taints {
			if t.Key == taintToRemove.Key && t.Value == taintToRemove.Value && t.Effect == taintToRemove.Effect {
				continue
			}
			newTaints = append(newTaints, t)
		}
		node.Spec.Taints = newTaints
	}
	return r.Patch(ctx, node, client.MergeFrom(original))
}
