/*
Copyright 2024.

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
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	danav1alpha1 "github.com/TalDebi/namespacelabel-assignment.git/api/v1alpha1"
	"github.com/go-logr/logr"
)

// NamespaceLabelReconciler reconciles a NamespaceLabel object
type NamespaceLabelReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

const (
	finalizerName         = "namespacelabel.finalizers.dana.io/finalizer"
	managementLabelPrefix = "kubernetes.io"
)

//manager
// +kubebuilder:rbac:groups=dana.dana.io,resources=namespacelabels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dana.dana.io,resources=namespacelabels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dana.dana.io,resources=namespacelabels/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//tenants
// +kubebuilder:rbac:groups=dana.dana.io,resources=namespacelabels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dana.dana.io,resources=namespacelabels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dana.dana.io,resources=namespacelabels/finalizers,verbs=update
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

func (r *NamespaceLabelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	log := log.FromContext(ctx)

	log.Info("Starting reconciliation for NamespaceLabel", "Namespace", req.Namespace, "Name", req.Name)

	// Fetch the NamespaceLabel instance
	namespaceLabel := &danav1alpha1.NamespaceLabel{}
	if err := r.Get(ctx, req.NamespacedName, namespaceLabel); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Fetched NamespaceLabel", "NamespaceLabel", namespaceLabel)

	// Fetch the Namespace instance
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: req.Namespace}, ns); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion
	if namespaceLabel.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(namespaceLabel, finalizerName) {
			controllerutil.AddFinalizer(namespaceLabel, finalizerName)
			if err := r.Update(ctx, namespaceLabel); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if controllerutil.ContainsFinalizer(namespaceLabel, finalizerName) {
			r.handleDeletion(ctx, namespaceLabel, ns)
		}

		return ctrl.Result{}, nil
	}

	// Ensure only one NamespaceLabel per namespace
	existingNamespaceLabels := &danav1alpha1.NamespaceLabelList{}
	if err := r.List(ctx, existingNamespaceLabels, client.InNamespace(req.Namespace)); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Existing NamespaceLabels", "Count", len(existingNamespaceLabels.Items))

	if len(existingNamespaceLabels.Items) > 1 {
		var err = fmt.Errorf("only one NamespaceLabel allowed per namespace")
		r.updateStatus(ctx, namespaceLabel, "NamespaceLabelsConflict", metav1.ConditionFalse, "Conflict", err.Error())
		return ctrl.Result{}, err
	}

	log.Info("Creating nsl")

	// Reconcile the namespace labels
	if err := r.reconcileNamespaceLabels(ctx, namespaceLabel, ns); err != nil {
		r.updateStatus(ctx, namespaceLabel, "UpdateFailed", metav1.ConditionFalse, "UpdateError", err.Error())
		return ctrl.Result{}, err
	}

	r.updateStatus(ctx, namespaceLabel, "LabelsApplied", metav1.ConditionTrue, "Success", "Namespace labels have been successfully updated")
	log.Info("nsl Created")

	return ctrl.Result{}, nil
}

func (r *NamespaceLabelReconciler) handleDeletion(
	ctx context.Context, namespaceLabel *danav1alpha1.NamespaceLabel, ns *corev1.Namespace) (ctrl.Result, error) {
	// Remove labels managed by this NamespaceLabel
	for key := range namespaceLabel.Spec.Labels {
		delete(ns.Labels, key)
	}
	if err := r.Update(ctx, ns); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(namespaceLabel, finalizerName)
	if err := r.Update(ctx, namespaceLabel); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func isManagementLabel(label string) bool {
	return strings.HasPrefix(label, managementLabelPrefix)
}

func (r *NamespaceLabelReconciler) reconcileNamespaceLabels(
	ctx context.Context, namespaceLabel *danav1alpha1.NamespaceLabel, ns *corev1.Namespace) error {

	// Track labels to be added and removed
	labelsToAdd := make(map[string]string)
	labelsToRemove := make(map[string]struct{})

	// Collect labels to add or update
	for key, value := range namespaceLabel.Spec.Labels {
		labelsToAdd[key] = value
	}

	// Ensure labels are not management labels
	for key := range labelsToAdd {
		if isManagementLabel(key) {
			return fmt.Errorf("cannot add protected or management label '%s'", key)
		}
	}

	// Collect labels to remove
	for key := range ns.Labels {
		if _, exists := labelsToAdd[key]; !exists && !isManagementLabel(key) {
			labelsToRemove[key] = struct{}{}
		}
	}

	// Remove labels that are no longer present in NamespaceLabel
	for key := range labelsToRemove {
		delete(ns.Labels, key)
	}

	// Apply labels to be added or updated
	for key, value := range labelsToAdd {
		ns.Labels[key] = value
	}

	// Update Namespace with new labels
	if err := r.Update(ctx, ns); err != nil {
		return err
	}

	return nil
}

func (r *NamespaceLabelReconciler) updateStatus(ctx context.Context, namespaceLabel *danav1alpha1.NamespaceLabel, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	// Update or append condition
	namespaceLabel.Status.Conditions = updateNewCondition(namespaceLabel.Status.Conditions, condition)

	// Update status
	if err := r.Status().Update(ctx, namespaceLabel); err != nil {
		r.Log.Error(err, "Failed to update NamespaceLabel status")
	}
}

// updateNewCondition appends a new condition or updates an existing one in the slice of conditions
func updateNewCondition(conditions []metav1.Condition, newCondition metav1.Condition) []metav1.Condition {
	for index := range conditions {
		if conditions[index].Type == newCondition.Type {
			conditions[index] = newCondition
			return conditions
		}
	}
	return append(conditions, newCondition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NamespaceLabelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&danav1alpha1.NamespaceLabel{}).
		Complete(r)
}
