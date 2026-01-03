package controller

import (
	"context"

	nodepoolv1 "github.com/PMuku/nodepool-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// helper function to reconcile deletion of a NodePool
func (r *NodePoolReconciler) reconcileDeletionHelper(ctx context.Context, nodePool *nodepoolv1.NodePool) (ctrl.Result, error) {
	// Release all assigned nodes
	assigned, err := r.getAssignedNodes(ctx, nodePool)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range assigned {
		if err := r.releaseNodeFromPool(ctx, &assigned[i], nodePool); err != nil {
			return ctrl.Result{}, err
		}
	}

	// record finalizer cleanup
	if r.Recorder != nil {
		r.Recorder.Eventf(
			nodePool,
			corev1.EventTypeNormal,
			"CleanedUp",
			"Released all nodes and removed NodePool finalizer",
		)
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(nodePool, finalizerName)
	return ctrl.Result{}, r.Update(ctx, nodePool)
}

type poolState struct {
	assigned           []corev1.Node
	usableAssigned     []corev1.Node
	eligibleUnassigned []corev1.Node
	safeToRelease      []corev1.Node
}

// helper function to get current cluster state relevant to a NodePool
func (r *NodePoolReconciler) getClusterState(ctx context.Context, nodePool *nodepoolv1.NodePool) (*poolState, error) {
	assigned, err := r.getAssignedNodes(ctx, nodePool)
	if err != nil {
		return nil, err
	}

	usableAssigned, err := r.getUsableAssignedNodes(ctx, nodePool)
	if err != nil {
		return nil, err
	}

	eligibleUnassigned, err := r.getEligibleUnassignedNodes(ctx, nodePool)
	if err != nil {
		return nil, err
	}

	safeToRelease, err := r.getSafeToReleaseAssignedNodes(ctx, nodePool)
	if err != nil {
		return nil, err
	}

	return &poolState{
		assigned:           assigned,
		usableAssigned:     usableAssigned,
		eligibleUnassigned: eligibleUnassigned,
		safeToRelease:      safeToRelease,
	}, nil
}

// helper function to handle scaling logic
func (r *NodePoolReconciler) scalingReconcile(ctx context.Context, nodePool *nodepoolv1.NodePool, state *poolState) (bool, ctrl.Result, error) {
	desiredCount := int(nodePool.Spec.Size)
	assignedCount := len(state.assigned)
	usableAssignedCount := len(state.usableAssigned)
	eligibleUnassignedCount := len(state.eligibleUnassigned)
	safeToReleaseCount := len(state.safeToRelease)
	effectiveAssignedCount := usableAssignedCount + safeToReleaseCount

	if needed := desiredCount - effectiveAssignedCount; needed > 0 && eligibleUnassignedCount > 0 {
		res, err := r.scaleUp(ctx, nodePool, state, needed)
		return true, res, err
	}

	if excess := assignedCount - desiredCount; excess > 0 && safeToReleaseCount > 0 {
		res, err := r.scaleDown(ctx, nodePool, state, excess)
		return true, res, err
	}

	return false, ctrl.Result{}, nil
}

func (r *NodePoolReconciler) scaleUp(ctx context.Context, nodePool *nodepoolv1.NodePool, state *poolState, needed int) (ctrl.Result, error) {
	toAssign := min(needed, len(state.eligibleUnassigned))

	for i := 0; i < toAssign; i++ {
		if err := r.assignNodeToPool(
			ctx,
			&state.eligibleUnassigned[i],
			nodePool,
		); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *NodePoolReconciler) scaleDown(ctx context.Context, nodePool *nodepoolv1.NodePool, state *poolState, excess int) (ctrl.Result, error) {
	toRelease := min(excess, len(state.safeToRelease))

	for i := 0; i < toRelease; i++ {
		if err := r.releaseNodeFromPool(
			ctx,
			&state.safeToRelease[i],
			nodePool,
		); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
