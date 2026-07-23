package cost

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

const (
	HoursPerMonth = 730.0
	GiB           = int64(1024 * 1024 * 1024)
)

// Rates are fallback per-resource prices used when the node's instance
// type is not in the embedded pricing table (or for on-prem clusters).
type Rates struct {
	CPUHourlyUSD   float64 // per vCPU-hour
	MemGBHourlyUSD float64 // per GiB-hour
	PVGBMonthlyUSD float64 // per GiB-month
	LBMonthlyUSD   float64 // per load balancer month
}

// DefaultRates: generic public-cloud on-demand baselines.
// CPU/mem: GCP published per-resource pricing. PV: generic SSD block
// storage. LB: ~$0.025/h cloud LB baseline.
func DefaultRates() Rates {
	return Rates{
		CPUHourlyUSD:   0.0316,
		MemGBHourlyUSD: 0.0042,
		PVGBMonthlyUSD: 0.10,
		LBMonthlyUSD:   18.0,
	}
}

type Model struct {
	Rates Rates
}

func NewModel(r Rates) *Model { return &Model{Rates: r} }

// RequestsMonthlyUSD prices a quantity of reserved CPU/memory.
func (m *Model) RequestsMonthlyUSD(cpuMilli, memBytes int64) (float64, string) {
	cpu := float64(cpuMilli) / 1000
	mem := float64(memBytes) / float64(GiB)
	usd := (cpu*m.Rates.CPUHourlyUSD + mem*m.Rates.MemGBHourlyUSD) * HoursPerMonth
	basis := fmt.Sprintf("%.2f vCPU × $%.4f/h + %.2f GiB × $%.4f/h, 730h/mo",
		cpu, m.Rates.CPUHourlyUSD, mem, m.Rates.MemGBHourlyUSD)
	return usd, basis
}

// NodeMonthlyUSD prices a whole node: instance-type label lookup first,
// allocatable-based per-resource fallback otherwise.
func (m *Model) NodeMonthlyUSD(n corev1.Node) (float64, string) {
	it := n.Labels["node.kubernetes.io/instance-type"]
	if it == "" {
		it = n.Labels["beta.kubernetes.io/instance-type"]
	}
	if hourly, ok := instanceHourlyUSD[it]; ok {
		return hourly * HoursPerMonth, fmt.Sprintf("%s on-demand $%.3f/h × 730h", it, hourly)
	}
	cpuMilli := n.Status.Allocatable.Cpu().MilliValue()
	memBytes := n.Status.Allocatable.Memory().Value()
	usd, basis := m.RequestsMonthlyUSD(cpuMilli, memBytes)
	return usd, "allocatable fallback: " + basis
}

func (m *Model) PVMonthlyUSD(capacityBytes int64) (float64, string) {
	gb := float64(capacityBytes) / float64(GiB)
	return gb * m.Rates.PVGBMonthlyUSD,
		fmt.Sprintf("%.0f GiB × $%.2f/GiB-mo", gb, m.Rates.PVGBMonthlyUSD)
}

func (m *Model) LBMonthly() (float64, string) {
	return m.Rates.LBMonthlyUSD, fmt.Sprintf("cloud LB baseline $%.0f/mo", m.Rates.LBMonthlyUSD)
}
