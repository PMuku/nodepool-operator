package controller

import (
	"context"

	nodepoolv1 "github.com/PMuku/nodepool-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

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
