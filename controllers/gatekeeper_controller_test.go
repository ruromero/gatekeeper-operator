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
	"testing"

	operatorv1alpha1 "github.com/font/gatekeeper-operator/api/v1alpha1"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/manifest"
	admregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var (
	auditReplicas   = int64(1)
	webhookReplicas = int64(3)
)

func TestReplicas(t *testing.T) {
	g := NewWithT(t)
	auditReplicaOverride := int64(4)
	webhookReplicaOverride := int64(7)
	gatekeeper := &operatorv1alpha1.Gatekeeper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "testns",
		},
	}
	// test default audit replicas
	auditManifest, err := getManifest(auditFile)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(auditManifest).ToNot(BeNil())
	testManifestReplicas(g, auditManifest, auditReplicas)
	// test nil audit replicas
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	testManifestReplicas(g, auditManifest, auditReplicas)
	// test audit replicas override
	gatekeeper.Spec.Audit = &operatorv1alpha1.AuditConfig{Replicas: &auditReplicaOverride}
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	testManifestReplicas(g, auditManifest, auditReplicaOverride)

	// test default webhook replicas
	webhookManifest, err := getManifest(webhookFile)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(webhookManifest).ToNot(BeNil())
	testManifestReplicas(g, webhookManifest, webhookReplicas)
	// test nil webhook replicas
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	testManifestReplicas(g, webhookManifest, webhookReplicas)
	// test webhook replicas override
	gatekeeper.Spec.Webhook = &operatorv1alpha1.WebhookConfig{Replicas: &webhookReplicaOverride}
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	testManifestReplicas(g, webhookManifest, webhookReplicaOverride)
}

func testManifestReplicas(g *WithT, manifest *manifest.Manifest, expectedReplicas int64) {
	replicas, found, err := unstructured.NestedInt64(manifest.Obj.Object, "spec", "replicas")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(replicas).To(BeIdenticalTo(expectedReplicas))
}

func TestAffinity(t *testing.T) {
	g := NewWithT(t)
	affinity := &corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"auditKey": "auditValue",
						},
					},
				},
			},
		},
	}
	gatekeeper := &operatorv1alpha1.Gatekeeper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "testns",
		},
	}
	// test default affinity
	auditManifest, err := getManifest(auditFile)
	webhookManifest, err := getManifest(webhookFile)
	g.Expect(err).ToNot(HaveOccurred())
	assertAuditAffinity(g, auditManifest, nil)
	assertWebhookAffinity(g, webhookManifest, nil)

	// test nil affinity
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertAuditAffinity(g, auditManifest, nil)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertWebhookAffinity(g, webhookManifest, nil)

	// test affinity override
	gatekeeper.Spec.Affinity = affinity

	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertAuditAffinity(g, auditManifest, affinity)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertWebhookAffinity(g, webhookManifest, affinity)
}

func assertAuditAffinity(g *WithT, manifest *manifest.Manifest, expected *corev1.Affinity) {
	current, found, err := unstructured.NestedFieldCopy(manifest.Obj.Object, "spec", "template", "spec", "affinity")
	g.Expect(err).ToNot(HaveOccurred())
	if expected == nil {
		g.Expect(found).To(BeFalse())
	} else {
		g.Expect(found).To(BeTrue())
		assertAffinity(g, expected, current)
	}
}

func assertWebhookAffinity(g *WithT, manifest *manifest.Manifest, expected *corev1.Affinity) {
	defaultConfig := &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
				{
					Weight: 100,
					PodAffinityTerm: corev1.PodAffinityTerm{
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "gatekeeper.sh/operation",
									Operator: metav1.LabelSelectorOpIn,
									Values: []string{
										"webhook",
									},
								},
							},
						},
						TopologyKey: "kubernetes.io/hostname",
					},
				},
			},
		},
	}
	current, found, err := unstructured.NestedFieldCopy(manifest.Obj.Object, "spec", "template", "spec", "affinity")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())
	if expected == nil {
		assertAffinity(g, defaultConfig, current)
	} else {
		assertAffinity(g, expected, current)
	}
}

func assertAffinity(g *WithT, expected *corev1.Affinity, current interface{}) {
	g.Expect(toMap(expected)).To(BeEquivalentTo(toMap(current)))
}

func TestNodeSelector(t *testing.T) {
	g := NewWithT(t)
	nodeSelector := map[string]string{
		"region": "emea",
	}

	gatekeeper := &operatorv1alpha1.Gatekeeper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "testns",
		},
	}
	// test default nodeSelector
	auditManifest, err := getManifest(auditFile)
	webhookManifest, err := getManifest(webhookFile)
	g.Expect(err).ToNot(HaveOccurred())
	assertNodeSelector(g, auditManifest, nil)
	assertNodeSelector(g, webhookManifest, nil)

	// test nil nodeSelector
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertNodeSelector(g, auditManifest, nil)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertNodeSelector(g, webhookManifest, nil)

	// test nodeSelector override
	gatekeeper.Spec.NodeSelector = nodeSelector
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertNodeSelector(g, auditManifest, nodeSelector)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertNodeSelector(g, webhookManifest, nodeSelector)
}

func assertNodeSelector(g *WithT, manifest *manifest.Manifest, expected map[string]string) {
	defaultConfig := map[string]string{
		"kubernetes.io/os": "linux",
	}
	current, found, err := unstructured.NestedStringMap(manifest.Obj.Object, "spec", "template", "spec", "nodeSelector")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())
	if expected == nil {
		g.Expect(defaultConfig).To(BeEquivalentTo(current))
	} else {
		g.Expect(expected).To(BeEquivalentTo(current))
	}
}

func TestPodAnnotations(t *testing.T) {
	g := NewWithT(t)
	podAnnotations := map[string]string{
		"my.annotation/foo":         "example",
		"some.other.annotation/bar": "baz",
	}

	gatekeeper := &operatorv1alpha1.Gatekeeper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "testns",
		},
	}
	// test default podAnnotations
	auditManifest, err := getManifest(auditFile)
	webhookManifest, err := getManifest(webhookFile)
	g.Expect(err).ToNot(HaveOccurred())
	assertPodAnnotations(g, auditManifest, nil)
	assertPodAnnotations(g, webhookManifest, nil)

	// test nil podAnnotations
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertPodAnnotations(g, auditManifest, nil)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertPodAnnotations(g, webhookManifest, nil)

	// test podAnnotations override
	gatekeeper.Spec.PodAnnotations = podAnnotations
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertPodAnnotations(g, auditManifest, podAnnotations)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertPodAnnotations(g, webhookManifest, podAnnotations)
}

func assertPodAnnotations(g *WithT, manifest *manifest.Manifest, expected map[string]string) {
	defaultConfig := map[string]string{
		"container.seccomp.security.alpha.kubernetes.io/manager": "runtime/default",
	}
	current, found, err := unstructured.NestedStringMap(manifest.Obj.Object, "spec", "template", "metadata", "annotations")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())
	if expected == nil {
		g.Expect(defaultConfig).To(BeEquivalentTo(current))
	} else {
		g.Expect(expected).To(BeEquivalentTo(current))
	}
}

func TestTolerations(t *testing.T) {
	g := NewWithT(t)
	tolerations := []corev1.Toleration{
		{
			Key:      "example",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectNoSchedule,
		},
		{
			Key:      "example2",
			Operator: corev1.TolerationOpExists,
			Effect:   corev1.TaintEffectPreferNoSchedule,
		},
	}

	gatekeeper := &operatorv1alpha1.Gatekeeper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "testns",
		},
	}
	// test default tolerations
	auditManifest, err := getManifest(auditFile)
	webhookManifest, err := getManifest(webhookFile)
	g.Expect(err).ToNot(HaveOccurred())
	assertTolerations(g, auditManifest, nil)
	assertTolerations(g, webhookManifest, nil)

	// test nil tolerations
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertTolerations(g, auditManifest, nil)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertTolerations(g, webhookManifest, nil)

	// test tolerations override
	gatekeeper.Spec.Tolerations = tolerations
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertTolerations(g, auditManifest, tolerations)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertTolerations(g, webhookManifest, tolerations)
}

func assertTolerations(g *WithT, manifest *manifest.Manifest, expected []corev1.Toleration) {
	current, found, err := unstructured.NestedSlice(manifest.Obj.Object, "spec", "template", "spec", "tolerations")
	g.Expect(err).ToNot(HaveOccurred())
	if expected == nil {
		g.Expect(found).To(BeFalse())
	} else {
		for i, toleration := range expected {
			g.Expect(toMap(toleration)).To(BeEquivalentTo(current[i]))
		}
	}
}

func TestResources(t *testing.T) {
	g := NewWithT(t)
	resources := &corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2000M"),
			corev1.ResourceMemory: resource.MustParse("1024Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("200m"),
			corev1.ResourceMemory: resource.MustParse("512"),
		},
	}

	gatekeeper := &operatorv1alpha1.Gatekeeper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "testns",
		},
	}
	// test default resources
	auditManifest, err := getManifest(auditFile)
	webhookManifest, err := getManifest(webhookFile)
	g.Expect(err).ToNot(HaveOccurred())
	assertResources(g, auditManifest, nil)
	assertResources(g, webhookManifest, nil)

	// test nil resources
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertResources(g, auditManifest, nil)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertResources(g, webhookManifest, nil)

	// test resources override
	gatekeeper.Spec.Resources = resources
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertResources(g, auditManifest, resources)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertResources(g, webhookManifest, resources)
}

func assertResources(g *WithT, manifest *manifest.Manifest, expected *corev1.ResourceRequirements) {
	defaultConfig := &corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1000m"),
			corev1.ResourceMemory: resource.MustParse("512Mi"),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		},
	}

	containers, found, err := unstructured.NestedSlice(manifest.Obj.Object, "spec", "template", "spec", "containers")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())

	for _, c := range containers {
		current, found, err := unstructured.NestedMap(toMap(c), "resources")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(found).To(BeTrue())
		if expected == nil {
			assertResource(g, defaultConfig, current)
		} else {
			assertResource(g, expected, current)
		}
	}
}

func assertResource(g *WithT, expected *corev1.ResourceRequirements, current map[string]interface{}) {
	g.Expect(expected.Limits.Cpu().Cmp(resource.MustParse(current["limits"].(map[string]interface{})["cpu"].(string)))).To(BeZero())
	g.Expect(expected.Limits.Memory().Cmp(resource.MustParse(current["limits"].(map[string]interface{})["memory"].(string)))).To(BeZero())
	g.Expect(expected.Requests.Cpu().Cmp(resource.MustParse(current["requests"].(map[string]interface{})["cpu"].(string)))).To(BeZero())
	g.Expect(expected.Requests.Memory().Cmp(resource.MustParse(current["requests"].(map[string]interface{})["memory"].(string)))).To(BeZero())
}

func TestImage(t *testing.T) {
	g := NewWithT(t)
	image := "mycustom-image/gatekeeper:v3.1.1"
	imagePullPolicy := corev1.PullIfNotPresent
	imageConfig := &operatorv1alpha1.ImageConfig{
		Image:           &image,
		ImagePullPolicy: &imagePullPolicy,
	}

	gatekeeper := &operatorv1alpha1.Gatekeeper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "testns",
		},
	}
	// test default image
	auditManifest, err := getManifest(auditFile)
	webhookManifest, err := getManifest(webhookFile)
	g.Expect(err).ToNot(HaveOccurred())
	assertImage(g, auditManifest, nil)
	assertImage(g, webhookManifest, nil)

	// test nil image
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertImage(g, auditManifest, nil)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertImage(g, webhookManifest, nil)

	// test image override
	gatekeeper.Spec.Image = imageConfig
	err = crOverrides(gatekeeper, auditFile, auditManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertImage(g, auditManifest, imageConfig)
	err = crOverrides(gatekeeper, webhookFile, webhookManifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertImage(g, webhookManifest, imageConfig)
}

func assertImage(g *WithT, manifest *manifest.Manifest, expected *operatorv1alpha1.ImageConfig) {
	defaultImage := "openpolicyagent/gatekeeper:v3.1.1"
	defaultImagePullPolicy := corev1.PullAlways

	containers, found, err := unstructured.NestedSlice(manifest.Obj.Object, "spec", "template", "spec", "containers")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())

	for _, c := range containers {
		currentImage, found, err := unstructured.NestedString(toMap(c), "image")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(found).To(BeTrue())
		if expected == nil {
			g.Expect(defaultImage).To(BeEquivalentTo(currentImage))
		} else {
			g.Expect(*expected.Image).To(BeEquivalentTo(currentImage))
		}
		currentImagePullPolicy, found, err := unstructured.NestedString(toMap(c), "imagePullPolicy")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(found).To(BeTrue())
		if expected == nil {
			g.Expect(defaultImagePullPolicy).To(BeEquivalentTo(currentImagePullPolicy))
		} else {
			g.Expect(*expected.ImagePullPolicy).To(BeEquivalentTo(currentImagePullPolicy))
		}
	}
}

func TestFailurePolicy(t *testing.T) {
	g := NewWithT(t)

	failurePolicy := admregv1.Fail
	webhook := operatorv1alpha1.WebhookConfig{
		FailurePolicy: &failurePolicy,
	}
	gatekeeper := &operatorv1alpha1.Gatekeeper{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "testns",
		},
	}
	// test default failurePolicy
	manifest, err := getManifest(validatingWebhookConfiguration)
	g.Expect(err).ToNot(HaveOccurred())
	assertFailurePolicy(g, manifest, nil)

	// test nil failurePolicy
	err = crOverrides(gatekeeper, validatingWebhookConfiguration, manifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertFailurePolicy(g, manifest, nil)

	// test failurePolicy override
	gatekeeper.Spec.Webhook = &webhook
	err = crOverrides(gatekeeper, validatingWebhookConfiguration, manifest)
	g.Expect(err).ToNot(HaveOccurred())
	assertFailurePolicy(g, manifest, &failurePolicy)
}

func assertFailurePolicy(g *WithT, manifest *manifest.Manifest, expected *admregv1.FailurePolicyType) {
	defaultPolicy := admregv1.Ignore

	webhooks, found, err := unstructured.NestedSlice(manifest.Obj.Object, "webhooks")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(found).To(BeTrue())

	for _, w := range webhooks {
		webhook := w.(map[string]interface{})
		if webhook["name"] == validationGatekeeperWebhook {
			current, found, err := unstructured.NestedString(toMap(w), "failurePolicy")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			if expected == nil {
				g.Expect(defaultPolicy).To(BeEquivalentTo(current))
			} else {
				g.Expect(*expected).To(BeEquivalentTo(current))
			}
		}
	}
}
