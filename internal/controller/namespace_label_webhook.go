package controller

import (
	"context"
	"net/http"

	danav1alpha1 "github.com/TalDebi/namespacelabel-assignment.git/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type NamespaceLabelValidator struct {
	Client  client.Client
	decoder *admission.Decoder
}

func (v *NamespaceLabelValidator) Handle(ctx context.Context, req admission.Request) admission.Response {
	log := log.FromContext(ctx)
	log.Info("Started calling webhook: %s\n", "Namespace", req.Namespace, "Name", req.Name)
	namespaceLabel := &danav1alpha1.NamespaceLabel{}

	err := (*v.decoder).Decode(req, namespaceLabel)
	if err != nil {
		log.Error(err, "Error decoding request: %v\n")
		return admission.Errored(http.StatusBadRequest, err)
	}

	// Ensure only one NamespaceLabel per namespace
	existingNamespaceLabels := &danav1alpha1.NamespaceLabelList{}
	if err := v.Client.List(ctx, existingNamespaceLabels, client.InNamespace(req.Namespace)); err != nil {
		log.Error(err, "Error listing existing labels: %v\n")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	if len(existingNamespaceLabels.Items) > 0 {
		return admission.Denied("only one NamespaceLabel allowed per namespace")
	}

	// Ensure labels are not management labels
	for key := range namespaceLabel.Spec.Labels {
		if isManagementLabel(key) {
			return admission.Denied("cannot add protected or management label")
		}
	}

	return admission.Allowed("")
}

func (v *NamespaceLabelValidator) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

func SetupWebhookWithManager(mgr ctrl.Manager) error {
	validator := &NamespaceLabelValidator{
		Client: mgr.GetClient(),
	}

	mgr.GetWebhookServer().Register("/validate-namespacelabel", &admission.Webhook{
		Handler: validator,
	})

	return nil
}
