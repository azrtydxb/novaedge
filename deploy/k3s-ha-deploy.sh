#!/usr/bin/env bash
#
# NovaEdge K3s HA Cluster Deployment Script
#
# Deploys a 3-master + 5-worker K3s HA cluster with NovaEdge
# CP VIP (L2 ARP for bootstrap) and BGP+BFD for service VIPs.
#
# Usage: ./k3s-ha-deploy.sh
#
set -euo pipefail

#############################################################################
# Configuration
#############################################################################

# Control plane VIP
CP_VIP="192.168.100.10"
CP_VIP_CIDR="${CP_VIP}/32"

# K3s token (generated randomly)
K3S_TOKEN="$(openssl rand -hex 32)"

# SSH credentials
SSH_USER="root"
SSH_PASS="Jbz49teq01!"

# Architecture
ARCH="arm64"

# Master nodes
MASTERS=(
  "192.168.100.11"
  "192.168.100.12"
  "192.168.100.13"
)

# Worker nodes
WORKERS=(
  "192.168.100.21"
  "192.168.100.22"
  "192.168.100.23"
  "192.168.100.24"
  "192.168.100.25"
)

# All nodes (masters first, then workers)
ALL_NODES=("${MASTERS[@]}" "${WORKERS[@]}")

# BGP configuration
BGP_PEER_AS=65000
BGP_PEER_ROUTER_1="192.168.100.2"
BGP_PEER_ROUTER_2="192.168.100.3"

# BGP AS number - iBGP: all nodes and routers use the same AS
BGP_LOCAL_AS=65000

# Service VIP IP pool
SVC_VIP_POOL="192.168.100.100/28"

# Temp dir for manifest files
TMPDIR_DEPLOY="$(mktemp -d)"
trap 'rm -rf "${TMPDIR_DEPLOY}"' EXIT

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

#############################################################################
# Helper functions
#############################################################################

log_info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "\n${BLUE}========================================${NC}"; echo -e "${BLUE}  $*${NC}"; echo -e "${BLUE}========================================${NC}\n"; }

ssh_cmd() {
  local host="$1"
  shift
  sshpass -p "${SSH_PASS}" ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -o LogLevel=ERROR "${SSH_USER}@${host}" "$@"
}

scp_cmd() {
  local src="$1"
  local host="$2"
  local dst="$3"
  sshpass -p "${SSH_PASS}" scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null \
    -o LogLevel=ERROR "${src}" "${SSH_USER}@${host}:${dst}"
}

wait_for_k3s_api() {
  local host="$1"
  local max_attempts="${2:-60}"
  local attempt=0
  log_info "Waiting for K3s API server on ${host}..."
  while [ $attempt -lt $max_attempts ]; do
    if ssh_cmd "$host" "kubectl get nodes" &>/dev/null; then
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 5
  done
  log_error "K3s API server on ${host} did not become ready in time"
  return 1
}

wait_for_node_ready() {
  local control_host="$1"
  local node_ip="$2"
  local max_attempts="${3:-60}"
  local attempt=0
  log_info "Waiting for node ${node_ip} to become Ready..."
  while [ $attempt -lt $max_attempts ]; do
    local status
    status=$(ssh_cmd "$control_host" "kubectl get nodes -o wide 2>/dev/null | grep '${node_ip}' | awk '{print \$2}'" 2>/dev/null || true)
    if [ "$status" = "Ready" ]; then
      log_info "Node ${node_ip} is Ready"
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 5
  done
  log_warn "Node ${node_ip} did not become Ready in time (may still be joining)"
  return 1
}

#############################################################################
# Pre-flight checks
#############################################################################

preflight_checks() {
  log_step "Pre-flight checks"

  if ! command -v sshpass &>/dev/null; then
    log_error "sshpass is not installed. Install it with: brew install hudochenkov/sshpass/sshpass"
    exit 1
  fi

  for host in "${ALL_NODES[@]}"; do
    if ssh_cmd "$host" "echo ok" &>/dev/null; then
      log_info "Host ${host} is reachable"
    else
      log_error "Host ${host} is NOT reachable via SSH"
      exit 1
    fi
  done

  log_info "All ${#ALL_NODES[@]} hosts are reachable"
  log_info "K3s token: ${K3S_TOKEN}"
}

#############################################################################
# Phase 1: Prepare all nodes
#############################################################################

prepare_node() {
  local host="$1"
  log_info "Preparing ${host}..."

  ssh_cmd "$host" 'bash -s' <<'PREPARE_EOF'
set -euo pipefail

# Disable swap
swapoff -a 2>/dev/null || true
sed -i '/swap/d' /etc/fstab 2>/dev/null || true

# Load required kernel modules
cat > /etc/modules-load.d/k3s.conf <<EOF
br_netfilter
overlay
ip_vs
ip_vs_rr
ip_vs_wrr
ip_vs_sh
nf_conntrack
EOF
for mod in br_netfilter overlay ip_vs ip_vs_rr ip_vs_wrr ip_vs_sh nf_conntrack; do
  modprobe "$mod" 2>/dev/null || true
done

# Set sysctl parameters
cat > /etc/sysctl.d/99-k3s.conf <<EOF
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
net.ipv4.ip_nonlocal_bind           = 1
net.ipv6.conf.all.forwarding        = 1
EOF
sysctl --system >/dev/null 2>&1

mkdir -p /etc/novaedge
echo "Node preparation complete"
PREPARE_EOF

  log_info "Node ${host} prepared"
}

prepare_all_nodes() {
  log_step "Phase 1: Preparing all nodes"

  for host in "${ALL_NODES[@]}"; do
    prepare_node "$host" &
  done
  wait

  log_info "All nodes prepared"
}

#############################################################################
# Phase 2: Install NovaEdge CP VIP on master nodes
#############################################################################

install_cpvip_on_master() {
  local host="$1"
  local node_name="master-$(echo "$host" | awk -F. '{print $4}')"

  log_info "Installing NovaEdge CP VIP on ${host} (${node_name})..."

  # Generate systemd unit locally and SCP it
  cat > "${TMPDIR_DEPLOY}/novaedge-cpvip-${host}.service" <<EOF
[Unit]
Description=NovaEdge Control Plane VIP Manager
Documentation=https://novaedge.io
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/novaedge-agent \\
  --control-plane-vip \\
  --cp-vip-address=${CP_VIP_CIDR} \\
  --cp-vip-mode=bgp \\
  --cp-bgp-local-as=${BGP_LOCAL_AS} \\
  --cp-bgp-router-id=${host} \\
  --cp-bgp-peer=${BGP_PEER_ROUTER_1}:${BGP_PEER_AS}:179 \\
  --cp-bgp-peer=${BGP_PEER_ROUTER_2}:${BGP_PEER_AS}:179 \\
  --cp-bfd-enabled=true \\
  --cp-bfd-detect-mult=3 \\
  --cp-bfd-tx-interval=300ms \\
  --cp-bfd-rx-interval=300ms \\
  --node-name=${node_name} \\
  --cp-api-port=6443 \\
  --cp-health-interval=1s \\
  --cp-health-timeout=3s \\
  --metrics-port=9092 \\
  --health-probe-port=9093
Restart=always
RestartSec=5
LimitNOFILE=65536
LimitNPROC=4096

[Install]
WantedBy=multi-user.target
EOF

  scp_cmd "${TMPDIR_DEPLOY}/novaedge-cpvip-${host}.service" "$host" "/etc/systemd/system/novaedge-cpvip.service"

  ssh_cmd "$host" "bash -s" <<EOF
set -euo pipefail

# Download novaedge-agent binary
echo "Downloading novaedge-agent (${ARCH})..."
curl -Lo /usr/local/bin/novaedge-agent \
  "https://github.com/piwi3910/novaedge/releases/latest/download/novaedge-agent-linux-${ARCH}" 2>/dev/null || {
  echo "Latest release not found, trying nightly..."
  curl -Lo /usr/local/bin/novaedge-agent \
    "https://github.com/piwi3910/novaedge/releases/download/nightly/novaedge-agent-linux-${ARCH}" 2>/dev/null
}
chmod +x /usr/local/bin/novaedge-agent

# Download novactl binary
echo "Downloading novactl (${ARCH})..."
curl -Lo /usr/local/bin/novactl \
  "https://github.com/piwi3910/novaedge/releases/latest/download/novactl-linux-${ARCH}" 2>/dev/null || {
  echo "Latest release not found, trying nightly..."
  curl -Lo /usr/local/bin/novactl \
    "https://github.com/piwi3910/novaedge/releases/download/nightly/novactl-linux-${ARCH}" 2>/dev/null
}
chmod +x /usr/local/bin/novactl

# Enable and start the CP VIP service
systemctl daemon-reload
systemctl enable novaedge-cpvip.service
systemctl start novaedge-cpvip.service

echo "NovaEdge CP VIP installed and started on ${node_name}"
EOF

  log_info "CP VIP installed on ${host}"
}

install_cpvip_on_masters() {
  log_step "Phase 2: Installing NovaEdge CP VIP on master nodes"

  for host in "${MASTERS[@]}"; do
    install_cpvip_on_master "$host" &
  done
  wait

  log_info "Waiting for CP VIP election..."
  sleep 5

  log_info "CP VIP service running on all masters"
}

#############################################################################
# Phase 3: Bootstrap K3s cluster
#############################################################################

bootstrap_first_master() {
  local host="${MASTERS[0]}"

  log_step "Phase 3a: Bootstrapping first K3s master (${host})"

  ssh_cmd "$host" "bash -s" <<EOF
set -euo pipefail

echo "Installing K3s with cluster-init on ${host}..."
curl -sfL https://get.k3s.io | K3S_TOKEN="${K3S_TOKEN}" sh -s - server \
  --cluster-init \
  --tls-san="${CP_VIP}" \
  --disable=servicelb \
  --node-name="master-\$(echo ${host} | awk -F. '{print \$4}')" \
  --write-kubeconfig-mode=644

echo "Waiting for K3s to be ready..."
for i in \$(seq 1 60); do
  if kubectl get nodes &>/dev/null; then
    echo "K3s is ready"
    break
  fi
  sleep 5
done

kubectl get nodes
EOF

  wait_for_k3s_api "$host"
  log_info "First master ${host} bootstrapped successfully"
}

join_additional_master() {
  local host="$1"
  local node_name="master-$(echo "$host" | awk -F. '{print $4}')"

  log_info "Joining master ${host} (${node_name}) to cluster..."

  ssh_cmd "$host" "bash -s" <<EOF
set -euo pipefail

echo "Installing K3s and joining cluster..."
curl -sfL https://get.k3s.io | K3S_TOKEN="${K3S_TOKEN}" sh -s - server \
  --server "https://${CP_VIP}:6443" \
  --tls-san="${CP_VIP}" \
  --disable=servicelb \
  --node-name="${node_name}" \
  --write-kubeconfig-mode=644

echo "K3s joined on ${node_name}"
EOF

  log_info "Master ${host} joined cluster"
}

join_masters() {
  log_step "Phase 3b: Joining additional masters"

  for host in "${MASTERS[@]:1}"; do
    join_additional_master "$host"
    sleep 10
  done

  wait_for_k3s_api "${MASTERS[0]}"
  for host in "${MASTERS[@]}"; do
    wait_for_node_ready "${MASTERS[0]}" "$host" 30 || true
  done

  ssh_cmd "${MASTERS[0]}" "kubectl get nodes -o wide"
}

join_worker() {
  local host="$1"
  local node_name="worker-$(echo "$host" | awk -F. '{print $4}')"

  log_info "Joining worker ${host} (${node_name}) to cluster..."

  ssh_cmd "$host" "bash -s" <<EOF
set -euo pipefail

echo "Installing K3s agent and joining cluster..."
curl -sfL https://get.k3s.io | K3S_TOKEN="${K3S_TOKEN}" sh -s - agent \
  --server "https://${CP_VIP}:6443" \
  --node-name="${node_name}"

echo "K3s agent joined on ${node_name}"
EOF

  log_info "Worker ${host} joined cluster"
}

join_workers() {
  log_step "Phase 3c: Joining worker nodes"

  for host in "${WORKERS[@]}"; do
    join_worker "$host" &
  done
  wait

  sleep 15
  for host in "${WORKERS[@]}"; do
    wait_for_node_ready "${MASTERS[0]}" "$host" 30 || true
  done

  log_info "All workers joined"
  ssh_cmd "${MASTERS[0]}" "kubectl get nodes -o wide"
}

#############################################################################
# Phase 4: Deploy NovaEdge with BGP+BFD
#############################################################################

generate_manifests() {
  log_info "Generating NovaEdge manifests..."

  # Controller deployment
  cat > "${TMPDIR_DEPLOY}/01-namespace.yaml" <<'EOF'
apiVersion: v1
kind: Namespace
metadata:
  name: nova-system
EOF

  # RBAC
  cat > "${TMPDIR_DEPLOY}/02-rbac.yaml" <<'EOF'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: novaedge-controller
  namespace: nova-system
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
  namespace: nova-system
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: novaedge-agent
  namespace: nova-system
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
  namespace: nova-system
EOF

  # Controller deployment
  cat > "${TMPDIR_DEPLOY}/03-controller.yaml" <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: novaedge-controller
  namespace: nova-system
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
        image: novaedge-controller:latest
        imagePullPolicy: IfNotPresent
        command: ["/novaedge-controller"]
        args:
        - "--leader-elect"
        - "--metrics-bind-address=:8080"
        - "--health-probe-bind-address=:8081"
        - "--grpc-bind-address=:9090"
        ports:
        - name: metrics
          containerPort: 8080
          protocol: TCP
        - name: health
          containerPort: 8081
          protocol: TCP
        - name: grpc
          containerPort: 9090
          protocol: TCP
        livenessProbe:
          httpGet:
            path: /healthz
            port: health
          initialDelaySeconds: 15
          periodSeconds: 20
        readinessProbe:
          httpGet:
            path: /readyz
            port: health
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          requests:
            cpu: 100m
            memory: 128Mi
          limits:
            cpu: 500m
            memory: 512Mi
      nodeSelector:
        node-role.kubernetes.io/control-plane: "true"
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
EOF

  # Controller Service
  cat > "${TMPDIR_DEPLOY}/03b-controller-service.yaml" <<'EOF'
apiVersion: v1
kind: Service
metadata:
  name: novaedge-controller
  namespace: nova-system
  labels:
    app.kubernetes.io/name: novaedge-controller
spec:
  selector:
    app.kubernetes.io/name: novaedge-controller
  ports:
  - name: grpc
    port: 9090
    targetPort: grpc
    protocol: TCP
  - name: metrics
    port: 8080
    targetPort: metrics
    protocol: TCP
  - name: health
    port: 8081
    targetPort: health
    protocol: TCP
EOF

  # Agent DaemonSet
  cat > "${TMPDIR_DEPLOY}/04-agent.yaml" <<'EOF'
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: novaedge-agent
  namespace: nova-system
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
        image: novaedge-agent:latest
        imagePullPolicy: IfNotPresent
        command: ["/novaedge-agent"]
        args:
        - "-controller-address=$(CONTROLLER_ADDRESS)"
        - "-node-name=$(NODE_NAME)"
        - "-metrics-port=9090"
        - "-health-probe-port=9091"
        - "-log-level=info"
        env:
        - name: CONTROLLER_ADDRESS
          value: novaedge-controller.nova-system.svc.cluster.local:9090
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
          protocol: TCP
        - name: https
          containerPort: 443
          hostPort: 443
          protocol: TCP
        - name: metrics
          containerPort: 9090
          protocol: TCP
        - name: health
          containerPort: 9091
          protocol: TCP
        livenessProbe:
          httpGet:
            path: /healthz
            port: health
            scheme: HTTP
          initialDelaySeconds: 30
          periodSeconds: 20
          timeoutSeconds: 5
          failureThreshold: 3
        readinessProbe:
          httpGet:
            path: /readyz
            port: health
            scheme: HTTP
          initialDelaySeconds: 10
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3
        securityContext:
          privileged: true
          capabilities:
            add:
            - NET_ADMIN
            - NET_RAW
            - NET_BIND_SERVICE
        volumeMounts:
        - name: certs
          mountPath: /etc/novaedge/certs
          readOnly: true
        - name: config
          mountPath: /etc/novaedge/config
          readOnly: true
        resources:
          requests:
            cpu: 500m
            memory: 256Mi
          limits:
            cpu: 2000m
            memory: 1Gi
      volumes:
      - name: certs
        emptyDir: {}
      - name: config
        configMap:
          name: novaedge-agent-config
          optional: true
EOF

  # IP Pool
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

  # BGP+BFD ProxyVIP for control plane (iBGP: same AS as routers)
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
    localAS: ${BGP_LOCAL_AS}
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

  log_info "Manifests generated in ${TMPDIR_DEPLOY}"
}

deploy_novaedge() {
  log_step "Phase 4: Deploying NovaEdge with BGP+BFD"

  local control_host="${MASTERS[0]}"

  # Generate all manifests locally
  generate_manifests

  # Create remote dir and SCP manifests
  ssh_cmd "$control_host" "mkdir -p /tmp/novaedge-deploy"
  for f in "${TMPDIR_DEPLOY}"/0*.yaml; do
    scp_cmd "$f" "$control_host" "/tmp/novaedge-deploy/$(basename "$f")"
  done

  # Apply everything on the remote host
  ssh_cmd "$control_host" "bash -s" <<'DEPLOY_EOF'
set -euo pipefail
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

echo "Installing NovaEdge CRDs..."
for crd_url in \
  "https://raw.githubusercontent.com/piwi3910/novaedge/main/config/crd/novaedge.io_proxygateways.yaml" \
  "https://raw.githubusercontent.com/piwi3910/novaedge/main/config/crd/novaedge.io_proxyroutes.yaml" \
  "https://raw.githubusercontent.com/piwi3910/novaedge/main/config/crd/novaedge.io_proxybackends.yaml" \
  "https://raw.githubusercontent.com/piwi3910/novaedge/main/config/crd/novaedge.io_proxypolicies.yaml" \
  "https://raw.githubusercontent.com/piwi3910/novaedge/main/config/crd/novaedge.io_proxyvips.yaml" \
  "https://raw.githubusercontent.com/piwi3910/novaedge/main/config/crd/novaedge.io_proxycertificates.yaml" \
  "https://raw.githubusercontent.com/piwi3910/novaedge/main/config/crd/novaedge.io_proxyippools.yaml" \
  "https://raw.githubusercontent.com/piwi3910/novaedge/main/config/crd/novaedge.io_novaedgeclusters.yaml" \
  "https://raw.githubusercontent.com/piwi3910/novaedge/main/config/crd/novaedge.io_novaedgefederations.yaml" \
  "https://raw.githubusercontent.com/piwi3910/novaedge/main/config/crd/novaedge.io_novaedgeremoteclusters.yaml"; do
  kubectl apply -f "$crd_url" 2>/dev/null || true
done

echo "Applying NovaEdge manifests..."
for manifest in /tmp/novaedge-deploy/*.yaml; do
  echo "  Applying $(basename "$manifest")..."
  kubectl apply -f "$manifest"
done

echo "Waiting for NovaEdge pods to start..."
kubectl -n nova-system rollout status deployment/novaedge-controller --timeout=120s || true
kubectl -n nova-system rollout status daemonset/novaedge-agent --timeout=120s || true

echo ""
echo "NovaEdge deployed successfully"
kubectl -n nova-system get pods -o wide

# Cleanup
rm -rf /tmp/novaedge-deploy
DEPLOY_EOF

  log_info "NovaEdge deployed with BGP+BFD"
}

#############################################################################
# Phase 5: Verification
#############################################################################

verify_cluster() {
  log_step "Phase 5: Verifying cluster"

  local control_host="${MASTERS[0]}"

  ssh_cmd "$control_host" "bash -s" <<'VERIFY_EOF'
set -euo pipefail
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

echo "=== Cluster Nodes ==="
kubectl get nodes -o wide

echo ""
echo "=== NovaEdge Pods ==="
kubectl -n nova-system get pods -o wide

echo ""
echo "=== CP VIP Service Status ==="
systemctl status novaedge-cpvip --no-pager -l 2>/dev/null | head -20 || true

echo ""
echo "=== ProxyVIP Resources ==="
kubectl get proxyvip -A 2>/dev/null || echo "No ProxyVIP resources found"

echo ""
echo "=== ProxyIPPool Resources ==="
kubectl get proxyippool -A 2>/dev/null || echo "No ProxyIPPool resources found"

echo ""
echo "=== System Pods ==="
kubectl -n kube-system get pods -o wide
VERIFY_EOF

  log_info "Cluster verification complete"
}

#############################################################################
# Phase 6: Fetch kubeconfig
#############################################################################

fetch_kubeconfig() {
  log_step "Phase 6: Fetching kubeconfig"

  local control_host="${MASTERS[0]}"
  local kubeconfig_path="${HOME}/.kube/novaedge-cluster.yaml"

  mkdir -p "${HOME}/.kube"

  ssh_cmd "$control_host" "cat /etc/rancher/k3s/k3s.yaml" | \
    sed "s/127.0.0.1/${CP_VIP}/g" | \
    sed "s/default/novaedge-cluster/g" > "${kubeconfig_path}"

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
  echo "  NovaEdge K3s HA Cluster Deployment"
  echo ""
  echo "  Masters: ${MASTERS[*]}"
  echo "  Workers: ${WORKERS[*]}"
  echo "  CP VIP:  ${CP_VIP}"
  echo "  Mode:    BGP + BFD"
  echo "  Arch:    ${ARCH}"
  echo "================================================================"
  echo -e "${NC}"

  preflight_checks
  prepare_all_nodes
  install_cpvip_on_masters
  bootstrap_first_master
  join_masters
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
  echo "K3s Token:         ${K3S_TOKEN}"
  echo ""
  echo "BGP Configuration (iBGP):"
  echo "  Local AS:        ${BGP_LOCAL_AS}"
  echo "  Peer routers:    ${BGP_PEER_ROUTER_1}, ${BGP_PEER_ROUTER_2} (AS ${BGP_PEER_AS})"
  echo ""
  echo "Kubeconfig: ~/.kube/novaedge-cluster.yaml"
  echo "  export KUBECONFIG=~/.kube/novaedge-cluster.yaml"
  echo ""
  echo "Next steps:"
  echo "  1. Configure BGP peers on your routers (${BGP_PEER_ROUTER_1}, ${BGP_PEER_ROUTER_2})"
  echo "  2. Enable BFD on the router peers"
  echo "  3. Test with: kubectl --kubeconfig=~/.kube/novaedge-cluster.yaml get nodes"
  echo -e "${NC}"
}

main "$@"
