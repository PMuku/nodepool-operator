package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	nodepoolv1 "github.com/PMuku/nodepool-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		err := r.scaleUp(ctx, nodePool, state, needed)
		return true, ctrl.Result{}, err
	}

	if excess := assignedCount - desiredCount; excess > 0 && safeToReleaseCount > 0 {
		err := r.scaleDown(ctx, nodePool, state, excess)
		return true, ctrl.Result{}, err
	}

	return false, ctrl.Result{}, nil
}

func (r *NodePoolReconciler) scaleUp(ctx context.Context, nodePool *nodepoolv1.NodePool, state *poolState, needed int) error {
	toAssign := min(needed, len(state.eligibleUnassigned))
	successfulAssign := 0

	for i := 0; i < toAssign; i++ {
		if err := r.assignNodeToPool(
			ctx,
			&state.eligibleUnassigned[i],
			nodePool,
		); err != nil {
			// record scale-up failure
			if r.Recorder != nil {
				r.Recorder.Eventf(
					nodePool,
					corev1.EventTypeWarning,
					"AssignFailed",
					"Failed to assign node %s: %v",
					state.eligibleUnassigned[i].Name,
					err,
				)
			}

			return err
		}
		successfulAssign++
	}
	// record scale-up success
	if r.Recorder != nil && successfulAssign > 0 {
		r.Recorder.Eventf(
			nodePool,
			corev1.EventTypeNormal,
			"ScaledUp",
			"Assigned %d node(s) to pool",
			successfulAssign,
		)
	}
	return nil
}

func (r *NodePoolReconciler) scaleDown(ctx context.Context, nodePool *nodepoolv1.NodePool, state *poolState, excess int) error {
	toRelease := min(excess, len(state.safeToRelease))
	successfulRelease := 0

	for i := 0; i < toRelease; i++ {
		if err := r.releaseNodeFromPool(
			ctx,
			&state.safeToRelease[i],
			nodePool,
		); err != nil {
			// record scale-down failure
			if r.Recorder != nil {
				r.Recorder.Eventf(
					nodePool,
					corev1.EventTypeWarning,
					"ReleaseFailed",
					"Failed to release node %s: %v",
					state.safeToRelease[i].Name,
					err,
				)
			}
			return err
		}
		successfulRelease++
	}

	// record scale-down success
	if r.Recorder != nil && successfulRelease > 0 {
		r.Recorder.Eventf(
			nodePool,
			corev1.EventTypeNormal,
			"ScaledDown",
			"Released %d node(s) from pool",
			successfulRelease,
		)
	}
	return nil
}

// functions to get and reconcile status
func getStatus(nodePool *nodepoolv1.NodePool, state *poolState) nodepoolv1.NodePoolStatus {
	desiredCount := int(nodePool.Spec.Size)
	assignedCount := len(state.assigned)
	usableAssignedCount := len(state.usableAssigned)
	eligibleUnassignedCount := len(state.eligibleUnassigned)
	safeToReleaseCount := len(state.safeToRelease)
	effectiveAssignedCount := usableAssignedCount + safeToReleaseCount

	newStatus := nodepoolv1.NodePoolStatus{
		DesiredSize:   int32(desiredCount),
		CurrentSize:   int32(effectiveAssignedCount),
		AssignedNodes: make([]string, 0, assignedCount),
	}
	for _, n := range state.assigned {
		newStatus.AssignedNodes = append(newStatus.AssignedNodes, n.Name)
	}

	switch {
	case usableAssignedCount == desiredCount:
		newStatus.Phase = nodepoolv1.NodePoolPhaseReady
		newStatus.Message = fmt.Sprintf(
			"Pool is ready (%d/%d nodes available)",
			usableAssignedCount,
			desiredCount,
		)

	case usableAssignedCount < desiredCount && eligibleUnassignedCount > 0:
		newStatus.Phase = nodepoolv1.NodePoolPhasePending
		newStatus.Message = fmt.Sprintf(
			"Waiting for eligible nodes (%d/%d available, %d eligible)",
			usableAssignedCount,
			desiredCount,
			eligibleUnassignedCount,
		)

	case usableAssignedCount < desiredCount && eligibleUnassignedCount == 0:
		newStatus.Phase = nodepoolv1.NodePoolPhaseDegraded
		newStatus.Message = fmt.Sprintf(
			"%d nodes needed but no eligible nodes available",
			desiredCount-usableAssignedCount,
		)

	case usableAssignedCount < desiredCount && safeToReleaseCount > 0:
		newStatus.Phase = nodepoolv1.NodePoolPhaseDegraded
		newStatus.Message = fmt.Sprintf(
			"Pool degraded: %d/%d nodes available (%d in maintenance)",
			usableAssignedCount,
			desiredCount,
			safeToReleaseCount,
		)

	case usableAssignedCount > desiredCount && safeToReleaseCount == 0:
		newStatus.Phase = nodepoolv1.NodePoolPhasePending
		newStatus.Message = fmt.Sprintf(
			"Over capacity (%d/%d), waiting for nodes to be cordoned",
			usableAssignedCount,
			desiredCount,
		)

	case usableAssignedCount > desiredCount && safeToReleaseCount > 0:
		newStatus.Phase = nodepoolv1.NodePoolPhasePending
		newStatus.Message = fmt.Sprintf(
			"Over capacity (%d/%d); %d node(s) ready for release",
			usableAssignedCount,
			desiredCount,
			safeToReleaseCount,
		)
	}

	return newStatus
}

func (r *NodePoolReconciler) reconcileStatus(ctx context.Context, nodePool *nodepoolv1.NodePool, state *poolState) (ctrl.Result, error) {
	original := nodePool.DeepCopy()
	newStatus := getStatus(nodePool, state)

	nodePool.Status = newStatus
	if reflect.DeepEqual(original.Status, nodePool.Status) {
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	// record phase change events
	if original.Status.Phase != nodePool.Status.Phase && r.Recorder != nil {
		switch nodePool.Status.Phase {
		case nodepoolv1.NodePoolPhaseReady:
			r.Recorder.Event(nodePool, corev1.EventTypeNormal, "Ready",
				nodePool.Status.Message)
		case nodepoolv1.NodePoolPhaseDegraded:
			r.Recorder.Event(nodePool, corev1.EventTypeWarning, "Degraded",
				nodePool.Status.Message)
		case nodepoolv1.NodePoolPhasePending:
			r.Recorder.Event(nodePool, corev1.EventTypeNormal, "Pending",
				nodePool.Status.Message)
		}
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute},
		r.Status().Patch(ctx, nodePool, client.MergeFrom(original))
}
