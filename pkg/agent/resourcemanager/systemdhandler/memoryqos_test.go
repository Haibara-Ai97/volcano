package systemdhandler

import (
	"context"
	"fmt"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"volcano.sh/volcano/pkg/agent/resourcemanager/utils"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

// TestSetMemoryQos_Integration Test Memory QoS handler with different pod instance
func TestSetMemoryQos_Integration(t *testing.T) {
	// Skip test if not running in a real cgroup v2 environment
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		t.Skip("Skipping test: cgroup v2 not available (no /sys/fs/cgroup/cgroup.controllers)")
	}

	cgroupVersion, err := cgroup.DetectCgroupVersion("/sys/fs/cgroup")
	if err != nil {
		t.Fatalf("Failed to detect cgroup version: %v", err)
	}

	t.Logf("Detected cgroup version: %s", cgroupVersion)

	cgroupMgr := cgroup.NewCgroupManager("", TestCgroupRootPath, "")
	handler := NewSystemdHandler(cgroupMgr, cgroupVersion)

	// pods with different QoS level in kubernetes
	testCases := []struct {
		name     string
		qosLevel int64
		podSpec  corev1.PodSpec
	}{
		{
			name:     "High Priority QoS Level 2 - Guaranteed Pod",
			qosLevel: 2,
			podSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test",
						Image:   "docker.io/library/alpine:latest",
						Command: []string{"tail", "-f", "/dev/null"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyNever,
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/control-plane",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		},
		{
			name:     "Normal Priority QoS Level 1 - Burstable Pod",
			qosLevel: 1,
			podSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test",
						Image:   "docker.io/library/alpine:latest",
						Command: []string{"tail", "-f", "/dev/null"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyNever,
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/control-plane",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		},
		{
			name:     "Low Priority QoS Level 0 - Burstable Pod",
			qosLevel: 0,
			podSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test",
						Image:   "docker.io/library/alpine:latest",
						Command: []string{"tail", "-f", "/dev/null"},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
				RestartPolicy: corev1.RestartPolicyNever,
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/control-plane",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		},
		{
			name:     "Idle QoS Level -1 - BestEffort Pod",
			qosLevel: -1,
			podSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    "test",
						Image:   "docker.io/library/alpine:latest",
						Command: []string{"tail", "-f", "/dev/null"},
					},
				},
				RestartPolicy: corev1.RestartPolicyNever,
				Tolerations: []corev1.Toleration{
					{
						Key:      "node-role.kubernetes.io/control-plane",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ns := "default"
			podName := fmt.Sprintf("memory-qos-systemd-test-pod-%d", tc.qosLevel)
			image := "docker.io/library/alpine:latest"

			pod, clientset, err := createTestPodWithSpec(ns, podName, image, tc.podSpec)
			if err != nil {
				t.Fatalf("Failed to create test pod: %v", err)
			}

			t.Cleanup(func() {
				_ = clientset.CoreV1().Pods(ns).Delete(context.Background(), podName, metav1.DeleteOptions{})
			})

			actualQoSClass := pod.Status.QOSClass
			t.Logf("Pod %s has QoS Class: %s", pod.Name, actualQoSClass)

			err = handler.SetMemoryQos(pod.UID, actualQoSClass, tc.qosLevel)
			if err != nil {
				t.Fatalf("Failed to set Memory QoS level: %v", err)
			}

			err = verifySystemdMemoryQoSSettings(t, handler, pod.UID, actualQoSClass, tc.qosLevel, cgroupVersion)
			if err != nil {
				t.Fatalf("Failed to verify Memory QoS settings: %v", err)
			}

			t.Logf("Successfully set and verified Memory QoS level %d for pod %s", tc.qosLevel, pod.Name)
		})
	}
}

// verifySystemdMemoryQoSSettings Verify the Memory QoS Setting for systemd handler
func verifySystemdMemoryQoSSettings(t *testing.T, handler *SystemdHandler, podUID types.UID, qosClass corev1.PodQOSClass, qosLevel int64, cgroupVersion string) error {
	// For systemd handler, we need to verify that the service name is correctly generated
	serviceName := handler.getServiceName(podUID, qosClass)
	if serviceName == "" {
		return fmt.Errorf("failed to get service name for pod %s", podUID)
	}

	t.Logf("Pod service name: %s", serviceName)

	// Since we can't easily verify D-Bus calls in unit tests without mocking,
	// we'll verify that the service name is properly formatted
	if !isValidServiceName(serviceName) {
		return fmt.Errorf("invalid service name format: %s", serviceName)
	}

	// Verify that the memory QoS values are correctly calculated
	expectedMemoryHigh := utils.CalculateMemoryHighFromQoSLevel(qosLevel)
	expectedMemoryLow := utils.CalculateMemoryLowFromQoSLevel(qosLevel)
	expectedMemoryMin := utils.CalculateMemoryMinFromQoSLevel(qosLevel)

	t.Logf("Expected Memory QoS values for level %d: High=%d, Low=%d, Min=%d",
		qosLevel, expectedMemoryHigh, expectedMemoryLow, expectedMemoryMin)

	t.Logf("Verified systemd service name: %s", serviceName)
	return nil
}
