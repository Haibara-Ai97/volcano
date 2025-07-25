package local

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

const TestCgroupRootPath = "/sys/fs/cgroup"

// TestCPULocalCollectMetrics_Integration Test LocalCollector.CollectLocalMetrics
func TestCPULocalCollectMetrics_Integration(t *testing.T) {
	// 这里的参数需要与你实际环境的cgroup driver和root路径一致
	cgroupDriver := cgroup.GetCgroupDriver()
	cgroupRoot := TestCgroupRootPath
	kubeCgroupRoot := "" // 通常为空

	manager := cgroup.NewCgroupManager(cgroupDriver, cgroupRoot, kubeCgroupRoot)
	collector := &CPUResourceCollector{cgroupManager: manager}

	metricInfo := &LocalMetricInfo{
		IncludeGuaranteedPods: true,
		IncludeSystemUsed:     true,
	}

	series, err := collector.CollectLocalMetrics(metricInfo, time.Now(), metav1.Duration{})
	if err != nil {
		t.Fatalf("CollectLocalMetrics failed: %v", err)
	}
	if len(series) == 0 {
		t.Fatalf("expected at least 1 timeseries, got 0")
	}
	t.Logf("CPU usage value: %v", series[0].Samples[0].Value)
}
