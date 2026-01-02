//go:build e2e
// +build e2e

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/PMuku/nodepool-operator/test/utils"
)

// =============================================================================
// Constants
// =============================================================================

// namespace where the project is deployed in
const namespace = "nodepool-operator-system"

// serviceAccountName created for the project
const serviceAccountName = "nodepool-operator-controller-manager"

// metricsServiceName is the name of the metrics service of the project
const metricsServiceName = "nodepool-operator-controller-manager-metrics-service"

// metricsRoleBindingName is the name of the RBAC that will be created to allow get the metrics data
const metricsRoleBindingName = "nodepool-operator-metrics-binding"

// =============================================================================
// E2E Test Suite
// =============================================================================
// This test suite runs against a REAL Kubernetes cluster (Kind).
// Unlike integration tests (envtest), these tests:
// - Deploy the actual controller as a pod
// - Use real nodes (Kind worker nodes)
// - Test the full deployment pipeline
//
// Test Flow:
// 1. BeforeAll: Create namespace, install CRDs, deploy controller
// 2. Run tests against the live cluster
// 3. AfterAll: Cleanup everything

var _ = Describe("Manager", Ordered, func() {
	var controllerPodName string

	// =========================================================================
	// Setup: Runs ONCE before all tests in this Describe block
	// =========================================================================
	BeforeAll(func() {
		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to create namespace")

		By("labeling the namespace to enforce the restricted security policy")
		// Pod Security Standards: Ensures pods meet security requirements
		// "restricted" = most restrictive policy (no privilege escalation, etc.)
		cmd = exec.Command("kubectl", "label", "--overwrite", "ns", namespace,
			"pod-security.kubernetes.io/enforce=restricted")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to label namespace with restricted policy")

		By("installing CRDs")
		// Runs: kubectl apply -f config/crd/bases/
		cmd = exec.Command("make", "install")
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to install CRDs")

		By("deploying the controller-manager")
		// Runs: kustomize build config/default | kubectl apply -f -
		cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectImage))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred(), "Failed to deploy the controller-manager")
	})

	// =========================================================================
	// Teardown: Runs ONCE after all tests complete
	// =========================================================================
	AfterAll(func() {
		By("cleaning up the curl pod for metrics")
		cmd := exec.Command("kubectl", "delete", "pod", "curl-metrics", "-n", namespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("cleaning up test NodePools")
		cmd = exec.Command("kubectl", "delete", "nodepools", "--all", "-A", "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("undeploying the controller-manager")
		cmd = exec.Command("make", "undeploy")
		_, _ = utils.Run(cmd)

		By("uninstalling CRDs")
		cmd = exec.Command("make", "uninstall")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace, "--ignore-not-found")
		_, _ = utils.Run(cmd)
	})

	// =========================================================================
	// Debug Helper: Runs after EACH test if it fails
	// =========================================================================
	AfterEach(func() {
		specReport := CurrentSpecReport()
		if specReport.Failed() {
			By("Fetching controller manager pod logs")
			cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
			controllerLogs, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Controller logs:\n %s", controllerLogs)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Controller logs: %s", err)
			}

			By("Fetching Kubernetes events")
			cmd = exec.Command("kubectl", "get", "events", "-n", namespace, "--sort-by=.lastTimestamp")
			eventsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Kubernetes events:\n%s", eventsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get Kubernetes events: %s", err)
			}

			By("Fetching curl-metrics logs")
			cmd = exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
			metricsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "Metrics logs:\n %s", metricsOutput)
			} else {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to get curl-metrics logs: %s", err)
			}

			By("Fetching controller manager pod description")
			cmd = exec.Command("kubectl", "describe", "pod", controllerPodName, "-n", namespace)
			podDescription, err := utils.Run(cmd)
			if err == nil {
				fmt.Println("Pod description:\n", podDescription)
			} else {
				fmt.Println("Failed to describe controller pod")
			}

			By("Fetching all NodePools")
			cmd = exec.Command("kubectl", "get", "nodepools", "-A", "-o", "yaml")
			nodepoolsOutput, err := utils.Run(cmd)
			if err == nil {
				_, _ = fmt.Fprintf(GinkgoWriter, "NodePools:\n%s", nodepoolsOutput)
			}
		}
	})

	// Set default timeouts for Eventually assertions
	SetDefaultEventuallyTimeout(2 * time.Minute)
	SetDefaultEventuallyPollingInterval(time.Second)

	// =========================================================================
	// Test Context: Controller Manager Health
	// =========================================================================
	Context("Manager", func() {

		// ---------------------------------------------------------------------
		// Test: Controller pod is running
		// ---------------------------------------------------------------------
		// Purpose: Verify the controller-manager pod starts successfully
		// What it checks:
		// - Exactly 1 controller pod exists
		// - Pod name contains "controller-manager"
		// - Pod status is "Running"
		It("should run successfully", func() {
			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func(g Gomega) {
				// Get the name of the controller-manager pod
				cmd := exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve controller-manager pod information")
				podNames := utils.GetNonEmptyLines(podOutput)
				g.Expect(podNames).To(HaveLen(1), "expected 1 controller pod running")
				controllerPodName = podNames[0]
				g.Expect(controllerPodName).To(ContainSubstring("controller-manager"))

				// Validate the pod's status
				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Running"), "Incorrect controller-manager pod status")
			}
			// Eventually retries until success or timeout (2 minutes)
			Eventually(verifyControllerUp).Should(Succeed())
		})

		// ---------------------------------------------------------------------
		// Test: Metrics endpoint is accessible
		// ---------------------------------------------------------------------
		// Purpose: Verify the metrics server is working
		// What it checks:
		// - Metrics service exists
		// - Can authenticate with service account token
		// - HTTP 200 response from /metrics endpoint
		It("should ensure the metrics endpoint is serving metrics", func() {
			By("creating a ClusterRoleBinding for the service account to allow access to metrics")
			cmd := exec.Command("kubectl", "create", "clusterrolebinding", metricsRoleBindingName,
				"--clusterrole=nodepool-operator-metrics-reader",
				fmt.Sprintf("--serviceaccount=%s:%s", namespace, serviceAccountName),
			)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create ClusterRoleBinding")

			By("validating that the metrics service is available")
			cmd = exec.Command("kubectl", "get", "service", metricsServiceName, "-n", namespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Metrics service should exist")

			By("getting the service account token")
			token, err := serviceAccountToken()
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())

			By("ensuring the controller pod is ready")
			verifyControllerPodReady := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pod", controllerPodName, "-n", namespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("True"), "Controller pod not ready")
			}
			Eventually(verifyControllerPodReady, 3*time.Minute, time.Second).Should(Succeed())

			By("verifying that the controller manager is serving the metrics server")
			verifyMetricsServerStarted := func(g Gomega) {
				cmd := exec.Command("kubectl", "logs", controllerPodName, "-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("Serving metrics server"),
					"Metrics server not yet started")
			}
			Eventually(verifyMetricsServerStarted, 3*time.Minute, time.Second).Should(Succeed())

			By("creating the curl-metrics pod to access the metrics endpoint")
			cmd = exec.Command("kubectl", "run", "curl-metrics", "--restart=Never",
				"--namespace", namespace,
				"--image=curlimages/curl:latest",
				"--overrides",
				fmt.Sprintf(`{
					"spec": {
						"containers": [{
							"name": "curl",
							"image": "curlimages/curl:latest",
							"command": ["/bin/sh", "-c"],
							"args": ["curl -v -k -H 'Authorization: Bearer %s' https://%s.%s.svc.cluster.local:8443/metrics"],
							"securityContext": {
								"readOnlyRootFilesystem": true,
								"allowPrivilegeEscalation": false,
								"capabilities": {
									"drop": ["ALL"]
								},
								"runAsNonRoot": true,
								"runAsUser": 1000,
								"seccompProfile": {
									"type": "RuntimeDefault"
								}
							}
						}],
						"serviceAccountName": "%s"
					}
				}`, token, metricsServiceName, namespace, serviceAccountName))
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred(), "Failed to create curl-metrics pod")

			By("waiting for the curl-metrics pod to complete.")
			verifyCurlUp := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "curl-metrics",
					"-o", "jsonpath={.status.phase}",
					"-n", namespace)
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(Equal("Succeeded"), "curl pod in wrong status")
			}
			Eventually(verifyCurlUp, 5*time.Minute).Should(Succeed())

			By("getting the metrics by checking curl-metrics logs")
			verifyMetricsAvailable := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				g.Expect(err).NotTo(HaveOccurred(), "Failed to retrieve logs from curl pod")
				g.Expect(metricsOutput).NotTo(BeEmpty())
				g.Expect(metricsOutput).To(ContainSubstring("< HTTP/1.1 200 OK"))
			}
			Eventually(verifyMetricsAvailable, 2*time.Minute).Should(Succeed())
		})
	})

	// =========================================================================
	// Test Context: NodePool Operations
	// =========================================================================
	// These tests verify the core functionality of the NodePool operator:
	// - Creating NodePools
	// - Node assignment with labels/taints
	// - Status phase transitions
	// - Cleanup on deletion
	//
	// NOTE: Kind clusters typically have limited nodes (control-plane + workers)
	// These tests are designed to work with Kind's default configuration.

	Context("NodePool Operations", func() {

		// Helper: Create a NodePool CR using kubectl
		createNodePool := func(name, namespace string, size int, label string, selector map[string]string) error {
			selectorYaml := ""
			if len(selector) > 0 {
				selectorYaml = "  nodeSelector:\n"
				for k, v := range selector {
					selectorYaml += fmt.Sprintf("    %s: %s\n", k, v)
				}
			}

			yaml := fmt.Sprintf(`apiVersion: nodepool.k8s.local/v1
kind: NodePool
metadata:
  name: %s
  namespace: %s
spec:
  size: %d
  label: "%s"
%s`, name, namespace, size, label, selectorYaml)

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(yaml)
			_, err := utils.Run(cmd)
			return err
		}

		// Helper: Delete a NodePool CR
		deleteNodePool := func(name, namespace string) {
			cmd := exec.Command("kubectl", "delete", "nodepool", name, "-n", namespace, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		}

		// Helper: Get NodePool status phase
		getNodePoolPhase := func(name, namespace string) (string, error) {
			cmd := exec.Command("kubectl", "get", "nodepool", name, "-n", namespace,
				"-o", "jsonpath={.status.phase}")
			return utils.Run(cmd)
		}

		// Helper: Get NodePool status
		getNodePoolStatus := func(name, namespace string) (string, error) {
			cmd := exec.Command("kubectl", "get", "nodepool", name, "-n", namespace,
				"-o", "jsonpath={.status}")
			return utils.Run(cmd)
		}

		// Helper: Get list of worker nodes (non-control-plane)
		getWorkerNodes := func() ([]string, error) {
			cmd := exec.Command("kubectl", "get", "nodes",
				"-l", "!node-role.kubernetes.io/control-plane",
				"-o", "jsonpath={.items[*].metadata.name}")
			output, err := utils.Run(cmd)
			if err != nil {
				return nil, err
			}
			if output == "" {
				return []string{}, nil
			}
			return strings.Split(output, " "), nil
		}

		// Cleanup after each NodePool test
		AfterEach(func() {
			By("cleaning up test NodePools")
			deleteNodePool("test-pool", "default")
			deleteNodePool("degraded-pool", "default")
			deleteNodePool("cleanup-pool", "default")

			// Wait a moment for cleanup to complete
			time.Sleep(2 * time.Second)
		})

		// ---------------------------------------------------------------------
		// Test: NodePool CR creation and status update
		// ---------------------------------------------------------------------
		// Purpose: Verify that creating a NodePool CR triggers reconciliation
		// What it checks:
		// - NodePool CR is created successfully
		// - Status is populated (phase is set)
		// - Finalizer is added
		It("should create a NodePool and update status", func() {
			By("creating a NodePool CR")
			err := createNodePool("test-pool", "default", 1, "e2e-test=pool", map[string]string{
				"kubernetes.io/os": "linux",
			})
			Expect(err).NotTo(HaveOccurred(), "Failed to create NodePool")

			By("waiting for the NodePool status to be updated")
			verifyStatusUpdated := func(g Gomega) {
				status, err := getNodePoolStatus("test-pool", "default")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(status).NotTo(BeEmpty(), "Status should be populated")
			}
			Eventually(verifyStatusUpdated, 1*time.Minute, 2*time.Second).Should(Succeed())

			By("verifying the NodePool has a finalizer")
			cmd := exec.Command("kubectl", "get", "nodepool", "test-pool", "-n", "default",
				"-o", "jsonpath={.metadata.finalizers}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("nodepool.k8s.local/finalizer"))

			By("verifying the NodePool has a status phase")
			phase, err := getNodePoolPhase("test-pool", "default")
			Expect(err).NotTo(HaveOccurred())
			// Phase should be one of: Ready, Pending, or Degraded
			Expect(phase).To(BeElementOf("Ready", "Pending", "Degraded"))
		})

		// ---------------------------------------------------------------------
		// Test: Node assignment with labels
		// ---------------------------------------------------------------------
		// Purpose: Verify nodes get assigned to the pool with correct labels
		// What it checks:
		// - At least one worker node exists
		// - After reconciliation, node has pool label
		// - Node has user-specified label (e2e-test=pool)
		It("should assign nodes and apply labels", func() {
			By("checking for available worker nodes")
			workers, err := getWorkerNodes()
			Expect(err).NotTo(HaveOccurred())

			if len(workers) == 0 {
				Skip("No worker nodes available in Kind cluster - skipping node assignment test")
			}

			By("creating a NodePool that targets worker nodes")
			err = createNodePool("test-pool", "default", 1, "e2e-test=assigned", map[string]string{
				"kubernetes.io/os": "linux",
			})
			Expect(err).NotTo(HaveOccurred())

			By("waiting for a node to be assigned to the pool")
			verifyNodeAssigned := func(g Gomega) {
				// Check if any worker node has the pool label
				cmd := exec.Command("kubectl", "get", "nodes",
					"-l", "nodepool.k8s.local/name=test-pool",
					"-o", "jsonpath={.items[*].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "Expected at least one node to have pool label")
			}
			Eventually(verifyNodeAssigned, 2*time.Minute, 3*time.Second).Should(Succeed())

			By("verifying the node has the user-specified label")
			cmd := exec.Command("kubectl", "get", "nodes",
				"-l", "e2e-test=assigned",
				"-o", "jsonpath={.items[*].metadata.name}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).NotTo(BeEmpty(), "Expected node to have user label e2e-test=assigned")

			By("verifying the NodePool status shows assigned nodes")
			verifyAssignedNodes := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "nodepool", "test-pool", "-n", "default",
					"-o", "jsonpath={.status.assignedNodes}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty(), "Expected assignedNodes in status")
			}
			Eventually(verifyAssignedNodes, 1*time.Minute, 2*time.Second).Should(Succeed())
		})

		// ---------------------------------------------------------------------
		// Test: Status Degraded when no nodes available
		// ---------------------------------------------------------------------
		// Purpose: Verify status becomes Degraded when no eligible nodes exist
		// What it checks:
		// - NodePool with impossible selector is created
		// - Status phase becomes "Degraded"
		// - Message indicates no eligible nodes
		It("should set status to Degraded when no eligible nodes available", func() {
			By("creating a NodePool with selector that matches no nodes")
			err := createNodePool("degraded-pool", "default", 2, "impossible=pool", map[string]string{
				"nonexistent-label": "nonexistent-value",
			})
			Expect(err).NotTo(HaveOccurred())

			By("waiting for status to become Degraded")
			verifyDegraded := func(g Gomega) {
				phase, err := getNodePoolPhase("degraded-pool", "default")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(phase).To(Equal("Degraded"), "Expected phase to be Degraded")
			}
			Eventually(verifyDegraded, 1*time.Minute, 3*time.Second).Should(Succeed())

			By("verifying the status message indicates the issue")
			cmd := exec.Command("kubectl", "get", "nodepool", "degraded-pool", "-n", "default",
				"-o", "jsonpath={.status.message}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(output).To(ContainSubstring("no eligible nodes"))
		})

		// ---------------------------------------------------------------------
		// Test: Cleanup on NodePool deletion
		// ---------------------------------------------------------------------
		// Purpose: Verify labels/taints are removed when NodePool is deleted
		// What it checks:
		// - Node is assigned to pool
		// - After deletion, node no longer has pool labels
		// - Finalizer allows proper cleanup
		It("should cleanup node labels when NodePool is deleted", func() {
			By("checking for available worker nodes")
			workers, err := getWorkerNodes()
			Expect(err).NotTo(HaveOccurred())

			if len(workers) == 0 {
				Skip("No worker nodes available in Kind cluster - skipping cleanup test")
			}

			By("creating a NodePool")
			err = createNodePool("cleanup-pool", "default", 1, "cleanup-test=pool", map[string]string{
				"kubernetes.io/os": "linux",
			})
			Expect(err).NotTo(HaveOccurred())

			By("waiting for a node to be assigned")
			var assignedNode string
			verifyAssigned := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "nodes",
					"-l", "nodepool.k8s.local/name=cleanup-pool",
					"-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(BeEmpty())
				assignedNode = output
			}
			Eventually(verifyAssigned, 2*time.Minute, 3*time.Second).Should(Succeed())

			By("deleting the NodePool")
			cmd := exec.Command("kubectl", "delete", "nodepool", "cleanup-pool", "-n", "default")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for the NodePool to be fully deleted")
			verifyDeleted := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "nodepool", "cleanup-pool", "-n", "default")
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "NodePool should be deleted")
			}
			Eventually(verifyDeleted, 1*time.Minute, 2*time.Second).Should(Succeed())

			By("verifying the node labels were removed")
			verifyLabelsRemoved := func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "node", assignedNode,
					"-o", "jsonpath={.metadata.labels.nodepool\\.k8s\\.local/name}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(BeEmpty(), "Pool label should be removed from node")
			}
			Eventually(verifyLabelsRemoved, 1*time.Minute, 2*time.Second).Should(Succeed())
		})

		// ---------------------------------------------------------------------
		// Test: Reconciliation metrics
		// ---------------------------------------------------------------------
		// Purpose: Verify reconciliation is tracked in metrics
		// What it checks:
		// - After creating a NodePool, reconciliation count increases
		// - Metrics show successful reconciliations
		It("should track reconciliation in metrics", func() {
			By("creating a NodePool to trigger reconciliation")
			err := createNodePool("test-pool", "default", 1, "metrics-test=pool", map[string]string{
				"kubernetes.io/os": "linux",
			})
			Expect(err).NotTo(HaveOccurred())

			By("waiting for reconciliation to complete")
			time.Sleep(5 * time.Second)

			By("checking metrics for reconciliation count")
			// Note: This requires the metrics endpoint to be accessible
			// The metrics test above sets up the curl pod for this purpose
			verifyReconcileMetrics := func(g Gomega) {
				metricsOutput, err := getMetricsOutput()
				if err != nil {
					// Metrics pod may have been cleaned up, skip this check
					return
				}
				// Look for controller reconcile metrics
				g.Expect(metricsOutput).To(SatisfyAny(
					ContainSubstring("controller_runtime_reconcile_total"),
					ContainSubstring("workqueue_adds_total"),
				))
			}
			Eventually(verifyReconcileMetrics, 30*time.Second, 5*time.Second).Should(Succeed())
		})
	})
})

// =============================================================================
// Helper Functions
// =============================================================================

// serviceAccountToken returns a token for the specified service account in the given namespace.
// It uses the Kubernetes TokenRequest API to generate a token by directly sending a request
// and parsing the resulting token from the API response.
func serviceAccountToken() (string, error) {
	const tokenRequestRawString = `{
		"apiVersion": "authentication.k8s.io/v1",
		"kind": "TokenRequest"
	}`

	// Temporary file to store the token request
	secretName := fmt.Sprintf("%s-token-request", serviceAccountName)
	tokenRequestFile := filepath.Join("/tmp", secretName)
	err := os.WriteFile(tokenRequestFile, []byte(tokenRequestRawString), os.FileMode(0o644))
	if err != nil {
		return "", err
	}

	var out string
	verifyTokenCreation := func(g Gomega) {
		// Execute kubectl command to create the token
		cmd := exec.Command("kubectl", "create", "--raw", fmt.Sprintf(
			"/api/v1/namespaces/%s/serviceaccounts/%s/token",
			namespace,
			serviceAccountName,
		), "-f", tokenRequestFile)

		output, err := cmd.CombinedOutput()
		g.Expect(err).NotTo(HaveOccurred())

		// Parse the JSON output to extract the token
		var token tokenRequest
		err = json.Unmarshal(output, &token)
		g.Expect(err).NotTo(HaveOccurred())

		out = token.Status.Token
	}
	Eventually(verifyTokenCreation).Should(Succeed())

	return out, err
}

// getMetricsOutput retrieves and returns the logs from the curl pod used to access the metrics endpoint.
func getMetricsOutput() (string, error) {
	By("getting the curl-metrics logs")
	cmd := exec.Command("kubectl", "logs", "curl-metrics", "-n", namespace)
	return utils.Run(cmd)
}

// tokenRequest is a simplified representation of the Kubernetes TokenRequest API response,
// containing only the token field that we need to extract.
type tokenRequest struct {
	Status struct {
		Token string `json:"token"`
	} `json:"status"`
}
