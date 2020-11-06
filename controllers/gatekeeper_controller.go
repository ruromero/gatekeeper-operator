/*


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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	operatorv1alpha1 "github.com/font/gatekeeper-operator/api/v1alpha1"
	"github.com/font/gatekeeper-operator/controllers/merge"
	"github.com/font/gatekeeper-operator/pkg/bindata"
	"github.com/go-logr/logr"
	"github.com/openshift/library-go/pkg/manifest"
	"github.com/pkg/errors"
	admregv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var (
	defaultGatekeeperCrName        = "gatekeeper"
	staticAssetsDir                = "config/gatekeeper/"
	auditFile                      = "apps_v1_deployment_gatekeeper-audit.yaml"
	webhookFile                    = "apps_v1_deployment_gatekeeper-controller-manager.yaml"
	validatingWebhookConfiguration = "admissionregistration.k8s.io_v1beta1_validatingwebhookconfiguration_gatekeeper-validating-webhook-configuration.yaml"
	orderedStaticAssets            = []string{
		"apiextensions.k8s.io_v1beta1_customresourcedefinition_configs.config.gatekeeper.sh.yaml",
		"apiextensions.k8s.io_v1beta1_customresourcedefinition_constrainttemplates.templates.gatekeeper.sh.yaml",
		"apiextensions.k8s.io_v1beta1_customresourcedefinition_constrainttemplatepodstatuses.status.gatekeeper.sh.yaml",
		"apiextensions.k8s.io_v1beta1_customresourcedefinition_constraintpodstatuses.status.gatekeeper.sh.yaml",
		"~g_v1_secret_gatekeeper-webhook-server-cert.yaml",
		"~g_v1_serviceaccount_gatekeeper-admin.yaml",
		"rbac.authorization.k8s.io_v1_clusterrole_gatekeeper-manager-role.yaml",
		"rbac.authorization.k8s.io_v1_clusterrolebinding_gatekeeper-manager-rolebinding.yaml",
		"rbac.authorization.k8s.io_v1_role_gatekeeper-manager-role.yaml",
		"rbac.authorization.k8s.io_v1_rolebinding_gatekeeper-manager-rolebinding.yaml",
		auditFile,
		webhookFile,
		"~g_v1_service_gatekeeper-webhook-service.yaml",
		validatingWebhookConfiguration,
	}
	validationGatekeeperWebhook = "validation.gatekeeper.sh"
)

const (
	gatekeeperFinalizer = "finalizer.operator.gatekeeper.sh"
)

// GatekeeperReconciler reconciles a Gatekeeper object
type GatekeeperReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// Gatekeeper Operator RBAC permissions to manager Gatekeeper custom resource
// +kubebuilder:rbac:groups=operator.gatekeeper.sh,namespace="system",resources=gatekeepers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operator.gatekeeper.sh,namespace="system",resources=gatekeepers/status,verbs=get;update;patch

// Gatekeeper Operator RBAC permissions to deploy Gatekeeper. Many of these
// RBAC permissions are needed because the operator must have the permissions
// to grant Gatekeeper its required RBAC permissions.

// Cluster Scoped
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.gatekeeper.sh,resources=configs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.gatekeeper.sh,resources=configs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=constraints.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=podsecuritypolicies,verbs=use
// +kubebuilder:rbac:groups=status.gatekeeper.sh,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates/finalizers,verbs=get;update;patch;delete
// +kubebuilder:rbac:groups=templates.gatekeeper.sh,resources=constrainttemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete

// Namespace Scoped
// +kubebuilder:rbac:groups=core,namespace="system",resources=secrets;serviceaccounts;services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,namespace="system",resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,namespace="system",resources=deployments,verbs=get;list;watch;create;update;patch;delete

func (r *GatekeeperReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("gatekeeper", req.NamespacedName)
	logger.Info("Reconciling Gatekeeper")

	if req.Name != defaultGatekeeperCrName {
		err := fmt.Errorf("Gatekeeper resource name must be '%s'", defaultGatekeeperCrName)
		logger.Error(err, "Invalid Gatekeeper resource name")
		// Return success to avoid requeue
		return ctrl.Result{}, nil
	}

	gatekeeper := &operatorv1alpha1.Gatekeeper{}
	err := r.Get(ctx, req.NamespacedName, gatekeeper)
	if err != nil {
		if apierrors.IsNotFound(err) {

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	isGatekeeperMarkedToBeDeleted := gatekeeper.GetDeletionTimestamp() != nil
	if isGatekeeperMarkedToBeDeleted {
		if sets.NewString(gatekeeper.GetFinalizers()...).Has(gatekeeperFinalizer) {

			if err := r.finalizeGatekeeper(logger, gatekeeper); err != nil {
				return ctrl.Result{}, err
			}

			controllerutil.RemoveFinalizer(gatekeeper, gatekeeperFinalizer)
			err := r.Update(ctx, gatekeeper)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !sets.NewString(gatekeeper.GetFinalizers()...).Has(gatekeeperFinalizer) {
		if err := r.addFinalizer(logger, gatekeeper); err != nil {
			return ctrl.Result{}, err
		}
	}

	err = r.deployGatekeeperResources(gatekeeper)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "Unable to deploy Gatekeeper resources")
	}

	return ctrl.Result{}, nil
}

func (r *GatekeeperReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.Gatekeeper{}).
		WithEventFilter(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldGeneration := e.MetaOld.GetGeneration()
				newGeneration := e.MetaNew.GetGeneration()

				return oldGeneration != newGeneration
			},
			DeleteFunc: func(e event.DeleteEvent) bool {

				return false
			},
		}).
		Complete(r)
}

func (r *GatekeeperReconciler) deployGatekeeperResources(gatekeeper *operatorv1alpha1.Gatekeeper) error {
	for _, a := range orderedStaticAssets {
		manifest, err := getManifest(a)
		if err != nil {
			return err
		}
		if err = crOverrides(gatekeeper, a, manifest); err != nil {
			return err
		}

		if err = r.updateOrCreateResource(manifest, gatekeeper); err != nil {
			return err
		}
	}
	return nil
}

func getManifest(asset string) (*manifest.Manifest, error) {
	manifest := &manifest.Manifest{}
	assetName := staticAssetsDir + asset
	bytes, err := bindata.Asset(assetName)
	if err != nil {
		return manifest, errors.Wrapf(err, "Unable to retrieve bindata asset %s", assetName)
	}

	err = manifest.UnmarshalJSON(bytes)
	if err != nil {
		return manifest, errors.Wrapf(err, "Unable to unmarshal YAML bytes for asset name %s", assetName)
	}
	return manifest, nil
}

func (r *GatekeeperReconciler) updateOrCreateResource(manifest *manifest.Manifest, gatekeeper *operatorv1alpha1.Gatekeeper) error {
	var err error
	ctx := context.Background()
	clusterObj := &unstructured.Unstructured{}
	clusterObj.SetAPIVersion(manifest.Obj.GetAPIVersion())
	clusterObj.SetKind(manifest.Obj.GetKind())

	namespacedName := types.NamespacedName{
		Namespace: manifest.Obj.GetNamespace(),
		Name:      manifest.Obj.GetName(),
	}

	logger := r.Log.WithValues("Gatekeeper resource", namespacedName)

	if manifest.Obj.GetNamespace() != "" {
		err = ctrl.SetControllerReference(gatekeeper, manifest.Obj, r.Scheme)
		if err != nil {
			return errors.Wrapf(err, "Unable to set controller reference for %s", namespacedName)
		}
	}

	err = r.Get(ctx, namespacedName, clusterObj)

	switch {
	case err == nil:
		err = merge.RetainClusterObjectFields(manifest.Obj, clusterObj)
		if err != nil {
			return errors.Wrapf(err, "Unable to retain cluster object fields from %s", namespacedName)
		}

		err = r.Update(ctx, manifest.Obj)
		if err != nil {
			return errors.Wrapf(err, "Error attempting to update resource %s", namespacedName)
		}

		logger.Info(fmt.Sprintf("Updated Gatekeeper resource"))

	case apierrors.IsNotFound(err):
		err = r.Create(ctx, manifest.Obj)
		if err != nil {
			return errors.Wrapf(err, "Error attempting to create resource %s", namespacedName)
		}
		logger.Info(fmt.Sprintf("Created Gatekeeper resource"))

	case err != nil:
		return errors.Wrapf(err, "Error attempting to get resource %s", namespacedName)
	}

	return err
}

func (r *GatekeeperReconciler) finalizeGatekeeper(reqLogger logr.Logger, gatekeeper *operatorv1alpha1.Gatekeeper) error {
	ctx := context.Background()

	var err error
	for _, a := range orderedStaticAssets {
		assetName := staticAssetsDir + a
		bytes, err := bindata.Asset(assetName)
		if err != nil {
			return errors.Wrapf(err, "Unable to retrieve bindata asset %s", assetName)
		}

		manifest := &manifest.Manifest{}
		err = manifest.UnmarshalJSON(bytes)
		if err != nil {
			return errors.Wrapf(err, "Unable to unmarshal YAML bytes for asset name %s", assetName)
		}

		// Delete cluster scoped resource not owned by the CR
		if manifest.Obj.GetNamespace() == "" {

			err = r.Delete(ctx, manifest.Obj)
			if err != nil && !apierrors.IsNotFound(err) {
				return errors.Wrapf(err, "Error Deleting Finalizer Resource. Kind: '%s'. Name: '%s'", manifest.GVK.Kind, manifest.Obj.GetName())
			}
		}

	}

	reqLogger.Info("Successfully finalized Gatekeeper")
	return err
}

func (r *GatekeeperReconciler) addFinalizer(reqLogger logr.Logger, g *operatorv1alpha1.Gatekeeper) error {
	ctx := context.Background()

	controllerutil.AddFinalizer(g, gatekeeperFinalizer)

	// Update CR
	err := r.Update(ctx, g)
	if err != nil {
		return errors.Wrapf(err, "Failed to update Gatekeeper with finalizer. Name: '%s'", g.Name)
	}
	return nil
}

var commonSpecOverridesFn = []func(*unstructured.Unstructured, operatorv1alpha1.GatekeeperSpec) error{setAffinity, setNodeSelector, setPodAnnotations, setTolerations, containerOverrides}
var commonContainerOverridesFn = []func(map[string]interface{}, operatorv1alpha1.GatekeeperSpec) error{setResources, setImage}

// crOverrides
func crOverrides(gatekeeper *operatorv1alpha1.Gatekeeper, asset string, manifest *manifest.Manifest) error {
	// audit overrides
	if asset == auditFile {
		if err := commonOverrides(manifest.Obj, gatekeeper.Spec); err != nil {
			return err
		}
		if err := auditOverrides(manifest.Obj, gatekeeper.Spec.Audit); err != nil {
			return err
		}
	}
	// webhook overrides
	if asset == webhookFile {
		if err := commonOverrides(manifest.Obj, gatekeeper.Spec); err != nil {
			return err
		}
		if err := webhookOverrides(manifest.Obj, gatekeeper.Spec.Webhook); err != nil {
			return err
		}
	}
	// validatingWebhookConfiguration overrides
	if asset == validatingWebhookConfiguration {
		if err := validatingWebhookConfigurationOverrides(manifest.Obj, gatekeeper.Spec.Webhook); err != nil {
			return err
		}
	}
	return nil
}

func commonOverrides(obj *unstructured.Unstructured, spec operatorv1alpha1.GatekeeperSpec) error {
	for _, f := range commonSpecOverridesFn {
		if err := f(obj, spec); err != nil {
			return err
		}
	}
	return nil
}

func auditOverrides(obj *unstructured.Unstructured, audit *operatorv1alpha1.AuditConfig) error {
	if audit != nil {
		if err := setReplicas(obj, audit.Replicas); err != nil {
			return err
		}
	}
	return nil
}

func webhookOverrides(obj *unstructured.Unstructured, webhook *operatorv1alpha1.WebhookConfig) error {
	if webhook != nil {
		if err := setReplicas(obj, webhook.Replicas); err != nil {
			return err
		}
	}
	return nil
}

func validatingWebhookConfigurationOverrides(obj *unstructured.Unstructured, webhook *operatorv1alpha1.WebhookConfig) error {
	if webhook != nil {
		if err := setFailurePolicy(obj, webhook.FailurePolicy); err != nil {
			return err
		}
	}
	return nil
}

func containerOverrides(obj *unstructured.Unstructured, spec operatorv1alpha1.GatekeeperSpec) error {
	containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return errors.Wrapf(err, "Failed to retrieve containers")
	}

	for _, c := range containers {
		container := c.(map[string]interface{})
		for _, f := range commonContainerOverridesFn {
			if err := f(container, spec); err != nil {
				return err
			}
		}
	}
	if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
		return errors.Wrapf(err, "Failed to set containers")
	}
	return nil
}

func setReplicas(obj *unstructured.Unstructured, replicas *int64) error {
	if replicas != nil {
		if err := unstructured.SetNestedField(obj.Object, *replicas, "spec", "replicas"); err != nil {
			return errors.Wrapf(err, "Failed to set replica value")
		}
	}
	return nil
}

func setFailurePolicy(obj *unstructured.Unstructured, failurePolicy *admregv1.FailurePolicyType) error {
	if failurePolicy != nil {
		webhooks, found, err := unstructured.NestedSlice(obj.Object, "webhooks")
		if err != nil || !found {
			return errors.Wrapf(err, "Failed to retrieve webhooks definition")
		}
		for _, w := range webhooks {
			webhook := w.(map[string]interface{})
			if webhook["name"] == validationGatekeeperWebhook {
				unstructured.SetNestedField(webhook, string(*failurePolicy), "failurePolicy")
			}
		}
		if err := unstructured.SetNestedSlice(obj.Object, webhooks, "webhooks"); err != nil {
			return errors.Wrapf(err, "Failed to set webhooks")
		}
	}
	return nil
}

// Generic setters

func setAffinity(obj *unstructured.Unstructured, spec operatorv1alpha1.GatekeeperSpec) error {
	if spec.Affinity != nil {
		if err := unstructured.SetNestedField(obj.Object, toMap(spec.Affinity), "spec", "template", "spec", "affinity"); err != nil {
			return errors.Wrapf(err, "Failed to set affinity value")
		}
	}
	return nil
}

func setNodeSelector(obj *unstructured.Unstructured, spec operatorv1alpha1.GatekeeperSpec) error {
	if spec.NodeSelector != nil {
		if err := unstructured.SetNestedStringMap(obj.Object, spec.NodeSelector, "spec", "template", "spec", "nodeSelector"); err != nil {
			return errors.Wrapf(err, "Failed to set nodeSelector value")
		}
	}
	return nil
}

func setPodAnnotations(obj *unstructured.Unstructured, spec operatorv1alpha1.GatekeeperSpec) error {
	if spec.PodAnnotations != nil {
		if err := unstructured.SetNestedStringMap(obj.Object, spec.PodAnnotations, "spec", "template", "metadata", "annotations"); err != nil {
			return errors.Wrapf(err, "Failed to set podAnnotations")
		}
	}
	return nil
}

func setTolerations(obj *unstructured.Unstructured, spec operatorv1alpha1.GatekeeperSpec) error {
	if spec.Tolerations != nil {
		tolerations := make([]interface{}, len(spec.Tolerations))
		for i, t := range spec.Tolerations {
			tolerations[i] = toMap(t)
		}
		if err := unstructured.SetNestedSlice(obj.Object, tolerations, "spec", "template", "spec", "tolerations"); err != nil {
			return errors.Wrapf(err, "Failed to set container resources")
		}
	}
	return nil
}

// Container specific setters

func setResources(container map[string]interface{}, spec operatorv1alpha1.GatekeeperSpec) error {
	if spec.Resources != nil {
		if err := unstructured.SetNestedField(container, toMap(spec.Resources), "resources"); err != nil {
			return errors.Wrapf(err, "Failed to set container resources")
		}
	}
	return nil
}

func setImage(container map[string]interface{}, spec operatorv1alpha1.GatekeeperSpec) error {
	if spec.Image == nil {
		return nil
	}
	if spec.Image.Image != nil {
		if err := unstructured.SetNestedField(container, *spec.Image.Image, "image"); err != nil {
			return errors.Wrapf(err, "Failed to set container image")
		}
	}
	if spec.Image.ImagePullPolicy != nil {
		if err := unstructured.SetNestedField(container, string(*spec.Image.ImagePullPolicy), "imagePullPolicy"); err != nil {
			return errors.Wrapf(err, "Failed to set container image pull policy")
		}
	}
	return nil
}

// toMap Convenience method to convert any struct into a map
func toMap(obj interface{}) map[string]interface{} {
	var result map[string]interface{}
	resultRec, _ := json.Marshal(obj)
	json.Unmarshal(resultRec, &result)
	return result
}
