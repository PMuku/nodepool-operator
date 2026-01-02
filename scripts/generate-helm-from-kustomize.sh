#!/bin/bash

# generate-helm-from-kustomize.sh
# Generates Helm chart from existing Kustomize manifests, splitting into separate files
# Usage: ./scripts/generate-helm-from-kustomize.sh

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
CHART_NAME="nodepool-operator"
CHART_DIR="charts/nodepool-operator"
CHART_VERSION="${CHART_VERSION:-0.1.0}"

IMAGE_REPO="${IMAGE_REPO:-ghcr.io/mukundparth/nodepool-operator}"
IMAGE_TAG="${IMAGE_TAG:-v0.1.0}"

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Check if required tools are installed
check_dependencies() {
    log_info "Checking dependencies..."
    
    if ! command -v kustomize >/dev/null 2>&1 && ! command -v kubectl >/dev/null 2>&1; then
        log_error "kustomize or kubectl is required but not installed."
    fi
    
    if ! command -v yq >/dev/null 2>&1; then
        log_error "yq is required but not installed. Install with: brew install yq"
    fi
    
    log_success "All dependencies available"
}

# Detect operating system
detect_os() {
    if [[ "$OSTYPE" == "darwin"* ]]; then
        OS="mac"
    else
        OS="linux"
    fi
    log_info "Detected OS: $OS"
}

# Get kustomize command
get_kustomize_cmd() {
    if command -v kustomize >/dev/null 2>&1; then
        echo "kustomize build"
    else
        echo "kubectl kustomize"
    fi
}

# Step 0: Create chart directory structure
create_chart_structure() {
    log_info "Step 0: Creating chart directory structure"
    
    rm -rf "$CHART_DIR"
    mkdir -p "$CHART_DIR/templates"
    mkdir -p "$CHART_DIR/crds"
    
    log_success "Chart directory structure created"
}

# Step 1: Create Chart.yaml
create_chart_yaml() {
    log_info "Step 1: Creating Chart.yaml"
    
    cat > "$CHART_DIR/Chart.yaml" << EOF
apiVersion: v2
name: ${CHART_NAME}
description: A Kubernetes operator for managing node pools with labels and taints
type: application
version: $CHART_VERSION
appVersion: "$IMAGE_TAG"
kubeVersion: ">=1.26.0-0"
keywords:
  - kubernetes
  - operator
  - nodepool
  - gpu
  - controller
home: https://github.com/PMuku/nodepool-operator
sources:
  - https://github.com/PMuku/nodepool-operator
maintainers:
  - name: PranavM
EOF
    
    log_success "Chart.yaml created"
}

# Step 2: Create values.yaml
create_values_yaml() {
    log_info "Step 2: Creating values.yaml"
    
    cat > "$CHART_DIR/values.yaml" << EOF
# Default values for ${CHART_NAME}
# This is a YAML-formatted file.

# Number of controller replicas
replicaCount: 1

image:
  repository: $IMAGE_REPO
  tag: "$IMAGE_TAG"
  pullPolicy: IfNotPresent

# Image pull secrets for private registries
imagePullSecrets: []

# Override the name of the chart
nameOverride: ""
fullnameOverride: ""


serviceAccount:
  create: true
  annotations: {}
  name: ""

# Leader election configuration
leaderElection:
  enabled: true

# Resource limits and requests
resources:
  limits:
    cpu: 500m
    memory: 128Mi
  requests:
    cpu: 10m
    memory: 64Mi

nodeSelector: {}
tolerations: []
affinity: {}

podSecurityContext:
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault

securityContext:
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL

healthProbes:
  livenessProbe:
    httpGet:
      path: /healthz
      port: 8081
    initialDelaySeconds: 15
    periodSeconds: 20
  readinessProbe:
    httpGet:
      path: /readyz
      port: 8081
    initialDelaySeconds: 5
    periodSeconds: 10

podAnnotations:
  kubectl.kubernetes.io/default-container: manager

podLabels: {}

metrics:
  enabled: false
  port: 8080
EOF
    
    log_success "values.yaml created"
}

# Step 3: Create _helpers.tpl
create_helpers() {
    log_info "Step 3: Creating _helpers.tpl"
    
    cat > "$CHART_DIR/templates/_helpers.tpl" << 'EOF'
{{/*
Expand the name of the chart.
*/}}
{{- define "nodepool-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "nodepool-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "nodepool-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nodepool-operator.labels" -}}
helm.sh/chart: {{ include "nodepool-operator.chart" . }}
{{ include "nodepool-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nodepool-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nodepool-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "nodepool-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "nodepool-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the namespace name
*/}}
{{- define "nodepool-operator.namespace" -}}
{{- .Release.Namespace }}
{{- end }}
EOF
    
    log_success "_helpers.tpl created"
}

# Step 4: Copy CRDs
copy_crds() {
    log_info "Step 4: Copying CRDs to chart/crds/"
    
    if ls config/crd/bases/*.yaml 1> /dev/null 2>&1; then
        cp config/crd/bases/*.yaml "$CHART_DIR/crds/"
        log_success "CRDs copied"
    else
        log_error "No CRDs found in config/crd/bases/"
    fi
}

# Step 5: Generate manifests with kustomize
generate_manifests() {
    log_info "Step 5: Generating manifests with kustomize"
    
    local kustomize_cmd=$(get_kustomize_cmd)
    local temp_file="$CHART_DIR/templates/_all_resources.yaml"
    
    $kustomize_cmd config/default > "$temp_file"
    
    if [[ -s "$temp_file" ]]; then
        log_success "Manifests generated"
    else
        log_error "Failed to generate manifests"
    fi
}

# Step 6: Split manifests into separate files by kind
split_manifests() {
    log_info "Step 6: Splitting manifests into separate files by kind"
    
    local temp_file="$CHART_DIR/templates/_all_resources.yaml"
    local templates_dir="$CHART_DIR/templates"
    
    # Count documents in the file
    local doc_count=$(yq eval-all 'documentIndex' "$temp_file" 2>/dev/null | tail -1)

    log_info "Found $((doc_count + 1)) resources in kustomize output"
    
    # Define resource kinds and their output filenames (bash 3.x compatible)
    # Format: "Kind:filename.yaml"
    local kinds="
        ServiceAccount:serviceaccount.yaml
        ClusterRole:clusterrole.yaml
        ClusterRoleBinding:clusterrolebinding.yaml
        Role:role.yaml
        RoleBinding:rolebinding.yaml
        Service:service.yaml
        Deployment:deployment.yaml
        ConfigMap:configmap.yaml
        Secret:secret.yaml
        NetworkPolicy:networkpolicy.yaml
    "

    # Extract each kind to its own file
    for pair in $kinds; do
        local kind="${pair%%:*}"
        local filename="${pair##*:}"
        local output_file="${templates_dir}/${filename}"
        
        # Extract resources of this kind
        local content=$(yq eval-all "select(.kind == \"$kind\")" "$temp_file" 2>/dev/null)
        
        if [[ -n "$content" && "$content" != "null" && "$content" != "" ]]; then
            echo "$content" > "$output_file"
            local count=$(yq eval-all "select(.kind == \"$kind\") | documentIndex" "$temp_file" 2>/dev/null | wc -l | tr -d ' ')

            log_success "  $kind -> ${filename} ($count resource(s))"
        fi
    done
    # Remove the temporary combined file
    rm -f "$temp_file"
    
    log_success "Manifests split into separate files"
}

# Step 7: Remove CRDs from templates (they're in crds/ folder)
remove_crds_from_templates() {
    log_info "Step 7: Removing CRDs from template files"
    
    for file in "$CHART_DIR/templates"/*.yaml; do
        if [[ -f "$file" ]]; then
            yq -i 'select(.kind != "CustomResourceDefinition")' "$file" 2>/dev/null || true
        fi
    done
    
    log_success "CRDs removed from templates"
}

# Step 8: Replace kustomize labels with helm labels
replace_labels() {
    log_info "Step 8: Replacing kustomize labels with Helm labels"
    
    for file in "$CHART_DIR/templates"/*.yaml; do
        if [[ -f "$file" && "$file" != *"_helpers.tpl"* && "$file" != *"NOTES.txt"* ]]; then
            if [[ "$OS" == "mac" ]]; then
                sed -i '' "s|app.kubernetes.io/managed-by: kustomize|app.kubernetes.io/managed-by: Helm|g" "$file" 2>/dev/null || true
            else
                sed -i "s|app.kubernetes.io/managed-by: kustomize|app.kubernetes.io/managed-by: Helm|g" "$file" 2>/dev/null || true
            fi
        fi
    done
    
    log_success "Labels replaced"
}

# Step 9: Template the namespace references
template_namespace() {
    log_info "Step 9: Templating namespace references"
    
    # Get the namespace from kustomize output
    local namespace_value="nodepool-operator-system"
    
    for file in "$CHART_DIR/templates"/*.yaml; do
        if [[ -f "$file" && "$file" != *"_helpers.tpl"* && "$file" != *"NOTES.txt"* ]]; then
            if [[ "$OS" == "mac" ]]; then
                sed -i '' "s|namespace: ${namespace_value}|namespace: {{ .Release.Namespace }}|g" "$file" 2>/dev/null || true
            else
                sed -i "s|namespace: ${namespace_value}|namespace: {{ .Release.Namespace }}|g" "$file" 2>/dev/null || true
            fi
        fi
    done
    
    log_success "Namespace templated"
}

# Step 10: Template image in deployment
template_image() {
    log_info "Step 10: Templating image in deployment"
    
    local deployment_file="$CHART_DIR/templates/deployment.yaml"
    
    if [[ -f "$deployment_file" ]]; then
        # Template image for the manager container
        yq -i '(select(.kind == "Deployment").spec.template.spec.containers[] | select(.name == "manager")) |= (
            .image = "{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}" |
            .imagePullPolicy = "{{ .Values.image.pullPolicy }}"
        )' "$deployment_file"
        
        log_success "Image templated"
    else
        log_warning "deployment.yaml not found, skipping image templating"
    fi
}

# Step 11: Template replicas
template_replicas() {
    log_info "Step 11: Templating replicas"
    
    local deployment_file="$CHART_DIR/templates/deployment.yaml"
    
    if [[ -f "$deployment_file" ]]; then
        yq -i '(select(.kind == "Deployment").spec.replicas) = "{{ .Values.replicaCount }}"' "$deployment_file"
        
        # Fix: yq adds quotes, remove them for Helm
        if [[ "$OS" == "mac" ]]; then
            sed -i '' "s|replicas: '{{ .Values.replicaCount }}'|replicas: {{ .Values.replicaCount }}|g" "$deployment_file"
            sed -i '' 's|replicas: "{{ .Values.replicaCount }}"|replicas: {{ .Values.replicaCount }}|g' "$deployment_file"
        else
            sed -i "s|replicas: '{{ .Values.replicaCount }}'|replicas: {{ .Values.replicaCount }}|g" "$deployment_file"
            sed -i 's|replicas: "{{ .Values.replicaCount }}"|replicas: {{ .Values.replicaCount }}|g' "$deployment_file"
        fi
        
        log_success "Replicas templated"
    else
        log_warning "deployment.yaml not found, skipping replicas templating"
    fi
}

# Step 12: Add imagePullSecrets to deployment
add_image_pull_secrets() {
    log_info "Step 12: Adding imagePullSecrets to deployment"
    
    local deployment_file="$CHART_DIR/templates/deployment.yaml"
    
    if [[ -f "$deployment_file" ]]; then
        # Check if imagePullSecrets already exists
        if grep -q "imagePullSecrets" "$deployment_file"; then
            log_info "imagePullSecrets already exists, skipping"
            return
        fi
        
        # Use awk to add imagePullSecrets after 'spec:' under template.spec
        # We look for the pattern "    spec:" (4 spaces, under template)
        awk '
        /^    spec:$/ {
            print $0
            print "      {{- with .Values.imagePullSecrets }}"
            print "      imagePullSecrets:"
            print "        {{- toYaml . | nindent 8 }}"
            print "      {{- end }}"
            next
        }
        { print }
        ' "$deployment_file" > "${deployment_file}.tmp"
        
        if [[ -s "${deployment_file}.tmp" ]]; then
            mv "${deployment_file}.tmp" "$deployment_file"
            log_success "imagePullSecrets added"
        else
            rm -f "${deployment_file}.tmp"
            log_warning "Failed to add imagePullSecrets"
        fi
    else
        log_warning "deployment.yaml not found, skipping imagePullSecrets"
    fi
}

# Step 13: Template resources
template_resources() {
    return
    log_info "Step 12: Templating resources"
    
    local deployment_file="$CHART_DIR/templates/deployment.yaml"
    
    if [[ -f "$deployment_file" ]]; then
        # This is complex with yq, use sed to replace the resources block
        # First, create a backup
        cp "$deployment_file" "${deployment_file}.bak"
        
        # Use awk to replace the resources block
        awk '
        /^          resources:/ {
            print "          resources:"
            print "            {{- toYaml .Values.resources | nindent 12 }}"
            in_resources = 1
            next
        }
        in_resources && /^          [a-z]/ {
            in_resources = 0
        }
        in_resources && /^        [a-z]/ {
            in_resources = 0
        }
        !in_resources { print }
        ' "${deployment_file}.bak" > "$deployment_file"
        
        rm -f "${deployment_file}.bak"
        
        log_success "Resources templated"
    else
        log_warning "deployment.yaml not found, skipping resources templating"
    fi
}

# Fix mangled Helm template syntax (yq can mangle {{ }} syntax)
fix_mangled_helm_syntax() {
    log_info "Fixing any mangled Helm template syntax"
    
    for file in "$CHART_DIR/templates"/*.yaml; do
        if [[ -f "$file" && "$file" != *"_helpers.tpl"* && "$file" != *"NOTES.txt"* ]]; then
            # Fix mangled namespace: {? {.Release.Namespace: ''} : ''}
            if [[ "$OS" == "mac" ]]; then
                sed -i '' "s|{? {.Release.Namespace: ''} : ''}|{{ .Release.Namespace }}|g" "$file" 2>/dev/null || true
                sed -i '' "s|{? {.Values\.[^}]*} : ''}|{{ .Values.\1 }}|g" "$file" 2>/dev/null || true
            else
                sed -i "s|{? {.Release.Namespace: ''} : ''}|{{ .Release.Namespace }}|g" "$file" 2>/dev/null || true
                sed -i "s|{? {.Values\.[^}]*} : ''}|{{ .Values.\1 }}|g" "$file" 2>/dev/null || true
            fi
        fi
    done
    
    log_success "Helm syntax fixed"
}

# Step 13: Create NOTES.txt
create_notes() {
    log_info "Step 13: Creating NOTES.txt"
    
    cat > "$CHART_DIR/templates/NOTES.txt" << 'EOF'
Thank you for installing {{ .Chart.Name }}!

Your release is named: {{ .Release.Name }}
Namespace: {{ .Release.Namespace }}

To verify the installation:

  kubectl get pods -n {{ .Release.Namespace }}
  kubectl get crd nodepools.nodepool.k8s.local

To create a NodePool:

  cat <<NODEPOOL | kubectl apply -f -
  apiVersion: nodepool.k8s.local/v1
  kind: NodePool
  metadata:
    name: example-pool
    namespace: default
  spec:
    size: 2
    label: "node-pool=example"
    nodeSelector:
      kubernetes.io/os: linux
  NODEPOOL

To check NodePool status:

  kubectl get nodepools -A
  kubectl describe nodepool example-pool

To uninstall:

  helm uninstall {{ .Release.Name }} -n {{ .Release.Namespace }}
EOF
    
    log_success "NOTES.txt created"
}

# Step 14: Clean up empty files
cleanup_empty_files() {
    log_info "Step 14: Cleaning up empty files"
    
    for file in "$CHART_DIR/templates"/*.yaml; do
        if [[ -f "$file" ]]; then
            # Check if file is empty or only contains whitespace/comments
            local content=$(grep -v '^#' "$file" 2>/dev/null | grep -v '^$' | grep -v '^---$' || true)
            if [[ -z "$content" || "$content" == "null" ]]; then
                rm -f "$file"
                log_info "  Removed empty file: $(basename "$file")"
            fi
        fi
    done
    
    log_success "Cleanup complete"
}

# Validate the final result
validate_result() {
    log_info "Step 15: Validating generated chart"
    
    echo
    log_info "Generated files:"
    find "$CHART_DIR" -type f | sort | while read -r file; do
        echo -e "  ${BLUE}→${NC} $file"
    done
    
    # Check key files exist
    echo
    local checks_passed=0
    local checks_total=0
    
    for check_file in "Chart.yaml" "values.yaml" "templates/_helpers.tpl" "templates/deployment.yaml"; do
        ((checks_total++))
        if [[ -f "$CHART_DIR/$check_file" ]]; then
            log_success "$check_file exists"
            ((checks_passed++))
        else
            log_warning "$check_file missing"
        fi
    done
    
    # Check CRDs
    ((checks_total++))
    if ls "$CHART_DIR/crds/"*.yaml 1> /dev/null 2>&1; then
        log_success "CRDs present in crds/"
        ((checks_passed++))
    else
        log_warning "No CRDs found"
    fi
    
    # Check image templating
    ((checks_total++))
    if grep -q "{{ .Values.image.repository }}" "$CHART_DIR/templates/deployment.yaml" 2>/dev/null; then
        log_success "Image templating OK"
        ((checks_passed++))
    else
        log_warning "Image templating may need manual adjustment"
    fi
    
    # Check namespace templating
    ((checks_total++))
    if grep -q "{{ .Release.Namespace }}" "$CHART_DIR/templates/"*.yaml 2>/dev/null; then
        log_success "Namespace templating OK"
        ((checks_passed++))
    else
        log_warning "Namespace templating may need manual adjustment"
    fi
    
    echo
    log_info "Validation: $checks_passed/$checks_total checks passed"
}

# Lint chart
lint_chart() {
    log_info "Step 16: Linting chart"
    
    if command -v helm &> /dev/null; then
        helm lint "$CHART_DIR" || log_warning "Helm lint found issues"
    else
        log_warning "Helm not found, skipping lint"
    fi
}

# Main execution
main() {
    echo
    echo -e "${BLUE}╔═══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║   NodePool Operator - Helm Chart Generator (Kustomize)    ║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════════════════════════════╝${NC}"
    echo
    
    # Ensure we're in the right directory
    if [[ ! -f "go.mod" ]] || [[ ! -d "config" ]]; then
        log_error "Please run this script from the root of the nodepool-operator project"
    fi
    
    check_dependencies
    detect_os
    
    echo
    log_info "Starting Helm chart generation from Kustomize..."
    echo
    
    create_chart_structure
    create_chart_yaml
    create_values_yaml
    create_helpers
    copy_crds
    generate_manifests
    split_manifests
    remove_crds_from_templates
    replace_labels
    # Run yq operations FIRST (they can mangle Helm template syntax)
    template_image
    template_replicas
    template_resources
    # Run sed-based templating AFTER yq to avoid mangling
    template_namespace
    add_image_pull_secrets
    fix_mangled_helm_syntax
    create_notes
    cleanup_empty_files
    validate_result
    lint_chart
    
    echo
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║   Helm chart generation completed!                        ║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════╝${NC}"
    echo
    echo -e "${YELLOW}Next steps:${NC}"
    echo -e "  ${BLUE}1.${NC} Review generated templates in ${CHART_DIR}/templates/"
    echo -e "  ${BLUE}2.${NC} Update image.repository in ${CHART_DIR}/values.yaml"
    echo -e "  ${BLUE}3.${NC} Test template: ${GREEN}helm template test ${CHART_DIR}${NC}"
    echo -e "  ${BLUE}4.${NC} Install: ${GREEN}helm install nodepool-operator ${CHART_DIR} -n nodepool-system --create-namespace${NC}"
    echo
}

# Run main function
main "$@"
