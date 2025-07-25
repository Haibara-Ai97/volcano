package local

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/agent/utils/cgroup"
)

func TestMemoryLocalCollectMetrics_Integration(t *testing.T) {
	cgroupDriver := cgroup.GetCgroupDriver()
	cgroupRoot := TestCgroupRootPath
	kubeCgroupRoot := ""

	manager := cgroup.NewCgroupManager(cgroupDriver, cgroupRoot, kubeCgroupRoot)
	collector := &MemoryResourceCollector{cgroupManager: manager}

	metricInfo := &LocalMetricInfo{
		IncludeSystemUsed: true,
	}

	series, err := collector.CollectLocalMetrics(metricInfo, time.Now(), metav1.Duration{})
	if err != nil {
		t.Fatalf("CollectLocalMetrics failed: %v", err)
	}
	if len(series) == 0 {
		t.Fatalf("expected at least 1 timeseries, got 0")
	}
	t.Logf("Memory usage value: %v", series[0].Samples[0].Value)
}
