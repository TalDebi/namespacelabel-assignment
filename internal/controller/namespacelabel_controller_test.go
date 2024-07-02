package controller

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
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

func initTestEnvironment() {
	scheme = runtime.NewScheme()
	Expect(danav1alpha1.AddToScheme(scheme)).To(Succeed())
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	k8sClient = fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx = context.Background()
}

func createNamespace(name string) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
}

func deleteNamespace(name string) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
}

func deleteAllNamespaceLabels() {
	namespaceLabelList := &danav1alpha1.NamespaceLabelList{}
	Expect(k8sClient.List(ctx, namespaceLabelList)).To(Succeed())
	for _, nl := range namespaceLabelList.Items {
		Expect(k8sClient.Delete(ctx, &nl)).To(Succeed())
	}
}

var _ = Describe("NamespaceLabel Controller", func() {
	BeforeEach(func() {
		initTestEnvironment()
		createNamespace("default")
	})

	AfterEach(func() {
		deleteAllNamespaceLabels()
		deleteNamespace("default")
	})

	Context("When reconciling a NamespaceLabel resource", func() {
		const namespaceName = "default"
		const resourceName = "test-resource"
		namespacedName := types.NamespacedName{Name: resourceName, Namespace: namespaceName}

		It("should successfully create, update, delete labels and delete NamespaceLabels", func() {
			By("creating the NamespaceLabel resource")
			namespaceLabel := &danav1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: namespaceName},
				Spec: danav1alpha1.NamespaceLabelSpec{
					Labels: map[string]string{"label_1": "a", "label_2": "b"},
				},
			}
			Expect(k8sClient.Create(ctx, namespaceLabel)).To(Succeed())

			created := &danav1alpha1.NamespaceLabel{}
			Expect(k8sClient.Get(ctx, namespacedName, created)).To(Succeed())

			By("reconciling the created resource")
			controllerReconciler := &NamespaceLabelReconciler{
				Client: k8sClient,
				Scheme: scheme,
				Log:    zap.New(zap.UseDevMode(true)),
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("checking that the labels were applied to the Namespace")
			namespace := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)).To(Succeed())
			Expect(namespace.Labels).To(HaveKeyWithValue("label_1", "a"))
			Expect(namespace.Labels).To(HaveKeyWithValue("label_2", "b"))

			By("updating the NamespaceLabel resource")
			retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				if err := k8sClient.Get(ctx, namespacedName, namespaceLabel); err != nil {
					return err
				}
				namespaceLabel.Spec.Labels["label_1"] = "updated"
				return k8sClient.Update(ctx, namespaceLabel)
			})
			Expect(retryErr).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)).To(Succeed())
			Expect(namespace.Labels).To(HaveKeyWithValue("label_1", "updated"))

			By("deleting a single label from the NamespaceLabel resource")
			delete(namespaceLabel.Spec.Labels, "label_2")
			Expect(k8sClient.Update(ctx, namespaceLabel)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)).To(Succeed())
			Expect(namespace.Labels).NotTo(HaveKey("label_2"))

			By("deleting the NamespaceLabel resource")
			Expect(k8sClient.Delete(ctx, namespaceLabel)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)).To(Succeed())
			Expect(namespace.Labels).NotTo(HaveKey("label_1"))
		})

		It("should prevent creating more than one NamespaceLabel per Namespace", func() {
			By("creating the first NamespaceLabel")
			firstNamespaceLabel := &danav1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "first-resource", Namespace: namespaceName},
				Spec:       danav1alpha1.NamespaceLabelSpec{Labels: map[string]string{"label_1": "a"}},
			}
			Expect(k8sClient.Create(ctx, firstNamespaceLabel)).To(Succeed())

			By("creating the second NamespaceLabel")
			secondNamespaceLabel := &danav1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "second-resource", Namespace: namespaceName},
				Spec:       danav1alpha1.NamespaceLabelSpec{Labels: map[string]string{"label_2": "b"}},
			}
			Expect(k8sClient.Create(ctx, secondNamespaceLabel)).To(Succeed())

			By("reconciling the second resource")
			controllerReconciler := &NamespaceLabelReconciler{
				Client: k8sClient,
				Scheme: scheme,
				Log:    zap.New(zap.UseDevMode(true)),
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "second-resource", Namespace: namespaceName}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("only one NamespaceLabel allowed per namespace"))
		})

		It("should prevent creating NamespaceLabel with managed labels", func() {
			By("creating the NamespaceLabel with managed labels")
			namespaceLabel := &danav1alpha1.NamespaceLabel{
				ObjectMeta: metav1.ObjectMeta{Name: "managed-label-resource", Namespace: namespaceName},
				Spec:       danav1alpha1.NamespaceLabelSpec{Labels: map[string]string{"kubernetes.io/managed": "true"}},
			}
			Expect(k8sClient.Create(ctx, namespaceLabel)).To(Succeed())

			By("reconciling the resource")
			controllerReconciler := &NamespaceLabelReconciler{
				Client: k8sClient,
				Scheme: scheme,
				Log:    zap.New(zap.UseDevMode(true)),
			}
			_, err := controllerReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "managed-label-resource", Namespace: namespaceName}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot add protected or management label 'kubernetes.io/managed'"))
		})
	})
})
