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

	// Handle deletion
	if nodePool.DeletionTimestamp != nil {
		return r.reconcileDeletionHelper(ctx, &nodePool)
	}

	if !controllerutil.ContainsFinalizer(&nodePool, finalizerName) {
		controllerutil.AddFinalizer(&nodePool, finalizerName)
		return ctrl.Result{Requeue: true}, r.Update(ctx, &nodePool)
	}

	// get current cluster state
	currPoolState, err := r.getClusterState(ctx, &nodePool)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Log nodepool state
	log.Info("NodePool reconciliation state",
		"nodepool", nodePool.Name,
		"desiredSize", nodePool.Spec.Size,
		"assigned", len(currPoolState.assigned),
		"usableAssigned", len(currPoolState.usableAssigned),
		"eligibleUnassigned", len(currPoolState.eligibleUnassigned),
		"safeToRelease", len(currPoolState.safeToRelease),
	)

	// Scaling logic - continue if NO scaling
	scaled, result, err := r.scalingReconcile(ctx, &nodePool, currPoolState)
	if scaled {
		return result, err
	}

	// Status update
	return r.reconcileStatus(ctx, &nodePool, currPoolState)
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
