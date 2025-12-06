/*
Copyright 2024 NovaEdge Authors.

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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	novaedgev1alpha1 "github.com/piwi3910/novaedge/api/v1alpha1"
)

const (
	novaEdgeClusterFinalizer = "novaedge.io/finalizer"

	// Condition types
	ConditionTypeReady        = "Ready"
	ConditionTypeControllerOK = "ControllerReady"
	ConditionTypeAgentOK      = "AgentReady"
	ConditionTypeWebUIOK      = "WebUIReady"
	ConditionTypeDegraded     = "Degraded"
)

// NovaEdgeClusterReconciler reconciles a NovaEdgeCluster object
type NovaEdgeClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=novaedge.io,resources=novaedgeclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=novaedge.io,resources=novaedgeclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=novaedge.io,resources=novaedgeclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments;daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services;serviceaccounts;configmaps;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings;roles;rolebindings,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop
func (r *NovaEdgeClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling NovaEdgeCluster", "name", req.Name, "namespace", req.Namespace)

	// Fetch the NovaEdgeCluster instance
	cluster := &novaedgev1alpha1.NovaEdgeCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("NovaEdgeCluster resource not found, ignoring")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get NovaEdgeCluster")
		return ctrl.Result{}, err
	}

	// Handle finalizer
	if cluster.ObjectMeta.DeletionTimestamp.IsZero() {
		// Add finalizer if not present
		if !controllerutil.ContainsFinalizer(cluster, novaEdgeClusterFinalizer) {
			controllerutil.AddFinalizer(cluster, novaEdgeClusterFinalizer)
			if err := r.Update(ctx, cluster); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// Handle deletion
		if controllerutil.ContainsFinalizer(cluster, novaEdgeClusterFinalizer) {
			if err := r.cleanupResources(ctx, cluster); err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(cluster, novaEdgeClusterFinalizer)
			if err := r.Update(ctx, cluster); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Initialize status if needed
	if cluster.Status.Phase == "" {
		cluster.Status.Phase = novaedgev1alpha1.ClusterPhasePending
		if err := r.Status().Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Reconcile all components
	var reconcileErrors []error

	// 1. Reconcile RBAC
	if err := r.reconcileRBAC(ctx, cluster); err != nil {
		reconcileErrors = append(reconcileErrors, fmt.Errorf("RBAC: %w", err))
	}

	// 2. Reconcile Controller
	if err := r.reconcileController(ctx, cluster); err != nil {
		reconcileErrors = append(reconcileErrors, fmt.Errorf("Controller: %w", err))
	}

	// 3. Reconcile Agent
	if err := r.reconcileAgent(ctx, cluster); err != nil {
		reconcileErrors = append(reconcileErrors, fmt.Errorf("Agent: %w", err))
	}

	// 4. Reconcile WebUI (if enabled)
	if cluster.Spec.WebUI != nil && (cluster.Spec.WebUI.Enabled == nil || *cluster.Spec.WebUI.Enabled) {
		if err := r.reconcileWebUI(ctx, cluster); err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("WebUI: %w", err))
		}
	}

	// Update status
	if err := r.updateStatus(ctx, cluster); err != nil {
		return ctrl.Result{}, err
	}

	if len(reconcileErrors) > 0 {
		logger.Error(reconcileErrors[0], "Reconciliation errors", "count", len(reconcileErrors))
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	logger.Info("NovaEdgeCluster reconciled successfully")
	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

// cleanupResources removes all resources created by the operator
func (r *NovaEdgeClusterReconciler) cleanupResources(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	logger := log.FromContext(ctx)
	logger.Info("Cleaning up resources for NovaEdgeCluster", "name", cluster.Name)

	// Resources will be garbage collected through owner references
	// Only need to clean up cluster-scoped resources

	// Delete ClusterRoleBinding
	crb := &rbacv1.ClusterRoleBinding{}
	crbName := fmt.Sprintf("%s-%s-controller", cluster.Namespace, cluster.Name)
	if err := r.Get(ctx, types.NamespacedName{Name: crbName}, crb); err == nil {
		if err := r.Delete(ctx, crb); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	agentCRBName := fmt.Sprintf("%s-%s-agent", cluster.Namespace, cluster.Name)
	if err := r.Get(ctx, types.NamespacedName{Name: agentCRBName}, crb); err == nil {
		if err := r.Delete(ctx, crb); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	// Delete ClusterRole
	cr := &rbacv1.ClusterRole{}
	crName := fmt.Sprintf("%s-%s-controller", cluster.Namespace, cluster.Name)
	if err := r.Get(ctx, types.NamespacedName{Name: crName}, cr); err == nil {
		if err := r.Delete(ctx, cr); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	agentCRName := fmt.Sprintf("%s-%s-agent", cluster.Namespace, cluster.Name)
	if err := r.Get(ctx, types.NamespacedName{Name: agentCRName}, cr); err == nil {
		if err := r.Delete(ctx, cr); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// reconcileRBAC creates necessary RBAC resources
func (r *NovaEdgeClusterReconciler) reconcileRBAC(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	// Controller ServiceAccount
	if err := r.reconcileControllerServiceAccount(ctx, cluster); err != nil {
		return err
	}

	// Controller ClusterRole
	if err := r.reconcileControllerClusterRole(ctx, cluster); err != nil {
		return err
	}

	// Controller ClusterRoleBinding
	if err := r.reconcileControllerClusterRoleBinding(ctx, cluster); err != nil {
		return err
	}

	// Agent ServiceAccount
	if err := r.reconcileAgentServiceAccount(ctx, cluster); err != nil {
		return err
	}

	// Agent ClusterRole
	if err := r.reconcileAgentClusterRole(ctx, cluster); err != nil {
		return err
	}

	// Agent ClusterRoleBinding
	if err := r.reconcileAgentClusterRoleBinding(ctx, cluster); err != nil {
		return err
	}

	return nil
}

func (r *NovaEdgeClusterReconciler) reconcileControllerServiceAccount(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.getControllerServiceAccountName(cluster),
			Namespace: cluster.Namespace,
			Labels:    r.getLabels(cluster, "controller"),
		},
	}

	if err := controllerutil.SetControllerReference(cluster, sa, r.Scheme); err != nil {
		return err
	}

	return r.createOrUpdate(ctx, sa)
}

func (r *NovaEdgeClusterReconciler) reconcileControllerClusterRole(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("%s-%s-controller", cluster.Namespace, cluster.Name),
			Labels: r.getLabels(cluster, "controller"),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"novaedge.io"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
			{
				APIGroups: []string{"networking.k8s.io"},
				Resources: []string{"ingresses", "ingressclasses"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"gateway.networking.k8s.io"},
				Resources: []string{"*"},
				Verbs:     []string{"get", "list", "watch", "update", "patch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"services", "endpoints", "pods", "nodes", "secrets", "configmaps"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"discovery.k8s.io"},
				Resources: []string{"endpointslices"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"create", "patch"},
			},
		},
	}

	return r.createOrUpdate(ctx, cr)
}

func (r *NovaEdgeClusterReconciler) reconcileControllerClusterRoleBinding(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("%s-%s-controller", cluster.Namespace, cluster.Name),
			Labels: r.getLabels(cluster, "controller"),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     fmt.Sprintf("%s-%s-controller", cluster.Namespace, cluster.Name),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      r.getControllerServiceAccountName(cluster),
				Namespace: cluster.Namespace,
			},
		},
	}

	return r.createOrUpdate(ctx, crb)
}

func (r *NovaEdgeClusterReconciler) reconcileAgentServiceAccount(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.getAgentServiceAccountName(cluster),
			Namespace: cluster.Namespace,
			Labels:    r.getLabels(cluster, "agent"),
		},
	}

	if err := controllerutil.SetControllerReference(cluster, sa, r.Scheme); err != nil {
		return err
	}

	return r.createOrUpdate(ctx, sa)
}

func (r *NovaEdgeClusterReconciler) reconcileAgentClusterRole(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("%s-%s-agent", cluster.Namespace, cluster.Name),
			Labels: r.getLabels(cluster, "agent"),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"novaedge.io"},
				Resources: []string{"proxyvips", "proxyvips/status"},
				Verbs:     []string{"get", "list", "watch", "update", "patch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"nodes"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"create", "patch"},
			},
		},
	}

	return r.createOrUpdate(ctx, cr)
}

func (r *NovaEdgeClusterReconciler) reconcileAgentClusterRoleBinding(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("%s-%s-agent", cluster.Namespace, cluster.Name),
			Labels: r.getLabels(cluster, "agent"),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     fmt.Sprintf("%s-%s-agent", cluster.Namespace, cluster.Name),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      r.getAgentServiceAccountName(cluster),
				Namespace: cluster.Namespace,
			},
		},
	}

	return r.createOrUpdate(ctx, crb)
}

// reconcileController creates/updates the controller Deployment
func (r *NovaEdgeClusterReconciler) reconcileController(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	// Create controller service
	if err := r.reconcileControllerService(ctx, cluster); err != nil {
		return err
	}

	// Create controller deployment
	return r.reconcileControllerDeployment(ctx, cluster)
}

func (r *NovaEdgeClusterReconciler) reconcileControllerService(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	grpcPort := int32(9090)
	if cluster.Spec.Controller.GRPCPort != nil {
		grpcPort = *cluster.Spec.Controller.GRPCPort
	}

	metricsPort := int32(8080)
	if cluster.Spec.Controller.MetricsPort != nil {
		metricsPort = *cluster.Spec.Controller.MetricsPort
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-controller", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    r.getLabels(cluster, "controller"),
		},
		Spec: corev1.ServiceSpec{
			Selector: r.getSelectorLabels(cluster, "controller"),
			Ports: []corev1.ServicePort{
				{
					Name:       "grpc",
					Port:       grpcPort,
					TargetPort: intstr.FromInt(int(grpcPort)),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "metrics",
					Port:       metricsPort,
					TargetPort: intstr.FromInt(int(metricsPort)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, svc, r.Scheme); err != nil {
		return err
	}

	return r.createOrUpdate(ctx, svc)
}

func (r *NovaEdgeClusterReconciler) reconcileControllerDeployment(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	replicas := int32(1)
	if cluster.Spec.Controller.Replicas != nil {
		replicas = *cluster.Spec.Controller.Replicas
	}

	image := r.getImage(cluster, "novaedge-controller")

	grpcPort := int32(9090)
	if cluster.Spec.Controller.GRPCPort != nil {
		grpcPort = *cluster.Spec.Controller.GRPCPort
	}

	metricsPort := int32(8080)
	if cluster.Spec.Controller.MetricsPort != nil {
		metricsPort = *cluster.Spec.Controller.MetricsPort
	}

	healthPort := int32(8081)
	if cluster.Spec.Controller.HealthPort != nil {
		healthPort = *cluster.Spec.Controller.HealthPort
	}

	args := []string{
		fmt.Sprintf("--grpc-bind-address=:%d", grpcPort),
		fmt.Sprintf("--metrics-bind-address=:%d", metricsPort),
		fmt.Sprintf("--health-probe-bind-address=:%d", healthPort),
	}

	if cluster.Spec.Controller.LeaderElection == nil || *cluster.Spec.Controller.LeaderElection {
		args = append(args, "--leader-elect=true")
	}

	args = append(args, cluster.Spec.Controller.ExtraArgs...)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-controller", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    r.getLabels(cluster, "controller"),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: r.getSelectorLabels(cluster, "controller"),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: r.getLabels(cluster, "controller"),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: r.getControllerServiceAccountName(cluster),
					NodeSelector:       cluster.Spec.Controller.NodeSelector,
					Tolerations:        cluster.Spec.Controller.Tolerations,
					Affinity:           cluster.Spec.Controller.Affinity,
					ImagePullSecrets:   cluster.Spec.ImagePullSecrets,
					Containers: []corev1.Container{
						{
							Name:            "controller",
							Image:           image,
							ImagePullPolicy: cluster.Spec.ImagePullPolicy,
							Args:            args,
							Ports: []corev1.ContainerPort{
								{
									Name:          "grpc",
									ContainerPort: grpcPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "metrics",
									ContainerPort: metricsPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "health",
									ContainerPort: healthPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: cluster.Spec.Controller.Resources,
							Env:       cluster.Spec.Controller.ExtraEnv,
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(int(healthPort)),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       20,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromInt(int(healthPort)),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							SecurityContext: cluster.Spec.Controller.SecurityContext,
						},
					},
					SecurityContext: cluster.Spec.Controller.PodSecurityContext,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, deployment, r.Scheme); err != nil {
		return err
	}

	return r.createOrUpdate(ctx, deployment)
}

// reconcileAgent creates/updates the agent DaemonSet
func (r *NovaEdgeClusterReconciler) reconcileAgent(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	image := r.getImage(cluster, "novaedge-agent")

	httpPort := int32(80)
	if cluster.Spec.Agent.HTTPPort != nil {
		httpPort = *cluster.Spec.Agent.HTTPPort
	}

	httpsPort := int32(443)
	if cluster.Spec.Agent.HTTPSPort != nil {
		httpsPort = *cluster.Spec.Agent.HTTPSPort
	}

	metricsPort := int32(9090)
	if cluster.Spec.Agent.MetricsPort != nil {
		metricsPort = *cluster.Spec.Agent.MetricsPort
	}

	healthPort := int32(8080)
	if cluster.Spec.Agent.HealthPort != nil {
		healthPort = *cluster.Spec.Agent.HealthPort
	}

	controllerGRPCPort := int32(9090)
	if cluster.Spec.Controller.GRPCPort != nil {
		controllerGRPCPort = *cluster.Spec.Controller.GRPCPort
	}

	hostNetwork := true
	if cluster.Spec.Agent.HostNetwork != nil {
		hostNetwork = *cluster.Spec.Agent.HostNetwork
	}

	dnsPolicy := corev1.DNSClusterFirstWithHostNet
	if cluster.Spec.Agent.DNSPolicy != "" {
		dnsPolicy = cluster.Spec.Agent.DNSPolicy
	}

	args := []string{
		fmt.Sprintf("--controller-address=%s-controller.%s.svc.cluster.local:%d",
			cluster.Name, cluster.Namespace, controllerGRPCPort),
		fmt.Sprintf("--http-port=%d", httpPort),
		fmt.Sprintf("--https-port=%d", httpsPort),
		fmt.Sprintf("--metrics-port=%d", metricsPort),
		fmt.Sprintf("--health-port=%d", healthPort),
	}

	// Add VIP configuration
	if cluster.Spec.Agent.VIP != nil && (cluster.Spec.Agent.VIP.Enabled == nil || *cluster.Spec.Agent.VIP.Enabled) {
		args = append(args, fmt.Sprintf("--vip-mode=%s", cluster.Spec.Agent.VIP.Mode))
		if cluster.Spec.Agent.VIP.Interface != "" {
			args = append(args, fmt.Sprintf("--vip-interface=%s", cluster.Spec.Agent.VIP.Interface))
		}
	}

	args = append(args, cluster.Spec.Agent.ExtraArgs...)

	// Merge volumes
	volumes := []corev1.Volume{}
	volumes = append(volumes, cluster.Spec.Agent.ExtraVolumes...)

	volumeMounts := []corev1.VolumeMount{}
	volumeMounts = append(volumeMounts, cluster.Spec.Agent.ExtraVolumeMounts...)

	// Security context for privileged operations (VIP, network)
	securityContext := cluster.Spec.Agent.SecurityContext
	if securityContext == nil {
		privileged := true
		securityContext = &corev1.SecurityContext{
			Privileged: &privileged,
			Capabilities: &corev1.Capabilities{
				Add: []corev1.Capability{
					"NET_ADMIN",
					"NET_RAW",
					"NET_BIND_SERVICE",
				},
			},
		}
	}

	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-agent", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    r.getLabels(cluster, "agent"),
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: r.getSelectorLabels(cluster, "agent"),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: r.getLabels(cluster, "agent"),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: r.getAgentServiceAccountName(cluster),
					HostNetwork:        hostNetwork,
					DNSPolicy:          dnsPolicy,
					NodeSelector:       cluster.Spec.Agent.NodeSelector,
					Tolerations:        cluster.Spec.Agent.Tolerations,
					ImagePullSecrets:   cluster.Spec.ImagePullSecrets,
					Volumes:            volumes,
					Containers: []corev1.Container{
						{
							Name:            "agent",
							Image:           image,
							ImagePullPolicy: cluster.Spec.ImagePullPolicy,
							Args:            args,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: httpPort,
									HostPort:      httpPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "https",
									ContainerPort: httpsPort,
									HostPort:      httpsPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "metrics",
									ContainerPort: metricsPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources:       cluster.Spec.Agent.Resources,
							Env:             cluster.Spec.Agent.ExtraEnv,
							VolumeMounts:    volumeMounts,
							SecurityContext: securityContext,
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt(int(healthPort)),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       20,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromInt(int(healthPort)),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
						},
					},
					SecurityContext: cluster.Spec.Agent.PodSecurityContext,
				},
			},
		},
	}

	// Apply update strategy
	if cluster.Spec.Agent.UpdateStrategy != nil {
		if cluster.Spec.Agent.UpdateStrategy.Type == "OnDelete" {
			daemonSet.Spec.UpdateStrategy = appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.OnDeleteDaemonSetStrategyType,
			}
		} else {
			maxUnavailable := intstr.FromInt(1)
			if cluster.Spec.Agent.UpdateStrategy.MaxUnavailable != nil {
				maxUnavailable = intstr.FromInt(int(*cluster.Spec.Agent.UpdateStrategy.MaxUnavailable))
			}
			daemonSet.Spec.UpdateStrategy = appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxUnavailable: &maxUnavailable,
				},
			}
		}
	}

	if err := controllerutil.SetControllerReference(cluster, daemonSet, r.Scheme); err != nil {
		return err
	}

	return r.createOrUpdate(ctx, daemonSet)
}

// reconcileWebUI creates/updates the web UI Deployment
func (r *NovaEdgeClusterReconciler) reconcileWebUI(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	// Create web UI service
	if err := r.reconcileWebUIService(ctx, cluster); err != nil {
		return err
	}

	// Create web UI deployment
	return r.reconcileWebUIDeployment(ctx, cluster)
}

func (r *NovaEdgeClusterReconciler) reconcileWebUIService(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	port := int32(9080)
	if cluster.Spec.WebUI.Port != nil {
		port = *cluster.Spec.WebUI.Port
	}

	serviceType := corev1.ServiceTypeClusterIP
	if cluster.Spec.WebUI.Service != nil && cluster.Spec.WebUI.Service.Type != "" {
		serviceType = cluster.Spec.WebUI.Service.Type
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-webui", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    r.getLabels(cluster, "webui"),
		},
		Spec: corev1.ServiceSpec{
			Selector: r.getSelectorLabels(cluster, "webui"),
			Type:     serviceType,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       port,
					TargetPort: intstr.FromInt(int(port)),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	if cluster.Spec.WebUI.Service != nil {
		if cluster.Spec.WebUI.Service.NodePort != nil {
			svc.Spec.Ports[0].NodePort = *cluster.Spec.WebUI.Service.NodePort
		}
		if cluster.Spec.WebUI.Service.Annotations != nil {
			svc.Annotations = cluster.Spec.WebUI.Service.Annotations
		}
	}

	if err := controllerutil.SetControllerReference(cluster, svc, r.Scheme); err != nil {
		return err
	}

	return r.createOrUpdate(ctx, svc)
}

func (r *NovaEdgeClusterReconciler) reconcileWebUIDeployment(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	replicas := int32(1)
	if cluster.Spec.WebUI.Replicas != nil {
		replicas = *cluster.Spec.WebUI.Replicas
	}

	image := r.getImage(cluster, "novactl")

	port := int32(9080)
	if cluster.Spec.WebUI.Port != nil {
		port = *cluster.Spec.WebUI.Port
	}

	args := []string{
		"web",
		fmt.Sprintf("--address=:%d", port),
		"--mode=kubernetes",
	}

	if cluster.Spec.WebUI.ReadOnly != nil && *cluster.Spec.WebUI.ReadOnly {
		args = append(args, "--read-only")
	}

	if cluster.Spec.WebUI.PrometheusEndpoint != "" {
		args = append(args, fmt.Sprintf("--prometheus-endpoint=%s", cluster.Spec.WebUI.PrometheusEndpoint))
	}

	if cluster.Spec.WebUI.TLS != nil && cluster.Spec.WebUI.TLS.Auto != nil && *cluster.Spec.WebUI.TLS.Auto {
		args = append(args, "--tls-auto")
	}

	args = append(args, cluster.Spec.WebUI.ExtraArgs...)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-webui", cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    r.getLabels(cluster, "webui"),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: r.getSelectorLabels(cluster, "webui"),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: r.getLabels(cluster, "webui"),
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: r.getControllerServiceAccountName(cluster),
					NodeSelector:       cluster.Spec.WebUI.NodeSelector,
					Tolerations:        cluster.Spec.WebUI.Tolerations,
					ImagePullSecrets:   cluster.Spec.ImagePullSecrets,
					Containers: []corev1.Container{
						{
							Name:            "webui",
							Image:           image,
							ImagePullPolicy: cluster.Spec.ImagePullPolicy,
							Args:            args,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: port,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Resources: cluster.Spec.WebUI.Resources,
							Env:       cluster.Spec.WebUI.ExtraEnv,
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt(int(port)),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       30,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/",
										Port: intstr.FromInt(int(port)),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, deployment, r.Scheme); err != nil {
		return err
	}

	return r.createOrUpdate(ctx, deployment)
}

// updateStatus updates the NovaEdgeCluster status
func (r *NovaEdgeClusterReconciler) updateStatus(ctx context.Context, cluster *novaedgev1alpha1.NovaEdgeCluster) error {
	logger := log.FromContext(ctx)

	// Fetch controller deployment status
	controllerDeploy := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-controller", cluster.Name),
		Namespace: cluster.Namespace,
	}, controllerDeploy); err == nil {
		cluster.Status.Controller = &novaedgev1alpha1.ComponentStatus{
			Ready:           controllerDeploy.Status.ReadyReplicas == *controllerDeploy.Spec.Replicas,
			Replicas:        *controllerDeploy.Spec.Replicas,
			ReadyReplicas:   controllerDeploy.Status.ReadyReplicas,
			UpdatedReplicas: controllerDeploy.Status.UpdatedReplicas,
			Version:         cluster.Spec.Version,
		}
	}

	// Fetch agent DaemonSet status
	agentDS := &appsv1.DaemonSet{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      fmt.Sprintf("%s-agent", cluster.Name),
		Namespace: cluster.Namespace,
	}, agentDS); err == nil {
		cluster.Status.Agent = &novaedgev1alpha1.ComponentStatus{
			Ready:           agentDS.Status.NumberReady == agentDS.Status.DesiredNumberScheduled,
			Replicas:        agentDS.Status.DesiredNumberScheduled,
			ReadyReplicas:   agentDS.Status.NumberReady,
			UpdatedReplicas: agentDS.Status.UpdatedNumberScheduled,
			Version:         cluster.Spec.Version,
		}
	}

	// Fetch web UI deployment status if enabled
	if cluster.Spec.WebUI != nil && (cluster.Spec.WebUI.Enabled == nil || *cluster.Spec.WebUI.Enabled) {
		webUIDeploy := &appsv1.Deployment{}
		if err := r.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf("%s-webui", cluster.Name),
			Namespace: cluster.Namespace,
		}, webUIDeploy); err == nil {
			cluster.Status.WebUI = &novaedgev1alpha1.ComponentStatus{
				Ready:           webUIDeploy.Status.ReadyReplicas == *webUIDeploy.Spec.Replicas,
				Replicas:        *webUIDeploy.Spec.Replicas,
				ReadyReplicas:   webUIDeploy.Status.ReadyReplicas,
				UpdatedReplicas: webUIDeploy.Status.UpdatedReplicas,
				Version:         cluster.Spec.Version,
			}
		}
	}

	// Update conditions
	controllerReady := cluster.Status.Controller != nil && cluster.Status.Controller.Ready
	agentReady := cluster.Status.Agent != nil && cluster.Status.Agent.Ready
	webUIReady := true
	if cluster.Spec.WebUI != nil && (cluster.Spec.WebUI.Enabled == nil || *cluster.Spec.WebUI.Enabled) {
		webUIReady = cluster.Status.WebUI != nil && cluster.Status.WebUI.Ready
	}

	// Controller ready condition
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeControllerOK,
		Status:             conditionStatus(controllerReady),
		ObservedGeneration: cluster.Generation,
		Reason:             conditionReason(controllerReady, "ControllerReady", "ControllerNotReady"),
		Message:            conditionMessage(controllerReady, "Controller is ready", "Controller is not ready"),
	})

	// Agent ready condition
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeAgentOK,
		Status:             conditionStatus(agentReady),
		ObservedGeneration: cluster.Generation,
		Reason:             conditionReason(agentReady, "AgentReady", "AgentNotReady"),
		Message:            conditionMessage(agentReady, "Agent DaemonSet is ready", "Agent DaemonSet is not ready"),
	})

	// WebUI ready condition (if enabled)
	if cluster.Spec.WebUI != nil && (cluster.Spec.WebUI.Enabled == nil || *cluster.Spec.WebUI.Enabled) {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               ConditionTypeWebUIOK,
			Status:             conditionStatus(webUIReady),
			ObservedGeneration: cluster.Generation,
			Reason:             conditionReason(webUIReady, "WebUIReady", "WebUINotReady"),
			Message:            conditionMessage(webUIReady, "Web UI is ready", "Web UI is not ready"),
		})
	}

	// Overall ready condition
	allReady := controllerReady && agentReady && webUIReady
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               ConditionTypeReady,
		Status:             conditionStatus(allReady),
		ObservedGeneration: cluster.Generation,
		Reason:             conditionReason(allReady, "AllComponentsReady", "SomeComponentsNotReady"),
		Message:            conditionMessage(allReady, "All components are ready", "Some components are not ready"),
	})

	// Update phase
	if allReady {
		cluster.Status.Phase = novaedgev1alpha1.ClusterPhaseRunning
	} else if controllerReady || agentReady {
		cluster.Status.Phase = novaedgev1alpha1.ClusterPhaseDegraded
	} else {
		cluster.Status.Phase = novaedgev1alpha1.ClusterPhaseInitializing
	}

	cluster.Status.Version = cluster.Spec.Version
	cluster.Status.ObservedGeneration = cluster.Generation

	if err := r.Status().Update(ctx, cluster); err != nil {
		logger.Error(err, "Failed to update status")
		return err
	}

	return nil
}

// Helper functions

func (r *NovaEdgeClusterReconciler) getLabels(cluster *novaedgev1alpha1.NovaEdgeCluster, component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "novaedge",
		"app.kubernetes.io/instance":   cluster.Name,
		"app.kubernetes.io/component":  component,
		"app.kubernetes.io/version":    cluster.Spec.Version,
		"app.kubernetes.io/managed-by": "novaedge-operator",
	}
}

func (r *NovaEdgeClusterReconciler) getSelectorLabels(cluster *novaedgev1alpha1.NovaEdgeCluster, component string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "novaedge",
		"app.kubernetes.io/instance":  cluster.Name,
		"app.kubernetes.io/component": component,
	}
}

func (r *NovaEdgeClusterReconciler) getImage(cluster *novaedgev1alpha1.NovaEdgeCluster, component string) string {
	repo := "ghcr.io/piwi3910/novaedge"
	if cluster.Spec.ImageRepository != "" {
		repo = cluster.Spec.ImageRepository
	}
	return fmt.Sprintf("%s/%s:%s", repo, component, cluster.Spec.Version)
}

func (r *NovaEdgeClusterReconciler) getControllerServiceAccountName(cluster *novaedgev1alpha1.NovaEdgeCluster) string {
	if cluster.Spec.Controller.ServiceAccount != nil && cluster.Spec.Controller.ServiceAccount.Name != "" {
		return cluster.Spec.Controller.ServiceAccount.Name
	}
	return fmt.Sprintf("%s-controller", cluster.Name)
}

func (r *NovaEdgeClusterReconciler) getAgentServiceAccountName(cluster *novaedgev1alpha1.NovaEdgeCluster) string {
	if cluster.Spec.Agent.ServiceAccount != nil && cluster.Spec.Agent.ServiceAccount.Name != "" {
		return cluster.Spec.Agent.ServiceAccount.Name
	}
	return fmt.Sprintf("%s-agent", cluster.Name)
}

func (r *NovaEdgeClusterReconciler) createOrUpdate(ctx context.Context, obj client.Object) error {
	logger := log.FromContext(ctx)

	key := client.ObjectKeyFromObject(obj)
	existing := obj.DeepCopyObject().(client.Object)

	if err := r.Get(ctx, key, existing); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating resource", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", key.Name)
			return r.Create(ctx, obj)
		}
		return err
	}

	// Update existing resource
	obj.SetResourceVersion(existing.GetResourceVersion())
	logger.V(1).Info("Updating resource", "kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", key.Name)
	return r.Update(ctx, obj)
}

func conditionStatus(ready bool) metav1.ConditionStatus {
	if ready {
		return metav1.ConditionTrue
	}
	return metav1.ConditionFalse
}

func conditionReason(ready bool, trueReason, falseReason string) string {
	if ready {
		return trueReason
	}
	return falseReason
}

func conditionMessage(ready bool, trueMsg, falseMsg string) string {
	if ready {
		return trueMsg
	}
	return falseMsg
}

// SetupWithManager sets up the controller with the Manager.
func (r *NovaEdgeClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&novaedgev1alpha1.NovaEdgeCluster{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ServiceAccount{}).
		Complete(r)
}
