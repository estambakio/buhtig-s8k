package main

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes/fake"
)

func TestNamespace_Name(t *testing.T) {
	for _, name := range []string{"One", "Two", "Three"} {
		k8sNs := corev1.Namespace{}
		k8sNs.ObjectMeta.Name = name
		ns := namespace(k8sNs)
		if ns.Name() != name {
			t.Errorf("Expected name %s, but got %s", name, ns.Name())
		}
	}
}

func TestNamespace_GithubSourceURL(t *testing.T) {
	for _, name := range []string{"One", "Two", "Three"} {
		ghLink := "http://" + name
		k8sNs := corev1.Namespace{}

		ns := namespace(k8sNs)

		if val, err := ns.GithubSourceURL(); err == nil {
			t.Errorf("Shoud've failed for empty value but returned %v", val)
		}

		metav1.SetMetaDataAnnotation(&ns.ObjectMeta, githubURLAnnotationName, ghLink)
		if val, err := ns.GithubSourceURL(); err != nil || val != ghLink {
			t.Errorf("Expected link %s, but got %s", ghLink, val)
		}
	}
}

func TestNamespace_HelmRelease(t *testing.T) {
	for _, name := range []string{"One", "Two", "Three"} {
		helmRelease := "dev-" + name
		k8sNs := corev1.Namespace{}

		ns := newNamespace(k8sNs)

		if val, err := ns.HelmRelease(); err == nil {
			t.Errorf("Shoud've failed for empty value but returned %v", val)
		}

		metav1.SetMetaDataAnnotation(&ns.ObjectMeta, helmReleaseAnnotationName, helmRelease)

		if val, err := ns.HelmRelease(); err != nil || val != helmRelease {
			t.Errorf("Expected link %s, but got %s", helmRelease, val)
		}
	}
}

func TestNamespace_String(t *testing.T) {
	name := "One"
	k8sNs := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	ns := namespace(k8sNs)
	str := fmt.Sprintf("%s", &ns)
	if str != name {
		t.Errorf("Expected name %s, but got %v", name, str)
	}
}

func TestNsChan_filter(t *testing.T) {
	var namespaces []*namespace
	for _, name := range []string{"One", "Two", "Three"} {
		k8sNs := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
		namespaces = append(namespaces, newNamespace((k8sNs)))
	}

	nsC := make(nsChan)

	// filter by names which start with "T"
	resultC := nsC.filter(func(ns *namespace) bool {
		return strings.HasPrefix(ns.Name(), "T")
	})

	go func() {
		for _, ns := range namespaces {
			nsC <- ns
		}
		close(nsC)
	}()

	i := 0

	for range resultC {
		i++
	}

	if i != 2 {
		t.Errorf("Expected i == 2, but got %v", i)
	}
}

// addK8sNs is a helper function which populates fake k8s client with namespaces
func addK8sNs(client *fake.Clientset, names []string, addLabel bool) (err error) {
	for _, name := range names {
		meta := metav1.ObjectMeta{}
		meta.Name = name
		if addLabel {
			meta.Labels = map[string]string{
				strings.Split(labelSelector, "=")[0]: strings.Split(labelSelector, "=")[1],
			}
		}
		ns := &corev1.Namespace{ObjectMeta: meta}
		_, err := client.CoreV1().Namespaces().Create(ns)
		if err != nil {
			return err
		}
	}
	return nil
}

func TestGetNamespaces(t *testing.T) {
	k8sClient := fake.NewSimpleClientset()

	// create k8s namespaces without required label
	namesWithoutLabel := []string{"One", "Two", "Three"}
	err := addK8sNs(k8sClient, namesWithoutLabel, false)
	if err != nil {
		t.Error(err)
	}

	// if there're no namespaces with required label then channel should be empty
	shouldBeEmptyNsChan := getNamespaces(k8sClient)

	i := 0
	for range shouldBeEmptyNsChan {
		i++
	}

	if i != 0 {
		t.Errorf("Expected empty channel, but got %d elements", i)
	}

	// create k8s namespaces with required label
	namesWithLabel := []string{"Four", "Five", "Six"}
	err = addK8sNs(k8sClient, namesWithLabel, true)
	if err != nil {
		t.Error(err)
	}

	// if there're namespaces with required label then channel should include all these namespaces
	shouldBeNotEmptyNsChan := getNamespaces(k8sClient)

	i = 0
	for ns := range shouldBeNotEmptyNsChan {
		if ns.ObjectMeta.Name != namesWithLabel[i] {
			t.Errorf("Expected name %s, but got %v", namesWithLabel[i], ns.ObjectMeta.Name)
		}
		i++
	}

	if i != len(namesWithLabel) {
		t.Errorf("Expected i == %d, but got %v", len(namesWithLabel), i)
	}
}

func TestIsNamespaceDeleted(t *testing.T) {
	k8sClient := fake.NewSimpleClientset()

	// create k8s namespaces
	names := []string{"One", "Two", "Three"}
	err := addK8sNs(k8sClient, names, false)
	if err != nil {
		t.Error(err)
	}

	k8sNs, err := k8sClient.CoreV1().Namespaces().Get(names[1], metav1.GetOptions{})

	// should delete namespace and return true
	ok := isNamespaceDeleted(k8sClient)(newNamespace(*k8sNs))

	nsList, err := k8sClient.CoreV1().Namespaces().List(metav1.ListOptions{})
	if err != nil {
		t.Error(err)
	}

	if len(nsList.Items) != (len(names) - 1) {
		t.Errorf("Failed to delete ns %s", names[1])
	}

	if !ok {
		t.Errorf("Expected %v for deleted namespace, but got %v", true, ok)
	}

	// try to delete non existing namespace
	nonExNs := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "IDontExist"}}

	// should return true because this namespace doesn't exist
	ok = isNamespaceDeleted(k8sClient)(newNamespace(nonExNs))

	if !ok {
		t.Errorf("Expected %v for not existing namespace, but got %v", true, ok)
	}
}
