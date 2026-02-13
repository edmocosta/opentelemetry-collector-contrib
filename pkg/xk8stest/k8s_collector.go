// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package xk8stest // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/xk8stest"

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func CreateCollectorObjects(t *testing.T, client *K8sClient, testID, manifestsDir string, templateValues map[string]string, host string) []*unstructured.Unstructured {
	if manifestsDir == "" {
		manifestsDir = filepath.Join(".", "testdata", "e2e", "collector")
	}
	manifestFiles, err := os.ReadDir(manifestsDir)
	require.NoErrorf(t, err, "failed to read collector manifests directory %s", manifestsDir)
	if host == "" {
		host = HostEndpoint(t)
	}
	var podNamespace string
	var podLabels map[string]any
	createdObjs := make([]*unstructured.Unstructured, 0, len(manifestFiles))
	for _, manifestFile := range manifestFiles {
		tmpl := template.Must(template.New(manifestFile.Name()).ParseFiles(filepath.Join(manifestsDir, manifestFile.Name())))
		manifest := &bytes.Buffer{}
		defaultTemplateValues := map[string]string{
			"Name":         "otelcol-" + testID,
			"HostEndpoint": host,
			"TestID":       testID,
		}
		maps.Copy(defaultTemplateValues, templateValues)
		require.NoError(t, tmpl.Execute(manifest, defaultTemplateValues))
		obj, err := CreateObject(client, manifest.Bytes())
		require.NoErrorf(t, err, "failed to create collector object from manifest %s", manifestFile.Name())
		objKind := obj.GetKind()
		if objKind == "Deployment" || objKind == "DaemonSet" {
			podNamespace = obj.GetNamespace()
			selector := obj.Object["spec"].(map[string]any)["selector"]
			podLabels = selector.(map[string]any)["matchLabels"].(map[string]any)
		}
		createdObjs = append(createdObjs, obj)
	}

	WaitForCollectorToStart(t, client, podNamespace, podLabels)

	return createdObjs
}

func WaitForCollectorToStart(t *testing.T, client *K8sClient, podNamespace string, podLabels map[string]any) {
	podGVR := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	listOptions := metav1.ListOptions{LabelSelector: SelectorFromMap(podLabels).String()}
	podTimeoutMinutes := 3
	t.Logf("waiting for collector pods to be ready")
	require.Eventuallyf(t, func() bool {
		list, err := client.DynamicClient.Resource(podGVR).Namespace(podNamespace).List(t.Context(), listOptions)
		require.NoError(t, err, "failed to list collector pods")
		podsNotReady := len(list.Items)
		if podsNotReady == 0 {
			t.Log("did not find collector pods")
			return false
		}

		var pods v1.PodList
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(list.UnstructuredContent(), &pods)
		require.NoError(t, err, "failed to convert unstructured to podList")

		for i := range pods.Items {
			pod := &pods.Items[i]
			podReady := false
			if pod.Status.Phase != v1.PodRunning {
				t.Logf("pod %v is not running, current phase: %v", pod.Name, pod.Status.Phase)
				printPodLogsForDebug(t, client, podNamespace, pod)
				continue
			}
			for _, cond := range pod.Status.Conditions {
				if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
					podsNotReady--
					podReady = true
				}
			}
			// Add some debug logs for crashing pods
			if !podReady {
				t.Logf("pod %s not ready; phase=%s", pod.Name, pod.Status.Phase)
				for i := range pod.Status.ContainerStatuses {
					cs := &pod.Status.ContainerStatuses[i]
					restartCount := cs.RestartCount
					if restartCount > 0 && cs.LastTerminationState.Terminated != nil {
						t.Logf("restart count = %d for container %s in pod %s, last terminated reason: %s", restartCount, cs.Name, pod.Name, cs.LastTerminationState.Terminated.Reason)
						t.Logf("termination message: %s", cs.LastTerminationState.Terminated.Message)
					}
				}
				printPodLogsForDebug(t, client, podNamespace, pod)
			}
		}
		if podsNotReady == 0 {
			t.Logf("collector pods are ready")
			return true
		}
		return false
	}, time.Duration(podTimeoutMinutes)*time.Minute, 2*time.Second,
		"collector pods were not ready within %d minutes", podTimeoutMinutes)
}

func printPodLogsForDebug(t *testing.T, client *K8sClient, namespace string, pod *v1.Pod) {
	if client == nil || client.KubeClient == nil {
		return
	}
	if pod == nil || pod.Name == "" {
		return
	}

	// Keep this small-ish to avoid spamming CI logs.
	const tailLines int64 = 200
	containers := make([]string, 0, len(pod.Spec.Containers))
	for i := range pod.Spec.Containers {
		containers = append(containers, pod.Spec.Containers[i].Name)
	}
	if len(containers) == 0 {
		// Fallback: some pod objects might not have spec in edge cases; still try without container name.
		containers = append(containers, "")
	}

	for _, container := range containers {
		containerLabel := container
		if containerLabel == "" {
			containerLabel = "<default>"
		}

		for _, previous := range []bool{false, true} {
			opts := &v1.PodLogOptions{
				Container: container,
				Previous:  previous,
				TailLines: func() *int64 { v := tailLines; return &v }(),
			}

			req := client.KubeClient.CoreV1().Pods(namespace).GetLogs(pod.Name, opts)
			stream, err := req.Stream(t.Context())
			if err != nil {
				// Not all pods/containers have logs yet (or previous logs), so keep it informational.
				t.Logf("pod %s container %s previous=%t: unable to stream logs: %v", pod.Name, containerLabel, previous, err)
				continue
			}
			data, readErr := io.ReadAll(stream)
			_ = stream.Close()
			if readErr != nil {
				t.Logf("pod %s container %s previous=%t: unable to read logs: %v", pod.Name, containerLabel, previous, readErr)
				continue
			}
			if len(bytes.TrimSpace(data)) == 0 {
				continue
			}

			head := fmt.Sprintf("---- logs pod=%s container=%s previous=%t (tail=%d) ----\n", pod.Name, containerLabel, previous, tailLines)
			t.Logf("%s%s", head, string(data))
		}
	}
}
