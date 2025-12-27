package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	corev1 "k8s.io/api/core/v1"
)

/*
Reconcile on specific node state changes
Checking only for cordoning and Ready since labels and taints are handled in reconcile
*/
func (r *NodePoolReconciler) nodePredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode, ok1 := e.ObjectOld.(*corev1.Node)
			newNode, ok2 := e.ObjectNew.(*corev1.Node)
			if !ok1 || !ok2 {
				return true
			}

			if oldNode.Spec.Unschedulable != newNode.Spec.Unschedulable {
				return true
			}

			if nodeReadyChanged(oldNode, newNode) {
				return true
			}

			return false
		},
	}
}

func nodeReadyChanged(oldNode, newNode *corev1.Node) bool {
	oldReady := isReadyCondition(oldNode)
	newReady := isReadyCondition(newNode)
	return oldReady != newReady
}

func isReadyCondition(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
