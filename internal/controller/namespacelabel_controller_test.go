package controller

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	danav1alpha1 "github.com/TalDebi/namespacelabel-assignment.git/api/v1alpha1"
)

var (
	ctx       context.Context
	k8sClient client.Client
	scheme    *runtime.Scheme
)

// Helper function to create the "default" namespace
func createDefaultNamespace() {
	defaultNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
	}
	Expect(k8sClient.Create(ctx, defaultNamespace)).To(Succeed())
}

// Helper function to delete created NamespaceLabel resources
func deleteNamespaceLabels() {
	namespaceLabelList := &danav1alpha1.NamespaceLabelList{}
	Expect(k8sClient.List(ctx, namespaceLabelList)).To(Succeed())
	for _, nl := range namespaceLabelList.Items {
		Expect(k8sClient.Delete(ctx, &nl)).To(Succeed())
	}
}

// Helper function to delete the "default" namespace
func deleteDefaultNamespace() {
	defaultNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
	}
	Expect(k8sClient.Delete(ctx, defaultNamespace)).To(Succeed())
}

var _ = Describe("NamespaceLabel Controller", func() {
	BeforeEach(func() {
		// Initialize a new runtime scheme
		scheme = runtime.NewScheme()

		// Add your CRD and any core Kubernetes API types to the scheme
		Expect(danav1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())

		// Create a new fake client with the initialized scheme
		k8sClient = fake.NewClientBuilder().WithScheme(scheme).Build()

		// Ensure the "default" namespace exists before each test
		createDefaultNamespace()

		// Initialize the context
		ctx = context.Background()
	})

	AfterEach(func() {
		// Clean up created NamespaceLabel resources
		deleteNamespaceLabels()

		// Clean up the "default" namespace
		deleteDefaultNamespace()
	})

	// Tests go here
	Context("When reconciling a NamespaceLabel resource", func() {
		const namespaceName = "default"
		const resourceName = "test-resource"

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespaceName,
		}

		It("should successfully create, update, delete single label, and delete labels", func() {
			By("creating the custom resource for the Kind NamespaceLabel")
			namespaceLabel := &danav1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespaceName,
				},
				Spec: danav1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"label_1": "a", "label_2": "b"},
				},
			}
			Expect(k8sClient.Create(ctx, namespaceLabel)).To(Succeed(), "Failed to create NamespaceLabel resource")

			// Verify resource creation
			created := &danav1alpha1.NamespaceLabel{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: namespaceName}, created)).To(Succeed(), "Failed to get created NamespaceLabel resource")

			By("reconciling the created resource")
			controllerReconciler := &NamespaceLabelReconciler{
				Client: k8sClient,
				Scheme: scheme,
				Log:    zap.New(zap.UseDevMode(true)),
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("checking that the labels were applied to the Namespace")
			namespace := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)).To(Succeed())
			Expect(namespace.Labels).To(HaveKeyWithValue("label_1", "a"))
			Expect(namespace.Labels).To(HaveKeyWithValue("label_2", "b"))

			By("updating the custom resource")
			namespaceLabel.Spec.Labels["label_1"] = "updated"
			Expect(k8sClient.Update(ctx, namespaceLabel)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)).To(Succeed())
			Expect(namespace.Labels).To(HaveKeyWithValue("label_1", "updated"))

			By("deleting a single label from the custom resource")
			delete(namespaceLabel.Spec.Labels, "label_2")
			Expect(k8sClient.Update(ctx, namespaceLabel)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)).To(Succeed())
			Expect(namespace.Labels).NotTo(HaveKey("label_2"))

			By("deleting the custom resource")
			Expect(k8sClient.Delete(ctx, namespaceLabel)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)).To(Succeed())
			Expect(namespace.Labels).NotTo(HaveKey("label_1"))
			Expect(namespace.Labels).NotTo(HaveKey("label_2"))
		})

		It("should prevent creating more than one NamespaceLabel per Namespace", func() {
			By("creating the first NamespaceLabel")
			firstNamespaceLabel := &danav1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "first-resource",
					Namespace: namespaceName,
				},
				Spec: danav1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"label_1": "a"},
				},
			}
			Expect(k8sClient.Create(ctx, firstNamespaceLabel)).To(Succeed())

			By("creating the second NamespaceLabel")
			secondNamespaceLabel := &danav1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "second-resource",
					Namespace: namespaceName,
				},
				Spec: danav1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"label_2": "b"},
				},
			}
			Expect(k8sClient.Create(ctx, secondNamespaceLabel)).To(Succeed())

			By("reconciling the second resource")
			controllerReconciler := &NamespaceLabelReconciler{
				Client: k8sClient,
				Scheme: scheme,
				Log:    zap.New(zap.UseDevMode(true)),
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "second-resource",
					Namespace: namespaceName,
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("only one NamespaceLabel allowed per namespace"))
		})

		It("should prevent creating NamespaceLabel with managed labels", func() {
			By("creating the NamespaceLabel with managed labels")
			namespaceLabel := &danav1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "managed-label-resource",
					Namespace: namespaceName,
				},
				Spec: danav1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"kubernetes.io/managed": "true"},
				},
			}
			Expect(k8sClient.Create(ctx, namespaceLabel)).To(Succeed())

			By("reconciling the resource")
			controllerReconciler := &NamespaceLabelReconciler{
				Client: k8sClient,
				Scheme: scheme,
				Log:    zap.New(zap.UseDevMode(true)),
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "managed-label-resource",
					Namespace: namespaceName,
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot add protected or management label 'kubernetes.io/managed'"))
		})
	})
})
