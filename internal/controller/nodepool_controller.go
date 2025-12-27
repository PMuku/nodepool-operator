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
	"context"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	nodepoolv1 "github.com/PMuku/nodepool-operator/api/v1"
)

// NodePoolReconciler reconciles a NodePool object
type NodePoolReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Finalizer for NodePool
const finalizerName = "nodepool.k8s.local/finalizer"

// +kubebuilder:rbac:groups=nodepool.k8s.local,resources=nodepools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nodepool.k8s.local,resources=nodepools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nodepool.k8s.local,resources=nodepools/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NodePool object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *NodePoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the NodePool instance
	var nodePool nodepoolv1.NodePool
	if err := r.Get(ctx, req.NamespacedName, &nodePool); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if nodePool.DeletionTimestamp != nil {
		// Release all assigned nodes
		assigned, err := r.getAssignedNodes(ctx, &nodePool)
		if err != nil {
			return ctrl.Result{}, err
		}
		for i := range assigned {
			if err := r.releaseNodeFromPool(ctx, &assigned[i], &nodePool); err != nil {
				return ctrl.Result{}, err
			}
		}

		// record finalizer cleanup
		if r.Recorder != nil {
			r.Recorder.Eventf(
				&nodePool,
				corev1.EventTypeNormal,
				"CleanedUp",
				"Released all nodes and removed NodePool finalizer",
			)
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(&nodePool, finalizerName)

		if err := r.Update(ctx, &nodePool); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(&nodePool, finalizerName) {
		controllerutil.AddFinalizer(&nodePool, finalizerName)
		return ctrl.Result{Requeue: true}, r.Update(ctx, &nodePool)
	}

	assigned, err := r.getAssignedNodes(ctx, &nodePool)
	if err != nil {
		return ctrl.Result{}, err
	}

	usableAssigned, err := r.getUsableAssignedNodes(ctx, &nodePool)
	if err != nil {
		return ctrl.Result{}, err
	}

	eligibleUnassigned, err := r.getEligibleUnassignedNodes(ctx, &nodePool)
	if err != nil {
		return ctrl.Result{}, err
	}

	safeToRelease, err := r.getSafeToReleaseAssignedNodes(ctx, &nodePool)
	if err != nil {
		return ctrl.Result{}, err
	}

	assignedCount := len(assigned)
	usableAssignedCount := len(usableAssigned)
	eligibleUnassignedCount := len(eligibleUnassigned)
	safeToReleaseCount := len(safeToRelease)
	desiredCount := int(nodePool.Spec.Size)

	// Log nodepool state
	log.Info("NodePool reconciliation state",
		"nodepool", nodePool.Name,
		"desiredSize", nodePool.Spec.Size,
		"assigned", assignedCount,
		"usableAssigned", usableAssignedCount,
		"eligibleUnassigned", eligibleUnassignedCount,
		"safeToRelease", safeToReleaseCount,
	)

	// Scale-up
	effectiveAssignedCount := usableAssignedCount + safeToReleaseCount
	needed := desiredCount - effectiveAssignedCount
	if needed > 0 && eligibleUnassignedCount > 0 {
		toAssign := min(needed, eligibleUnassignedCount)

		successfulAssign, failure := 0, false
		for i := 0; i < toAssign; i++ {
			if err := r.assignNodeToPool(ctx, &eligibleUnassigned[i], &nodePool); err != nil {
				// record scale-up failure
				if r.Recorder != nil && !failure {
					r.Recorder.Eventf(
						&nodePool,
						corev1.EventTypeWarning,
						"AssignFailed",
						"Failed to assign node %s: %v",
						eligibleUnassigned[i].Name,
						err,
					)
					failure = true
				}
				return ctrl.Result{}, err
			}
			successfulAssign++
		}

		// record scale-up success
		if r.Recorder != nil && successfulAssign > 0 {
			r.Recorder.Eventf(
				&nodePool,
				corev1.EventTypeNormal,
				"ScaledUp",
				"Assigned %d node(s) to pool",
				successfulAssign,
			)
		}
		return ctrl.Result{}, nil
	}

	// Scale-down
	excess := assignedCount - desiredCount
	if excess > 0 && safeToReleaseCount > 0 {
		toRelease := min(excess, safeToReleaseCount)

		successfulRelease, failure := 0, false
		for i := 0; i < toRelease; i++ {
			if err := r.releaseNodeFromPool(ctx, &safeToRelease[i], &nodePool); err != nil {
				// record scale-down failure
				if r.Recorder != nil && !failure {
					r.Recorder.Eventf(
						&nodePool,
						corev1.EventTypeWarning,
						"ReleaseFailed",
						"Failed to release node %s: %v",
						safeToRelease[i].Name,
						err,
					)
					failure = true
				}
				return ctrl.Result{}, err
			}
			successfulRelease++
		}

		// record scale-down success
		if r.Recorder != nil && successfulRelease > 0 {
			r.Recorder.Eventf(
				&nodePool,
				corev1.EventTypeNormal,
				"ScaledDown",
				"Released %d node(s) from pool",
				successfulRelease,
			)
		}

		return ctrl.Result{}, nil
	}

	// Status update

	original := nodePool.DeepCopy()
	newStatus := nodePool.Status.DeepCopy()
	newStatus.DesiredSize = int32(desiredCount)
	newStatus.CurrentSize = int32(effectiveAssignedCount)
	newStatus.AssignedNodes = make([]string, 0, len(assigned))
	for _, n := range assigned {
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

	nodePool.Status = *newStatus

	if !reflect.DeepEqual(original.Status, nodePool.Status) {

		// record phase change events
		if original.Status.Phase != nodePool.Status.Phase {
			switch nodePool.Status.Phase {
			case nodepoolv1.NodePoolPhaseReady:
				if r.Recorder != nil {
					r.Recorder.Event(&nodePool, corev1.EventTypeNormal, "Ready",
						nodePool.Status.Message)
				}
			case nodepoolv1.NodePoolPhaseDegraded:
				if r.Recorder != nil {
					r.Recorder.Event(&nodePool, corev1.EventTypeWarning, "Degraded",
						nodePool.Status.Message)
				}
			case nodepoolv1.NodePoolPhasePending:
				if r.Recorder != nil {
					r.Recorder.Event(&nodePool, corev1.EventTypeNormal, "Pending",
						nodePool.Status.Message)
				}
			}
		}

		if err := r.Status().Patch(ctx, &nodePool, client.MergeFrom(original)); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodePoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&nodepoolv1.NodePool{}).
		Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.enqueueAllNodePools),
			builder.WithPredicates(r.nodePredicate()),
		).
		Named("nodepool").
		Complete(r)
}

func (r *NodePoolReconciler) enqueueAllNodePools(
	ctx context.Context,
	obj client.Object,
) []ctrl.Request {

	node, ok := obj.(*corev1.Node)
	if ok {
		// Ignore control-plane / master nodes
		if _, exists := node.Labels["node-role.kubernetes.io/control-plane"]; exists {
			return nil
		}
		if _, exists := node.Labels["node-role.kubernetes.io/master"]; exists {
			return nil
		}
	}

	var pools nodepoolv1.NodePoolList
	if err := r.List(ctx, &pools); err != nil {
		return nil
	}

	reqs := make([]ctrl.Request, 0, len(pools.Items))
	for _, p := range pools.Items {
		reqs = append(reqs, ctrl.Request{
			NamespacedName: client.ObjectKey{
				Name:      p.Name,
				Namespace: p.Namespace,
			},
		})
	}
	return reqs
}
