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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nodepoolv1 "github.com/PMuku/nodepool-operator/api/v1"
)

var _ = Describe("NodePool Controller", func() {

	// Helper function to create a test node
	// Parameters:
	//   - name: unique node name
	//   - labels: node labels (including pool assignments if any)
	//   - ready: whether the node should have Ready=True condition
	//   - cordoned: whether the node is unschedulable
	createTestNode := func(ctx context.Context, name string, labels map[string]string, ready bool, cordoned bool) *corev1.Node {
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: labels,
			},
			Spec: corev1.NodeSpec{
				Unschedulable: cordoned,
			},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		}
		if !ready {
			node.Status.Conditions[0].Status = corev1.ConditionFalse
		}
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
		return node
	}

	// Helper function to delete a test node
	// Ignores NotFound errors for cleanup convenience
	deleteTestNode := func(ctx context.Context, name string) {
		node := &corev1.Node{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, node)
		if err == nil {
			Expect(k8sClient.Delete(ctx, node)).To(Succeed())
		}
	}

	// Helper function to create a NodePool
	// Parameters:
	//   - name, namespace: NodePool identifiers
	//   - size: desired number of nodes in pool
	//   - label: label to apply to nodes (format: key=value)
	//   - selector: NodeSelector for filtering eligible nodes
	createNodePool := func(ctx context.Context, name, namespace string, size int32, label string, selector map[string]string) *nodepoolv1.NodePool {
		nodePool := &nodepoolv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: nodepoolv1.NodePoolSpec{
				Size:         size,
				Label:        label,
				NodeSelector: selector,
			},
		}
		Expect(k8sClient.Create(ctx, nodePool)).To(Succeed())
		return nodePool
	}

	// =========================================================================
	// Test: Finalizer Management
	// =========================================================================
	// Finalizers ensure cleanup happens before resource deletion.
	// The controller adds a finalizer on first reconcile to:
	// 1. Prevent immediate deletion
	// 2. Allow cleanup of node labels/taints before NodePool is removed

	Context("Finalizer Management", func() {
		const (
			nodePoolName      = "test-finalizer-pool"
			nodePoolNamespace = "default"
		)

		ctx := context.Background()

		AfterEach(func() {
			// Cleanup NodePool after each test
			nodePool := &nodepoolv1.NodePool{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      nodePoolName,
				Namespace: nodePoolNamespace,
			}, nodePool)
			if err == nil {
				// Remove finalizer to allow deletion
				nodePool.Finalizers = nil
				_ = k8sClient.Update(ctx, nodePool)
				_ = k8sClient.Delete(ctx, nodePool)
			}
		})

		It("should add finalizer on first reconcile", func() {
			By("Creating a NodePool without finalizer")
			_ = createNodePool(ctx, nodePoolName, nodePoolNamespace, 1, "pool=test", nil)

			By("Running reconciliation")
			reconciler := &NodePoolReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile should add the finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      nodePoolName,
					Namespace: nodePoolNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying finalizer was added")
			updatedPool := &nodepoolv1.NodePool{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      nodePoolName,
				Namespace: nodePoolNamespace,
			}, updatedPool)).To(Succeed())

			// The finalizer should be present
			Expect(updatedPool.Finalizers).To(ContainElement("nodepool.k8s.local/finalizer"))
		})
	})

	// =========================================================================
	// Test: Scale-Up Scenarios
	// =========================================================================
	// Scale-up tests verify that the controller correctly assigns eligible
	// nodes to a NodePool when currentSize < desiredSize.
	// Key behaviors:
	// - Only Ready, schedulable nodes are assigned
	// - Nodes must match NodeSelector (if specified)
	// - Cordoned nodes are NOT assigned during scale-up

	Context("Scale-Up Scenarios", func() {
		const (
			nodePoolName      = "test-scaleup-pool"
			nodePoolNamespace = "default"
		)

		ctx := context.Background()

		AfterEach(func() {
			// Cleanup NodePool
			nodePool := &nodepoolv1.NodePool{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      nodePoolName,
				Namespace: nodePoolNamespace,
			}, nodePool); err == nil {
				nodePool.Finalizers = nil
				_ = k8sClient.Update(ctx, nodePool)
				_ = k8sClient.Delete(ctx, nodePool)
			}
			// Cleanup test nodes
			deleteTestNode(ctx, "scaleup-node-1")
			deleteTestNode(ctx, "scaleup-node-2")
		})

		It("should assign eligible nodes to reach desired size", func() {
			By("Creating eligible worker nodes")
			// Both nodes: Ready=true, Cordoned=false, matching selector
			createTestNode(ctx, "scaleup-node-1", map[string]string{"zone": "test"}, true, false)
			createTestNode(ctx, "scaleup-node-2", map[string]string{"zone": "test"}, true, false)

			By("Creating a NodePool with size 2")
			_ = createNodePool(ctx, nodePoolName, nodePoolNamespace, 2, "pool=scaleup", map[string]string{"zone": "test"})

			By("Running reconciliation multiple times")
			reconciler := &NodePoolReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile: adds finalizer, requests requeue
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      nodePoolName,
					Namespace: nodePoolNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile: assigns nodes
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      nodePoolName,
					Namespace: nodePoolNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying nodes are labeled with pool membership")
			node1 := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "scaleup-node-1"}, node1)).To(Succeed())
			// Check pool ownership label
			Expect(node1.Labels).To(HaveKeyWithValue("nodepool.k8s.local/name", nodePoolName))
			// Check user-specified label from nodePool.Spec.Label
			Expect(node1.Labels).To(HaveKeyWithValue("pool", "scaleup"))
		})

		It("should not assign cordoned nodes", func() {
			By("Creating one ready node and one cordoned node")
			createTestNode(ctx, "scaleup-node-1", map[string]string{"zone": "test"}, true, false) // Ready, schedulable
			createTestNode(ctx, "scaleup-node-2", map[string]string{"zone": "test"}, true, true)  // Ready, but cordoned

			By("Creating a NodePool with size 2")
			_ = createNodePool(ctx, nodePoolName, nodePoolNamespace, 2, "pool=scaleup", map[string]string{"zone": "test"})

			By("Running reconciliation")
			reconciler := &NodePoolReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Run multiple reconciles to ensure stable state
			for i := 0; i < 3; i++ {
				_, _ = reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      nodePoolName,
						Namespace: nodePoolNamespace,
					},
				})
			}

			By("Verifying only the ready, schedulable node was assigned")
			node1 := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "scaleup-node-1"}, node1)).To(Succeed())
			Expect(node1.Labels).To(HaveKey("nodepool.k8s.local/name"))

			// Cordoned node should NOT be assigned
			node2 := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "scaleup-node-2"}, node2)).To(Succeed())
			Expect(node2.Labels).NotTo(HaveKey("nodepool.k8s.local/name"))
		})
	})

	// =========================================================================
	// Test: Scale-Down Scenarios
	// =========================================================================
	// Scale-down tests verify that the controller correctly releases nodes
	// when currentSize > desiredSize.
	// Key behaviors:
	// - Only cordoned/maintenance nodes are released (safe release)
	// - Non-cordoned nodes are NOT released automatically
	// - Labels and taints are removed from released nodes

	Context("Scale-Down Scenarios", func() {
		const (
			nodePoolName      = "test-scaledown-pool"
			nodePoolNamespace = "default"
		)

		ctx := context.Background()

		AfterEach(func() {
			// Cleanup NodePool
			nodePool := &nodepoolv1.NodePool{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      nodePoolName,
				Namespace: nodePoolNamespace,
			}, nodePool); err == nil {
				nodePool.Finalizers = nil
				_ = k8sClient.Update(ctx, nodePool)
				_ = k8sClient.Delete(ctx, nodePool)
			}
			// Cleanup test nodes
			deleteTestNode(ctx, "scaledown-node-1")
			deleteTestNode(ctx, "scaledown-node-2")
		})

		It("should release cordoned nodes when scaling down", func() {
			By("Creating nodes that are already assigned to the pool")
			// Node 1: Active (not cordoned) - should NOT be released
			_ = createTestNode(ctx, "scaledown-node-1", map[string]string{
				"zone":                    "test",
				"nodepool.k8s.local/name": nodePoolName,
				"pool":                    "scaledown",
			}, true, false)

			// Node 2: Cordoned - safe to release
			_ = createTestNode(ctx, "scaledown-node-2", map[string]string{
				"zone":                    "test",
				"nodepool.k8s.local/name": nodePoolName,
				"pool":                    "scaledown",
			}, true, true) // Cordoned

			By("Creating NodePool with size 1 (smaller than current 2 assigned)")
			_ = createNodePool(ctx, nodePoolName, nodePoolNamespace, 1, "pool=scaledown", map[string]string{"zone": "test"})

			By("Running reconciliation")
			reconciler := &NodePoolReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			for i := 0; i < 3; i++ {
				_, _ = reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      nodePoolName,
						Namespace: nodePoolNamespace,
					},
				})
			}

			By("Verifying cordoned node was released (labels removed)")
			updatedNode2 := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "scaledown-node-2"}, updatedNode2)).To(Succeed())
			Expect(updatedNode2.Labels).NotTo(HaveKey("nodepool.k8s.local/name"))

			By("Verifying non-cordoned node is still assigned")
			updatedNode1 := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "scaledown-node-1"}, updatedNode1)).To(Succeed())
			Expect(updatedNode1.Labels).To(HaveKey("nodepool.k8s.local/name"))
		})
	})

	// =========================================================================
	// Test: Status Phase Transitions
	// =========================================================================
	// Status tests verify the controller correctly sets NodePool.Status.Phase:
	// - Ready: currentSize == desiredSize (all nodes healthy)
	// - Pending: scaling in progress
	// - Degraded: cannot reach desired size (no eligible nodes)

	Context("Status Phase Transitions", func() {
		const (
			nodePoolName      = "test-status-pool"
			nodePoolNamespace = "default"
		)

		ctx := context.Background()

		AfterEach(func() {
			nodePool := &nodepoolv1.NodePool{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      nodePoolName,
				Namespace: nodePoolNamespace,
			}, nodePool); err == nil {
				nodePool.Finalizers = nil
				_ = k8sClient.Update(ctx, nodePool)
				_ = k8sClient.Delete(ctx, nodePool)
			}
			deleteTestNode(ctx, "status-node-1")
		})

		It("should set status to Ready when desired size is met", func() {
			By("Creating a node and NodePool with matching size")
			createTestNode(ctx, "status-node-1", map[string]string{"zone": "test"}, true, false)
			_ = createNodePool(ctx, nodePoolName, nodePoolNamespace, 1, "pool=status", map[string]string{"zone": "test"})

			By("Running reconciliation until stable")
			reconciler := &NodePoolReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Multiple reconciles to reach steady state
			for i := 0; i < 5; i++ {
				_, _ = reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      nodePoolName,
						Namespace: nodePoolNamespace,
					},
				})
			}

			By("Verifying status is Ready")
			nodePool := &nodepoolv1.NodePool{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      nodePoolName,
				Namespace: nodePoolNamespace,
			}, nodePool)).To(Succeed())

			Expect(nodePool.Status.Phase).To(Equal(nodepoolv1.NodePoolPhaseReady))
			Expect(nodePool.Status.CurrentSize).To(Equal(int32(1)))
			Expect(nodePool.Status.DesiredSize).To(Equal(int32(1)))
		})

		It("should set status to Degraded when no eligible nodes available", func() {
			By("Creating NodePool requesting nodes that don't exist")
			// NodeSelector requires zone=nonexistent, but no such nodes exist
			_ = createNodePool(ctx, nodePoolName, nodePoolNamespace, 2, "pool=status", map[string]string{"zone": "nonexistent"})

			By("Running reconciliation")
			reconciler := &NodePoolReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			for i := 0; i < 3; i++ {
				_, _ = reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      nodePoolName,
						Namespace: nodePoolNamespace,
					},
				})
			}

			By("Verifying status is Degraded")
			nodePool := &nodepoolv1.NodePool{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      nodePoolName,
				Namespace: nodePoolNamespace,
			}, nodePool)).To(Succeed())

			Expect(nodePool.Status.Phase).To(Equal(nodepoolv1.NodePoolPhaseDegraded))
		})
	})

	// =========================================================================
	// Test: Deletion and Cleanup
	// =========================================================================
	// Deletion tests verify the finalizer-based cleanup:
	// 1. When NodePool is deleted, finalizer triggers cleanup
	// 2. All assigned nodes have labels/taints removed
	// 3. Finalizer is removed, allowing K8s to delete the resource

	Context("Deletion and Cleanup", func() {
		const (
			nodePoolName      = "test-delete-pool"
			nodePoolNamespace = "default"
		)

		ctx := context.Background()

		AfterEach(func() {
			deleteTestNode(ctx, "delete-node-1")
		})

		It("should release all nodes and remove finalizer on deletion", func() {
			By("Creating a node already assigned to the pool")
			_ = createTestNode(ctx, "delete-node-1", map[string]string{
				"zone":                    "test",
				"nodepool.k8s.local/name": nodePoolName,
				"pool":                    "delete",
			}, true, false)

			By("Creating NodePool")
			nodePool := createNodePool(ctx, nodePoolName, nodePoolNamespace, 1, "pool=delete", map[string]string{"zone": "test"})

			By("Adding finalizer via reconciliation")
			reconciler := &NodePoolReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      nodePoolName,
					Namespace: nodePoolNamespace,
				},
			})

			By("Deleting the NodePool")
			Expect(k8sClient.Delete(ctx, nodePool)).To(Succeed())

			By("Running reconciliation to trigger cleanup")
			// This reconcile sees DeletionTimestamp set, triggers cleanup
			_, _ = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      nodePoolName,
					Namespace: nodePoolNamespace,
				},
			})

			By("Verifying node labels were removed")
			node := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "delete-node-1"}, node)).To(Succeed())
			Expect(node.Labels).NotTo(HaveKey("nodepool.k8s.local/name"))

			By("Verifying NodePool was fully deleted")
			deletedPool := &nodepoolv1.NodePool{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      nodePoolName,
				Namespace: nodePoolNamespace,
			}, deletedPool)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	// =========================================================================
	// Test: Node Taints
	// =========================================================================
	// Taint tests verify that nodes get appropriate taints when assigned:
	// - If nodePool.Spec.Taint is set: use that taint
	// - If not set: derive taint from nodePool.Spec.Label with NoSchedule effect

	Context("Node Taints", func() {
		const (
			nodePoolName      = "test-taint-pool"
			nodePoolNamespace = "default"
		)

		ctx := context.Background()

		AfterEach(func() {
			nodePool := &nodepoolv1.NodePool{}
			if err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      nodePoolName,
				Namespace: nodePoolNamespace,
			}, nodePool); err == nil {
				nodePool.Finalizers = nil
				_ = k8sClient.Update(ctx, nodePool)
				_ = k8sClient.Delete(ctx, nodePool)
			}
			deleteTestNode(ctx, "taint-node-1")
		})

		It("should apply default taint derived from label when no taint specified", func() {
			By("Creating a node")
			createTestNode(ctx, "taint-node-1", map[string]string{"zone": "test"}, true, false)

			By("Creating NodePool without custom taint (will use label-derived taint)")
			// Label is "pool=taint", so default taint will be pool=taint:NoSchedule
			_ = createNodePool(ctx, nodePoolName, nodePoolNamespace, 1, "pool=taint", map[string]string{"zone": "test"})

			By("Running reconciliation")
			reconciler := &NodePoolReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			for i := 0; i < 3; i++ {
				_, _ = reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      nodePoolName,
						Namespace: nodePoolNamespace,
					},
				})
			}

			By("Verifying default taint was applied")
			node := &corev1.Node{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "taint-node-1"}, node)).To(Succeed())

			// Look for the default taint: pool=taint:NoSchedule
			hasTaint := false
			for _, t := range node.Spec.Taints {
				if t.Key == "pool" && t.Value == "taint" && t.Effect == corev1.TaintEffectNoSchedule {
					hasTaint = true
					break
				}
			}
			Expect(hasTaint).To(BeTrue(), "Expected default taint pool=taint:NoSchedule to be applied")
		})
	})

	// =========================================================================
	// Test: Basic Reconciliation (Original Test - Kept for Backward Compatibility)
	// =========================================================================

	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		nodepool := &nodepoolv1.NodePool{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind NodePool")
			err := k8sClient.Get(ctx, typeNamespacedName, nodepool)
			if err != nil && errors.IsNotFound(err) {
				resource := &nodepoolv1.NodePool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: nodepoolv1.NodePoolSpec{
						Size:  1,
						Label: "pool=test",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &nodepoolv1.NodePool{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				// Remove finalizer before deletion
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)

				By("Cleanup the specific resource instance NodePool")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &NodePoolReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
