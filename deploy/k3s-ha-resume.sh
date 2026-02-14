#!/usr/bin/env bash
#
# NovaEdge K3s HA Cluster Deployment - Resume Script
#
# Resumes deployment after fixing binary issue.
# Uses expect instead of sshpass for SSH auth.
#
set -euo pipefail

#############################################################################
# Configuration
#############################################################################

CP_VIP="192.168.100.10"
CP_VIP_CIDR="${CP_VIP}/32"

# Use the same token from the first run
K3S_TOKEN="8447c567a8493a46cc4f3ff28acc77d8f7580448111b3505f0507bcad8f8b6c8"

SSH_USER="root"
SSH_PASS='Jbz49teq01!'

MASTERS=("192.168.100.11" "192.168.100.12" "192.168.100.13")
WORKERS=("192.168.100.21" "192.168.100.22" "192.168.100.23" "192.168.100.24" "192.168.100.25")
ALL_NODES=("${MASTERS[@]}" "${WORKERS[@]}")

BGP_PEER_AS=65000
BGP_PEER_ROUTER_1="192.168.100.2"
BGP_PEER_ROUTER_2="192.168.100.3"

SVC_VIP_POOL="192.168.100.100/28"

AGENT_BIN="/tmp/novaedge-builds/novaedge-agent-linux-arm64"
NOVACTL_BIN="/tmp/novaedge-builds/novactl-linux-arm64"

TMPDIR_DEPLOY="$(mktemp -d)"
trap 'rm -rf "${TMPDIR_DEPLOY}"' EXIT

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "\n${BLUE}========================================${NC}"; echo -e "${BLUE}  $*${NC}"; echo -e "${BLUE}========================================${NC}\n"; }

#############################################################################
# SSH helpers using expect
#############################################################################

ssh_cmd() {
  local host="$1"
  shift
  local cmd="$*"
  expect -c "
    set timeout 300
    log_user 1
    spawn ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR ${SSH_USER}@${host} {${cmd}}
    expect {
      \"password:\" { send \"${SSH_PASS}\r\"; exp_continue }
      eof
    }
    foreach {pid spawnid os_error_flag value} [wait] break
    exit \$value
  " 2>&1 | grep -v "^spawn " | grep -v "password:" || true
}

scp_cmd() {
  local src="$1"
  local host="$2"
  local dst="$3"
  expect -c "
    set timeout 300
    log_user 1
    spawn scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR ${src} ${SSH_USER}@${host}:${dst}
    expect {
      \"password:\" { send \"${SSH_PASS}\r\"; exp_continue }
      eof
    }
    foreach {pid spawnid os_error_flag value} [wait] break
    exit \$value
  " 2>&1 | grep -v "^spawn " | grep -v "password:" || true
}

ssh_script() {
  local host="$1"
  local script_file="$2"
  expect -c "
    set timeout 600
    log_user 1
    spawn ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR ${SSH_USER}@${host} bash -s < ${script_file}
    expect {
      \"password:\" { send \"${SSH_PASS}\r\"; exp_continue }
      eof
    }
    foreach {pid spawnid os_error_flag value} [wait] break
    exit \$value
  " 2>&1 | grep -v "^spawn " | grep -v "password:" || true
}

#############################################################################
# Phase 0: Fix binaries on masters and stop broken K3s on .12
#############################################################################

fix_binaries() {
  log_step "Phase 0: Fixing binaries on all masters"

  for host in "${MASTERS[@]}"; do
    log_info "Uploading correct arm64 binaries to ${host}..."
    scp_cmd "${AGENT_BIN}" "$host" "/usr/local/bin/novaedge-agent"
    scp_cmd "${NOVACTL_BIN}" "$host" "/usr/local/bin/novactl"
    ssh_cmd "$host" "chmod +x /usr/local/bin/novaedge-agent /usr/local/bin/novactl"
    log_info "Binaries fixed on ${host}"
  done

  # Stop broken K3s on .12 before restarting cpvip
  log_info "Stopping broken K3s on master-12..."
  ssh_cmd "192.168.100.12" "systemctl stop k3s 2>/dev/null || true; /usr/local/bin/k3s-killall.sh 2>/dev/null || true"

  # Restart cpvip on all masters
  for host in "${MASTERS[@]}"; do
    log_info "Restarting cpvip on ${host}..."
    ssh_cmd "$host" "systemctl restart novaedge-cpvip"
  done

  log_info "Waiting for CP VIP to bind..."
  sleep 10

  # Check VIP
  ssh_cmd "192.168.100.11" "ip addr show | grep 192.168.100.10 || echo 'VIP not bound yet'"
}

#############################################################################
# Phase 3: Resume K3s cluster bootstrap
#############################################################################

resume_k3s_masters() {
  log_step "Phase 3: Resuming K3s master join"

  # Check if .11 is still healthy
  log_info "Verifying master-11..."
  ssh_cmd "192.168.100.11" "kubectl get nodes -o wide"

  # Rejoin master-12
  log_info "Re-joining master-12..."
  cat > "${TMPDIR_DEPLOY}/join-master-12.sh" <<'EOF'
set -euo pipefail

# Uninstall broken K3s first
/usr/local/bin/k3s-uninstall.sh 2>/dev/null || true

echo "Installing K3s and joining cluster..."
curl -sfL https://get.k3s.io | K3S_TOKEN="8447c567a8493a46cc4f3ff28acc77d8f7580448111b3505f0507bcad8f8b6c8" sh -s - server \
  --server "https://192.168.100.10:6443" \
  --tls-san="192.168.100.10" \
  --disable=servicelb \
  --node-name="master-12" \
  --write-kubeconfig-mode=644

echo "K3s joined on master-12"
EOF
  ssh_script "192.168.100.12" "${TMPDIR_DEPLOY}/join-master-12.sh"

  log_info "Waiting 15s for etcd to stabilize..."
  sleep 15

  # Join master-13
  log_info "Joining master-13..."
  cat > "${TMPDIR_DEPLOY}/join-master-13.sh" <<'EOF'
set -euo pipefail

echo "Installing K3s and joining cluster..."
curl -sfL https://get.k3s.io | K3S_TOKEN="8447c567a8493a46cc4f3ff28acc77d8f7580448111b3505f0507bcad8f8b6c8" sh -s - server \
  --server "https://192.168.100.10:6443" \
  --tls-san="192.168.100.10" \
  --disable=servicelb \
  --node-name="master-13" \
  --write-kubeconfig-mode=644

echo "K3s joined on master-13"
EOF
  ssh_script "192.168.100.13" "${TMPDIR_DEPLOY}/join-master-13.sh"

  log_info "Waiting for all masters..."
  sleep 15

  ssh_cmd "192.168.100.11" "kubectl get nodes -o wide"
}

#############################################################################
# Phase 3c: Join workers
#############################################################################

join_workers() {
  log_step "Phase 3c: Joining worker nodes"

  for host in "${WORKERS[@]}"; do
    local node_name="worker-$(echo "$host" | awk -F. '{print $4}')"
    log_info "Joining worker ${host} (${node_name})..."

    cat > "${TMPDIR_DEPLOY}/join-${host}.sh" <<EOF
set -euo pipefail
echo "Installing K3s agent and joining cluster..."
curl -sfL https://get.k3s.io | K3S_TOKEN="8447c567a8493a46cc4f3ff28acc77d8f7580448111b3505f0507bcad8f8b6c8" sh -s - agent \\
  --server "https://192.168.100.10:6443" \\
  --node-name="${node_name}"
echo "K3s agent joined on ${node_name}"
EOF
    ssh_script "$host" "${TMPDIR_DEPLOY}/join-${host}.sh" &
  done
  wait

  log_info "Waiting for workers to become Ready..."
  sleep 30

  ssh_cmd "192.168.100.11" "kubectl get nodes -o wide"
}

#############################################################################
# Phase 4: Deploy NovaEdge with BGP+BFD
#############################################################################

deploy_novaedge() {
  log_step "Phase 4: Deploying NovaEdge with BGP+BFD"

  local control_host="${MASTERS[0]}"

  # Generate manifests locally
  cat > "${TMPDIR_DEPLOY}/01-namespace.yaml" <<'EOF'
apiVersion: v1
kind: Namespace
metadata:
  name: novaedge-system
EOF

  cat > "${TMPDIR_DEPLOY}/02-rbac.yaml" <<'EOF'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: novaedge-controller
  namespace: novaedge-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: novaedge-controller
rules:
- apiGroups: [""]
  resources: ["nodes", "services", "endpoints", "pods", "secrets", "configmaps", "events"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: [""]
  resources: ["services/status"]
  verbs: ["update", "patch"]
- apiGroups: ["discovery.k8s.io"]
  resources: ["endpointslices"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["networking.k8s.io"]
  resources: ["ingresses", "ingressclasses"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["networking.k8s.io"]
  resources: ["ingresses/status"]
  verbs: ["update", "patch"]
- apiGroups: ["gateway.networking.k8s.io"]
  resources: ["gateways", "httproutes", "tcproutes", "tlsroutes", "grpcroutes", "gatewayclasses"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["gateway.networking.k8s.io"]
  resources: ["gateways/status", "httproutes/status", "gatewayclasses/status"]
  verbs: ["update", "patch"]
- apiGroups: ["novaedge.io"]
  resources: ["*"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
- apiGroups: ["novaedge.io"]
  resources: ["*/status"]
  verbs: ["update", "patch"]
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: novaedge-controller
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: novaedge-controller
subjects:
- kind: ServiceAccount
  name: novaedge-controller
  namespace: novaedge-system
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: novaedge-agent
  namespace: novaedge-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: novaedge-agent
rules:
- apiGroups: [""]
  resources: ["nodes", "pods", "services", "endpoints", "secrets"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["novaedge.io"]
  resources: ["*"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: novaedge-agent
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: novaedge-agent
subjects:
- kind: ServiceAccount
  name: novaedge-agent
  namespace: novaedge-system
EOF

  cat > "${TMPDIR_DEPLOY}/03-controller.yaml" <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: novaedge-controller
  namespace: novaedge-system
  labels:
    app.kubernetes.io/name: novaedge-controller
spec:
  replicas: 3
  selector:
    matchLabels:
      app.kubernetes.io/name: novaedge-controller
  template:
    metadata:
      labels:
        app.kubernetes.io/name: novaedge-controller
    spec:
      serviceAccountName: novaedge-controller
      tolerations:
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchLabels:
                  app.kubernetes.io/name: novaedge-controller
              topologyKey: kubernetes.io/hostname
      containers:
      - name: controller
        image: ghcr.io/piwi3910/novaedge-controller:nightly
        args:
        - "--enable-service-lb"
        - "--leader-elect=true"
        ports:
        - name: grpc
          containerPort: 8082
        - name: metrics
          containerPort: 8080
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
EOF

  cat > "${TMPDIR_DEPLOY}/04-agent.yaml" <<'AGENTEOF'
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: novaedge-agent
  namespace: novaedge-system
  labels:
    app.kubernetes.io/name: novaedge-agent
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: novaedge-agent
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
  template:
    metadata:
      labels:
        app.kubernetes.io/name: novaedge-agent
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      serviceAccountName: novaedge-agent
      tolerations:
      - operator: Exists
      containers:
      - name: agent
        image: ghcr.io/piwi3910/novaedge-agent:nightly
        args:
        - "--node-name=$(NODE_NAME)"
        - "--controller-addr=novaedge-controller.novaedge-system.svc.cluster.local:8082"
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        - name: NODE_IP
          valueFrom:
            fieldRef:
              fieldPath: status.hostIP
        ports:
        - name: http
          containerPort: 80
          hostPort: 80
        - name: https
          containerPort: 443
          hostPort: 443
        - name: metrics
          containerPort: 9090
        securityContext:
          privileged: true
          capabilities:
            add:
            - NET_ADMIN
            - NET_RAW
            - SYS_ADMIN
        resources:
          requests:
            cpu: 500m
            memory: 256Mi
          limits:
            cpu: 2000m
            memory: 1Gi
AGENTEOF

  cat > "${TMPDIR_DEPLOY}/05-ippool.yaml" <<EOF
apiVersion: novaedge.io/v1alpha1
kind: ProxyIPPool
metadata:
  name: default
spec:
  cidrs:
    - "${SVC_VIP_POOL}"
  autoAssign: true
EOF

  cat > "${TMPDIR_DEPLOY}/06-proxyvip-cp.yaml" <<EOF
apiVersion: novaedge.io/v1alpha1
kind: ProxyVIP
metadata:
  name: k3s-cp-vip
spec:
  address: ${CP_VIP_CIDR}
  mode: BGP
  addressFamily: ipv4
  ports:
    - 6443
  bgpConfig:
    localAS: ${BGP_PEER_AS}
    peers:
      - address: "${BGP_PEER_ROUTER_1}"
        as: ${BGP_PEER_AS}
        port: 179
      - address: "${BGP_PEER_ROUTER_2}"
        as: ${BGP_PEER_AS}
        port: 179
  bfd:
    enabled: true
    detectMultiplier: 3
    desiredMinTxInterval: "300ms"
    requiredMinRxInterval: "300ms"
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/control-plane: "true"
EOF

  # SCP manifests to control host
  ssh_cmd "$control_host" "mkdir -p /tmp/novaedge-deploy"
  for f in "${TMPDIR_DEPLOY}"/0*.yaml; do
    scp_cmd "$f" "$control_host" "/tmp/novaedge-deploy/$(basename "$f")"
  done

  # Also SCP the CRD files from local repo
  ssh_cmd "$control_host" "mkdir -p /tmp/novaedge-deploy/crds"
  for crd in config/crd/novaedge.io_*.yaml; do
    scp_cmd "$crd" "$control_host" "/tmp/novaedge-deploy/crds/$(basename "$crd")"
  done

  # Apply on remote
  cat > "${TMPDIR_DEPLOY}/apply.sh" <<'APPLYEOF'
set -euo pipefail
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

echo "Installing NovaEdge CRDs..."
for crd in /tmp/novaedge-deploy/crds/*.yaml; do
  kubectl apply -f "$crd" 2>/dev/null || true
done

echo "Applying NovaEdge manifests..."
for manifest in /tmp/novaedge-deploy/0*.yaml; do
  echo "  Applying $(basename "$manifest")..."
  kubectl apply -f "$manifest"
done

echo "Waiting for NovaEdge controller..."
kubectl -n novaedge-system rollout status deployment/novaedge-controller --timeout=120s 2>/dev/null || true

echo "Waiting for NovaEdge agents..."
kubectl -n novaedge-system rollout status daemonset/novaedge-agent --timeout=120s 2>/dev/null || true

echo ""
echo "NovaEdge deployed:"
kubectl -n novaedge-system get pods -o wide

rm -rf /tmp/novaedge-deploy
APPLYEOF
  ssh_script "$control_host" "${TMPDIR_DEPLOY}/apply.sh"

  log_info "NovaEdge deployed with BGP+BFD"
}

#############################################################################
# Phase 5: Verify
#############################################################################

verify_cluster() {
  log_step "Phase 5: Verifying cluster"

  cat > "${TMPDIR_DEPLOY}/verify.sh" <<'EOF'
set -euo pipefail
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

echo "=== Cluster Nodes ==="
kubectl get nodes -o wide

echo ""
echo "=== NovaEdge Pods ==="
kubectl -n novaedge-system get pods -o wide 2>/dev/null || echo "No NovaEdge pods"

echo ""
echo "=== CP VIP Status ==="
systemctl status novaedge-cpvip --no-pager -l 2>/dev/null | head -15 || true

echo ""
echo "=== VIP bound? ==="
ip addr show | grep 192.168.100.10 || echo "VIP not bound on this node"

echo ""
echo "=== ProxyVIP ==="
kubectl get proxyvip -A 2>/dev/null || echo "No ProxyVIP"

echo ""
echo "=== ProxyIPPool ==="
kubectl get proxyippool -A 2>/dev/null || echo "No ProxyIPPool"

echo ""
echo "=== System Pods ==="
kubectl -n kube-system get pods
EOF
  ssh_script "192.168.100.11" "${TMPDIR_DEPLOY}/verify.sh"
}

#############################################################################
# Phase 6: Fetch kubeconfig
#############################################################################

fetch_kubeconfig() {
  log_step "Phase 6: Fetching kubeconfig"

  mkdir -p "${HOME}/.kube"
  local kubeconfig_path="${HOME}/.kube/novaedge-cluster.yaml"

  expect -c "
    set timeout 30
    log_user 0
    spawn ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR ${SSH_USER}@192.168.100.11 cat /etc/rancher/k3s/k3s.yaml
    expect \"password:\" { send \"${SSH_PASS}\r\" }
    expect eof
    set output \$expect_out(buffer)
    puts -nonewline \$output
  " 2>/dev/null | sed "s/127.0.0.1/${CP_VIP}/g" | sed "s/default/novaedge-cluster/g" > "${kubeconfig_path}"

  chmod 600 "${kubeconfig_path}"
  log_info "Kubeconfig saved to ${kubeconfig_path}"
  log_info "Use: export KUBECONFIG=${kubeconfig_path}"
}

#############################################################################
# Main
#############################################################################

main() {
  echo -e "${BLUE}"
  echo "================================================================"
  echo "  NovaEdge K3s HA - Resume Deployment"
  echo "================================================================"
  echo -e "${NC}"

  fix_binaries
  resume_k3s_masters
  join_workers
  deploy_novaedge
  verify_cluster
  fetch_kubeconfig

  log_step "Deployment Complete!"

  echo -e "${GREEN}"
  echo "K3s HA cluster with NovaEdge BGP+BFD is ready!"
  echo ""
  echo "Control Plane VIP: ${CP_VIP}"
  echo "API Server:        https://${CP_VIP}:6443"
  echo ""
  echo "Kubeconfig: ~/.kube/novaedge-cluster.yaml"
  echo "  export KUBECONFIG=~/.kube/novaedge-cluster.yaml"
  echo -e "${NC}"
}

main "$@"
