# kubeoptimizer Phase 0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** kubeconfig만으로 30초 안에 "이 클러스터에서 월 얼마 새는지" 리포트하는 읽기전용 Go CLI (`kubeoptimizer scan`) — 스펙의 Phase 0 (OSS 무료 티어) 전체.

**Architecture:** Snapshot Collector가 k8s API(+선택적 metrics-server)를 한 번 수집해 불변 스냅샷을 만들고, 체크 엔진(체크 1개 = 파일 1개, 동일 인터페이스)이 스냅샷 위에서 Finding을 생성하며, Cost Model이 노드 라벨→단가 매핑으로 월 비용을 추정하고, Reporter가 터미널 테이블/JSON으로 출력한다. 데이터 소스는 자동 감지 — 없으면 우아하게 스킵하고 리포트에 명시.

**Tech Stack:** Go 1.22+, k8s.io/client-go, k8s.io/api, k8s.io/apimachinery, k8s.io/metrics, spf13/cobra. 그 외 서드파티 의존성 금지 (터미널 테이블은 stdlib `text/tabwriter`).

## Global Constraints

- **읽기전용:** 코드 전체에서 k8s API 동사는 get/list만. mutate 동사(create/update/patch/delete) 사용 금지.
- **텔레메트리 없음:** k8s API 외 어떤 네트워크 호출도 금지.
- **의존성 제한:** k8s.io/{client-go,api,apimachinery,metrics} + spf13/cobra만.
- **모듈 경로:** `github.com/DPS0340/kubeoptimizer` (GitHub 공개 직전 실제 username으로 sed 치환).
- **비용 표기:** USD, 월 단위(HoursPerMonth=730), 모든 Finding에 산출 근거(CostBasis) 문자열 필수.
- **Confidence:** `certain` | `estimate` 두 값만.
- **UI 문자열:** 영어 (글로벌 OSS). 문서/커밋은 한국어 허용.
- **조용한 실패 금지:** 수집 실패는 리소스 단위 격리 후 리포트 Notes에 노출. metrics 미감지도 Notes에 명시.

---

### Task 1: Go 모듈 스캐폴드 + Cost Model

**Files:**
- Create: `go.mod` (via `go mod init`)
- Create: `internal/cost/model.go`
- Create: `internal/cost/pricing_data.go`
- Test: `internal/cost/model_test.go`

**Interfaces:**
- Consumes: 없음 (최하위 레이어)
- Produces (후속 태스크가 사용):
  - `cost.DefaultRates() Rates`
  - `cost.NewModel(r Rates) *Model`
  - `(*Model).RequestsMonthlyUSD(cpuMilli, memBytes int64) (float64, string)` — (usd, basis)
  - `(*Model).NodeMonthlyUSD(n corev1.Node) (float64, string)`
  - `(*Model).PVMonthlyUSD(capacityBytes int64) (float64, string)`
  - `(*Model).LBMonthly() (float64, string)`
  - `cost.HoursPerMonth = 730.0`, `cost.GiB`

- [ ] **Step 1: 모듈 초기화 + 의존성 설치**

```bash
cd /Users/lee/programming/kubeoptimizer
go mod init github.com/DPS0340/kubeoptimizer
go get k8s.io/client-go@latest k8s.io/api@latest k8s.io/apimachinery@latest k8s.io/metrics@latest github.com/spf13/cobra@latest
```

Expected: `go.mod`/`go.sum` 생성.

- [ ] **Step 2: 실패하는 테스트 작성**

`internal/cost/model_test.go`:

```go
package cost

import (
	"math"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func almostEqual(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestRequestsMonthlyUSD(t *testing.T) {
	m := NewModel(DefaultRates())
	// 1 vCPU + 1 GiB: (0.0316 + 0.0042) * 730 = 26.134
	usd, basis := m.RequestsMonthlyUSD(1000, GiB)
	if !almostEqual(usd, 26.134) {
		t.Fatalf("usd = %v, want ~26.134", usd)
	}
	if basis == "" {
		t.Fatal("basis must not be empty")
	}
}

func TestNodeMonthlyUSD_KnownInstanceType(t *testing.T) {
	m := NewModel(DefaultRates())
	n := corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{"node.kubernetes.io/instance-type": "m5.xlarge"},
	}}
	usd, basis := m.NodeMonthlyUSD(n)
	// 0.192 * 730 = 140.16
	if !almostEqual(usd, 140.16) {
		t.Fatalf("usd = %v, want ~140.16", usd)
	}
	if basis == "" {
		t.Fatal("basis must not be empty")
	}
}

func TestNodeMonthlyUSD_UnknownFallsBackToAllocatable(t *testing.T) {
	m := NewModel(DefaultRates())
	n := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"node.kubernetes.io/instance-type": "weird.type"}},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("8Gi"),
		}},
	}
	usd, _ := m.NodeMonthlyUSD(n)
	// (2*0.0316 + 8*0.0042) * 730 = 70.664
	if !almostEqual(usd, 70.664) {
		t.Fatalf("usd = %v, want ~70.664", usd)
	}
}

func TestPVAndLB(t *testing.T) {
	m := NewModel(DefaultRates())
	usd, _ := m.PVMonthlyUSD(100 * GiB) // 100 GiB * $0.10 = $10
	if !almostEqual(usd, 10.0) {
		t.Fatalf("pv usd = %v, want 10.0", usd)
	}
	lb, _ := m.LBMonthly()
	if !almostEqual(lb, 18.0) {
		t.Fatalf("lb usd = %v, want 18.0", lb)
	}
}
```

- [ ] **Step 3: 실패 확인**

Run: `go test ./internal/cost/ -v`
Expected: FAIL — `undefined: NewModel` 등 컴파일 에러.

- [ ] **Step 4: 구현**

`internal/cost/model.go`:

```go
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
```

`internal/cost/pricing_data.go`:

```go
package cost

// On-demand hourly USD, us-east-1 / equivalent region baseline.
// Unknown types fall back to per-resource Rates. Regional variance is
// why node costs are labeled confidence=estimate by callers.
var instanceHourlyUSD = map[string]float64{
	// AWS
	"t3.medium":   0.0416,
	"t3.large":    0.0832,
	"m5.large":    0.096,
	"m5.xlarge":   0.192,
	"m5.2xlarge":  0.384,
	"c5.xlarge":   0.17,
	"c5.2xlarge":  0.34,
	"r5.xlarge":   0.252,
	"p3.2xlarge":  3.06,
	"g4dn.xlarge": 0.526,
	// GCP
	"e2-standard-2": 0.067,
	"e2-standard-4": 0.134,
	"n2-standard-4": 0.194,
	"n2-standard-8": 0.388,
	// Azure
	"Standard_D2s_v3": 0.096,
	"Standard_D4s_v3": 0.192,
}
```

- [ ] **Step 5: 통과 확인**

Run: `go test ./internal/cost/ -v`
Expected: PASS (4 tests)

- [ ] **Step 6: 커밋**

```bash
git add go.mod go.sum internal/cost/
git commit -m "feat: cost model with instance pricing table and per-resource fallback"
```

---

### Task 2: Snapshot 타입 + Collector

**Files:**
- Create: `internal/snapshot/snapshot.go`
- Create: `internal/snapshot/collector.go`
- Test: `internal/snapshot/collector_test.go`

**Interfaces:**
- Consumes: 없음
- Produces:
  - `snapshot.Snapshot{ CollectedAt time.Time; Nodes []corev1.Node; Pods []corev1.Pod; PVs []corev1.PersistentVolume; PVCs []corev1.PersistentVolumeClaim; Services []corev1.Service; Endpoints map[string]*corev1.Endpoints; Jobs []batchv1.Job; HasMetrics bool; PodUsage map[string]snapshot.PodUsage; Errors []string }`
  - `snapshot.PodUsage{ CPUMilli, MemBytes int64 }`
  - `snapshot.Key(ns, name string) string` — `"ns/name"`
  - `snapshot.Collect(ctx, kube kubernetes.Interface, mc metricsclient.Interface) *Snapshot` — mc는 nil 허용

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/snapshot/collector_test.go`:

```go
package snapshot

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func TestCollectGathersResources(t *testing.T) {
	kube := fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"}},
		&corev1.Endpoints{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"}},
	)
	s := Collect(context.Background(), kube, nil)
	if len(s.Nodes) != 1 || len(s.Pods) != 1 || len(s.Services) != 1 {
		t.Fatalf("unexpected counts: nodes=%d pods=%d svcs=%d", len(s.Nodes), len(s.Pods), len(s.Services))
	}
	if s.Endpoints[Key("default", "s1")] == nil {
		t.Fatal("endpoints not indexed by ns/name")
	}
	if s.HasMetrics {
		t.Fatal("HasMetrics must be false when metrics client is nil")
	}
}

func TestCollectIsolatesFailures(t *testing.T) {
	kube := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}})
	kube.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})
	s := Collect(context.Background(), kube, nil)
	if len(s.Nodes) != 1 {
		t.Fatal("node collection must survive pod list failure")
	}
	if len(s.Errors) == 0 {
		t.Fatal("pod list failure must be recorded in Errors")
	}
}

func TestCollectMetrics(t *testing.T) {
	kube := fake.NewSimpleClientset()
	mc := metricsfake.NewSimpleClientset(&metricsv1beta1.PodMetrics{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
		Containers: []metricsv1beta1.ContainerMetrics{{
			Name: "c1",
			Usage: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("250m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}},
	})
	s := Collect(context.Background(), kube, mc)
	if !s.HasMetrics {
		t.Fatal("HasMetrics must be true")
	}
	u := s.PodUsage[Key("default", "p1")]
	if u.CPUMilli != 250 || u.MemBytes != 512*1024*1024 {
		t.Fatalf("usage = %+v", u)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/snapshot/ -v`
Expected: FAIL — `undefined: Collect`.

- [ ] **Step 3: 구현**

`internal/snapshot/snapshot.go`:

```go
package snapshot

import (
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// PodUsage is live usage summed over a pod's containers (metrics-server).
type PodUsage struct {
	CPUMilli int64
	MemBytes int64
}

// Snapshot is an immutable view of the cluster taken at one point in
// time. Checks read only from here — never from the API directly.
type Snapshot struct {
	CollectedAt time.Time
	Nodes       []corev1.Node
	Pods        []corev1.Pod
	PVs         []corev1.PersistentVolume
	PVCs        []corev1.PersistentVolumeClaim
	Services    []corev1.Service
	Endpoints   map[string]*corev1.Endpoints // key: ns/name
	Jobs        []batchv1.Job
	HasMetrics  bool
	PodUsage    map[string]PodUsage // key: ns/name
	Errors      []string            // per-resource collection failures
}

func Key(ns, name string) string { return ns + "/" + name }
```

`internal/snapshot/collector.go`:

```go
package snapshot

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Collect lists every resource kind independently: one failure is
// recorded in Errors and never aborts the rest (no silent failures,
// no all-or-nothing). mc may be nil (metrics-server absent).
func Collect(ctx context.Context, kube kubernetes.Interface, mc metricsclient.Interface) *Snapshot {
	s := &Snapshot{
		CollectedAt: time.Now(),
		Endpoints:   map[string]*corev1.Endpoints{},
		PodUsage:    map[string]PodUsage{},
	}
	errf := func(format string, a ...any) { s.Errors = append(s.Errors, fmt.Sprintf(format, a...)) }
	opts := metav1.ListOptions{}

	if l, err := kube.CoreV1().Nodes().List(ctx, opts); err != nil {
		errf("nodes: %v", err)
	} else {
		s.Nodes = l.Items
	}
	if l, err := kube.CoreV1().Pods("").List(ctx, opts); err != nil {
		errf("pods: %v", err)
	} else {
		s.Pods = l.Items
	}
	if l, err := kube.CoreV1().PersistentVolumes().List(ctx, opts); err != nil {
		errf("persistentvolumes: %v", err)
	} else {
		s.PVs = l.Items
	}
	if l, err := kube.CoreV1().PersistentVolumeClaims("").List(ctx, opts); err != nil {
		errf("persistentvolumeclaims: %v", err)
	} else {
		s.PVCs = l.Items
	}
	if l, err := kube.CoreV1().Services("").List(ctx, opts); err != nil {
		errf("services: %v", err)
	} else {
		s.Services = l.Items
	}
	if l, err := kube.CoreV1().Endpoints("").List(ctx, opts); err != nil {
		errf("endpoints: %v", err)
	} else {
		for i := range l.Items {
			e := &l.Items[i]
			s.Endpoints[Key(e.Namespace, e.Name)] = e
		}
	}
	if l, err := kube.BatchV1().Jobs("").List(ctx, opts); err != nil {
		errf("jobs: %v", err)
	} else {
		s.Jobs = l.Items
	}

	if mc != nil {
		// Absence of metrics-server is expected, not an error: leave
		// HasMetrics=false and let the report note it.
		if l, err := mc.MetricsV1beta1().PodMetricses("").List(ctx, opts); err == nil {
			s.HasMetrics = true
			for _, pm := range l.Items {
				var u PodUsage
				for _, c := range pm.Containers {
					u.CPUMilli += c.Usage.Cpu().MilliValue()
					u.MemBytes += c.Usage.Memory().Value()
				}
				s.PodUsage[Key(pm.Namespace, pm.Name)] = u
			}
		}
	}
	return s
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/snapshot/ -v`
Expected: PASS (3 tests)

- [ ] **Step 5: 커밋**

```bash
git add internal/snapshot/
git commit -m "feat: snapshot collector with per-resource failure isolation"
```

---

### Task 3: Check 코어 (Finding, 인터페이스, 공용 헬퍼, 레지스트리)

**Files:**
- Create: `internal/check/check.go`
- Test: `internal/check/check_test.go`

**Interfaces:**
- Consumes: `snapshot.Snapshot`, `cost.Model`
- Produces (모든 체크 태스크가 사용):
  - `check.Confidence` (`check.Certain`, `check.Estimate`)
  - `check.Finding{ CheckID, Target, Reason string; MonthlyCost float64; CostBasis string; Confidence Confidence; Action string }` — JSON 태그 포함
  - `check.Check` 인터페이스: `ID() string`, `Run(s *snapshot.Snapshot, m *cost.Model) []Finding`
  - `check.All() []Check` — 체크 태스크마다 여기에 추가
  - 헬퍼: `podRequests(p corev1.Pod) (cpuMilli, memBytes int64)`, `groupKey(p corev1.Pod) string`, `fmtMem(b int64) string`, `max64(a, b int64) int64`, `const MiB`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/check/check_test.go`:

```go
package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodRequests(t *testing.T) {
	p := corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
		{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("500m"),
			corev1.ResourceMemory: resource.MustParse("256Mi"),
		}}},
		{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("250m"),
		}}},
	}}}
	cpu, mem := podRequests(p)
	if cpu != 750 || mem != 256*MiB {
		t.Fatalf("cpu=%d mem=%d", cpu, mem)
	}
}

func TestGroupKeyStripsReplicaSetHash(t *testing.T) {
	p := corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Namespace:       "prod",
		Name:            "web-7d9f8b6c5-x2k4j",
		OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "web-7d9f8b6c5"}},
	}}
	if got := groupKey(p); got != "prod/ReplicaSet/web" {
		t.Fatalf("groupKey = %q", got)
	}
	bare := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "solo"}}
	if got := groupKey(bare); got != "prod/Pod/solo" {
		t.Fatalf("groupKey bare = %q", got)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/check/ -v`
Expected: FAIL — `undefined: podRequests`.

- [ ] **Step 3: 구현**

`internal/check/check.go`:

```go
package check

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/DPS0340/kubeoptimizer/pkg/cost"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

type Confidence string

const (
	Certain  Confidence = "certain"
	Estimate Confidence = "estimate"
)

const MiB = int64(1024 * 1024)

// Finding is one detected instance of waste, priced monthly with its
// derivation spelled out (CostBasis) — inflated numbers kill trust.
type Finding struct {
	CheckID     string     `json:"check"`
	Target      string     `json:"target"`
	Reason      string     `json:"reason"`
	MonthlyCost float64    `json:"monthly_cost_usd"`
	CostBasis   string     `json:"cost_basis,omitempty"`
	Confidence  Confidence `json:"confidence"`
	Action      string     `json:"action,omitempty"`
}

// Check: one rule = one file. Checks are pure over the snapshot.
type Check interface {
	ID() string
	Run(s *snapshot.Snapshot, m *cost.Model) []Finding
}

// All returns every registered check. Each check task appends here.
func All() []Check {
	return []Check{}
}

func podRequests(p corev1.Pod) (cpuMilli, memBytes int64) {
	for _, c := range p.Spec.Containers {
		if r, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
			cpuMilli += r.MilliValue()
		}
		if r, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
			memBytes += r.Value()
		}
	}
	return
}

// groupKey groups pods by their top-level workload. ReplicaSet names
// carry a template hash suffix which we strip to get the Deployment name.
func groupKey(p corev1.Pod) string {
	if len(p.OwnerReferences) > 0 {
		o := p.OwnerReferences[0]
		name := o.Name
		if o.Kind == "ReplicaSet" {
			if i := strings.LastIndex(name, "-"); i > 0 {
				name = name[:i]
			}
		}
		return p.Namespace + "/" + o.Kind + "/" + name
	}
	return p.Namespace + "/Pod/" + p.Name
}

func fmtMem(b int64) string {
	return resource.NewQuantity(b, resource.BinarySI).String()
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/check/ -v`
Expected: PASS (2 tests)

- [ ] **Step 5: 커밋**

```bash
git add internal/check/
git commit -m "feat: check engine core - Finding, Check interface, shared helpers"
```

---

### Task 4: PV 체크 (Released/미부착 PV, 고아 PVC)

**Files:**
- Create: `internal/check/pv.go`
- Modify: `internal/check/check.go` — `All()`에 `PVCheck{}` 추가
- Test: `internal/check/pv_test.go`

**Interfaces:**
- Consumes: Task 3의 Finding/Check/helpers, `(*cost.Model).PVMonthlyUSD`
- Produces: `check.PVCheck` (ID: `"unused-pv"`)

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/check/pv_test.go`:

```go
package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/cost"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

func testModel() *cost.Model { return cost.NewModel(cost.DefaultRates()) }

func TestPVCheck(t *testing.T) {
	s := &snapshot.Snapshot{
		PVs: []corev1.PersistentVolume{
			{ // Released → certain finding
				ObjectMeta: metav1.ObjectMeta{Name: "pv-released"},
				Spec: corev1.PersistentVolumeSpec{Capacity: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("100Gi")}},
				Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeReleased},
			},
			{ // Bound → no finding
				ObjectMeta: metav1.ObjectMeta{Name: "pv-bound"},
				Spec: corev1.PersistentVolumeSpec{Capacity: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("10Gi")}},
				Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
			},
		},
		PVCs: []corev1.PersistentVolumeClaim{
			{ // bound but no pod mounts it → orphan finding
				ObjectMeta: metav1.ObjectMeta{Name: "orphan", Namespace: "default"},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase:    corev1.ClaimBound,
					Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("50Gi")},
				},
			},
			{ // bound and mounted → no finding
				ObjectMeta: metav1.ObjectMeta{Name: "used", Namespace: "default"},
				Status: corev1.PersistentVolumeClaimStatus{
					Phase:    corev1.ClaimBound,
					Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("20Gi")},
				},
			},
		},
		Pods: []corev1.Pod{{
			ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"},
			Spec: corev1.PodSpec{Volumes: []corev1.Volume{{
				Name: "data",
				VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "used"}},
			}}},
		}},
	}
	fs := PVCheck{}.Run(s, testModel())
	if len(fs) != 2 {
		t.Fatalf("findings = %d, want 2 (released PV + orphan PVC), got %+v", len(fs), fs)
	}
	if fs[0].Confidence != Certain {
		t.Fatalf("released PV must be certain, got %s", fs[0].Confidence)
	}
	if fs[0].MonthlyCost < 9.9 || fs[0].MonthlyCost > 10.1 { // 100Gi × $0.10
		t.Fatalf("released PV cost = %v, want ~10", fs[0].MonthlyCost)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/check/ -run TestPVCheck -v`
Expected: FAIL — `undefined: PVCheck`.

- [ ] **Step 3: 구현**

`internal/check/pv.go`:

```go
package check

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/cost"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

// PVCheck finds storage that is billed but serves no workload:
// Released/Available PVs and bound PVCs no pod mounts.
type PVCheck struct{}

func (PVCheck) ID() string { return "unused-pv" }

func (PVCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	var out []Finding
	for _, pv := range s.PVs {
		if pv.Status.Phase != corev1.VolumeReleased && pv.Status.Phase != corev1.VolumeAvailable {
			continue
		}
		capQ := pv.Spec.Capacity[corev1.ResourceStorage]
		usd, basis := m.PVMonthlyUSD(capQ.Value())
		conf := Certain
		if pv.Status.Phase == corev1.VolumeAvailable {
			conf = Estimate // static/local PVs may not be billed
		}
		out = append(out, Finding{
			CheckID:     "unused-pv",
			Target:      "pv/" + pv.Name,
			Reason:      fmt.Sprintf("PersistentVolume is %s — not bound to any claim", pv.Status.Phase),
			MonthlyCost: usd,
			CostBasis:   basis,
			Confidence:  conf,
			Action:      "verify data is not needed, then: kubectl delete pv " + pv.Name,
		})
	}

	mounted := map[string]bool{}
	for _, p := range s.Pods {
		for _, v := range p.Spec.Volumes {
			if v.PersistentVolumeClaim != nil {
				mounted[snapshot.Key(p.Namespace, v.PersistentVolumeClaim.ClaimName)] = true
			}
		}
	}
	for _, pvc := range s.PVCs {
		if pvc.Status.Phase != corev1.ClaimBound || mounted[snapshot.Key(pvc.Namespace, pvc.Name)] {
			continue
		}
		capQ := pvc.Status.Capacity[corev1.ResourceStorage]
		usd, basis := m.PVMonthlyUSD(capQ.Value())
		out = append(out, Finding{
			CheckID:     "unused-pv",
			Target:      fmt.Sprintf("pvc/%s/%s", pvc.Namespace, pvc.Name),
			Reason:      "PVC is bound but not mounted by any pod",
			MonthlyCost: usd,
			CostBasis:   basis,
			Confidence:  Estimate, // may be intentionally kept (backups etc.)
			Action:      fmt.Sprintf("verify, then: kubectl -n %s delete pvc %s", pvc.Namespace, pvc.Name),
		})
	}
	return out
}
```

`internal/check/check.go`의 `All()` 수정:

```go
func All() []Check {
	return []Check{
		PVCheck{},
	}
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/check/ -v`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/check/
git commit -m "feat: unused-pv check - released/available PVs and orphan PVCs"
```

---

### Task 5: LoadBalancer 체크

**Files:**
- Create: `internal/check/loadbalancer.go`
- Modify: `internal/check/check.go` — `All()`에 `LBCheck{}` 추가
- Test: `internal/check/loadbalancer_test.go`

**Interfaces:**
- Consumes: `s.Services`, `s.Endpoints`, `(*cost.Model).LBMonthly`
- Produces: `check.LBCheck` (ID: `"idle-loadbalancer"`)

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/check/loadbalancer_test.go`:

```go
package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

func TestLBCheck(t *testing.T) {
	lbType := func(name string) corev1.Service {
		return corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		}
	}
	s := &snapshot.Snapshot{
		Services: []corev1.Service{
			lbType("idle-lb"),    // no endpoints object at all → finding
			lbType("healthy-lb"), // has ready addresses → no finding
			{ // ClusterIP → ignored
				ObjectMeta: metav1.ObjectMeta{Name: "clusterip", Namespace: "default"},
				Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
			},
		},
		Endpoints: map[string]*corev1.Endpoints{
			snapshot.Key("default", "healthy-lb"): {Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{IP: "10.0.0.1"}},
			}}},
		},
	}
	fs := LBCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1, got %+v", len(fs), fs)
	}
	if fs[0].Target != "svc/default/idle-lb" || fs[0].Confidence != Certain {
		t.Fatalf("unexpected finding: %+v", fs[0])
	}
	if fs[0].MonthlyCost != 18.0 {
		t.Fatalf("cost = %v, want 18.0", fs[0].MonthlyCost)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/check/ -run TestLBCheck -v`
Expected: FAIL — `undefined: LBCheck`.

- [ ] **Step 3: 구현**

`internal/check/loadbalancer.go`:

```go
package check

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/cost"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

// LBCheck finds LoadBalancer services with zero ready endpoints:
// the cloud LB is billed hourly while routing traffic to nothing.
type LBCheck struct{}

func (LBCheck) ID() string { return "idle-loadbalancer" }

func (LBCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	var out []Finding
	for _, svc := range s.Services {
		if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
			continue
		}
		ready := 0
		if ep := s.Endpoints[snapshot.Key(svc.Namespace, svc.Name)]; ep != nil {
			for _, sub := range ep.Subsets {
				ready += len(sub.Addresses)
			}
		}
		if ready > 0 {
			continue
		}
		usd, basis := m.LBMonthly()
		out = append(out, Finding{
			CheckID:     "idle-loadbalancer",
			Target:      fmt.Sprintf("svc/%s/%s", svc.Namespace, svc.Name),
			Reason:      "LoadBalancer service has no ready endpoints — cloud LB billed for nothing",
			MonthlyCost: usd,
			CostBasis:   basis,
			Confidence:  Certain,
			Action:      fmt.Sprintf("fix selector or delete: kubectl -n %s delete svc %s", svc.Namespace, svc.Name),
		})
	}
	return out
}
```

`All()`에 `LBCheck{}` 추가.

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/check/ -v`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/check/
git commit -m "feat: idle-loadbalancer check"
```

---

### Task 6: 좀비 워크로드 체크

**Files:**
- Create: `internal/check/zombie.go`
- Modify: `internal/check/check.go` — `All()`에 `ZombieCheck{}` 추가
- Test: `internal/check/zombie_test.go`

**Interfaces:**
- Consumes: `s.Pods`, `s.Jobs`, `s.CollectedAt`, `podRequests`, `(*cost.Model).RequestsMonthlyUSD`
- Produces: `check.ZombieCheck` (ID: `"zombie-workloads"`)
- 룰: CrashLoopBackOff이고 age>72h → requests 비용(estimate) / Failed age>72h → $0 / Succeeded age>7d → $0 / 완료 후 7d 지난 Job(TTL 미설정) → $0

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/check/zombie_test.go`:

```go
package check

import (
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

func TestZombieCheck(t *testing.T) {
	now := time.Now()
	old := metav1.NewTime(now.Add(-96 * time.Hour))      // 4d
	week := metav1.NewTime(now.Add(-8 * 24 * time.Hour)) // 8d
	fresh := metav1.NewTime(now.Add(-1 * time.Hour))

	crashPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "crash", Namespace: "default", CreationTimestamp: old},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			}},
		}}},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			}},
		},
	}
	freshCrash := *crashPod.DeepCopy()
	freshCrash.Name = "fresh-crash"
	freshCrash.CreationTimestamp = fresh

	failedPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "failed", Namespace: "default", CreationTimestamp: old},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed},
	}
	donePod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "done", Namespace: "default", CreationTimestamp: week},
		Status:     corev1.PodStatus{Phase: corev1.PodSucceeded},
	}
	doneJob := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "old-job", Namespace: "default"},
		Status:     batchv1.JobStatus{CompletionTime: &week},
	}

	s := &snapshot.Snapshot{
		CollectedAt: now,
		Pods:        []corev1.Pod{crashPod, freshCrash, failedPod, donePod},
		Jobs:        []batchv1.Job{doneJob},
	}
	fs := ZombieCheck{}.Run(s, testModel())
	// crash(1) + failed(1) + succeeded(1) + job(1) = 4; fresh-crash excluded
	if len(fs) != 4 {
		t.Fatalf("findings = %d, want 4, got %+v", len(fs), fs)
	}
	var crashCost float64
	for _, f := range fs {
		if f.Target == "pod/default/crash" {
			crashCost = f.MonthlyCost
		}
	}
	// 1 vCPU + 1 GiB reserved: ~26.13/mo
	if crashCost < 26 || crashCost > 27 {
		t.Fatalf("crashloop pod cost = %v, want ~26.13", crashCost)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/check/ -run TestZombieCheck -v`
Expected: FAIL — `undefined: ZombieCheck`.

- [ ] **Step 3: 구현**

`internal/check/zombie.go`:

```go
package check

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/cost"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

const (
	crashLoopMinAge  = 72 * time.Hour
	staleFinishedAge = 7 * 24 * time.Hour
)

// ZombieCheck finds workloads that consume or clutter without serving:
// long-crashing pods still reserve their requests on a node (real cost);
// old Failed/Succeeded pods and finished Jobs are hygiene findings ($0).
type ZombieCheck struct{}

func (ZombieCheck) ID() string { return "zombie-workloads" }

func (ZombieCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	var out []Finding
	for _, p := range s.Pods {
		age := s.CollectedAt.Sub(p.CreationTimestamp.Time)
		target := fmt.Sprintf("pod/%s/%s", p.Namespace, p.Name)

		if isCrashLooping(p) && age > crashLoopMinAge {
			cpu, mem := podRequests(p)
			usd, basis := m.RequestsMonthlyUSD(cpu, mem)
			out = append(out, Finding{
				CheckID:     "zombie-workloads",
				Target:      target,
				Reason:      fmt.Sprintf("CrashLoopBackOff for %dd — requests stay reserved on the node", int(age.Hours()/24)),
				MonthlyCost: usd,
				CostBasis:   basis,
				Confidence:  Estimate,
				Action:      "fix the crash or scale the workload to zero",
			})
			continue
		}
		if p.Status.Phase == corev1.PodFailed && age > crashLoopMinAge {
			out = append(out, Finding{
				CheckID: "zombie-workloads", Target: target,
				Reason:     fmt.Sprintf("Failed pod lingering for %dd", int(age.Hours()/24)),
				Confidence: Certain,
				Action:     fmt.Sprintf("kubectl -n %s delete pod %s", p.Namespace, p.Name),
			})
		}
		if p.Status.Phase == corev1.PodSucceeded && age > staleFinishedAge {
			out = append(out, Finding{
				CheckID: "zombie-workloads", Target: target,
				Reason:     fmt.Sprintf("Succeeded pod lingering for %dd", int(age.Hours()/24)),
				Confidence: Certain,
				Action:     fmt.Sprintf("kubectl -n %s delete pod %s", p.Namespace, p.Name),
			})
		}
	}
	for _, j := range s.Jobs {
		if j.Status.CompletionTime == nil || j.Spec.TTLSecondsAfterFinished != nil {
			continue
		}
		age := s.CollectedAt.Sub(j.Status.CompletionTime.Time)
		if age > staleFinishedAge {
			out = append(out, Finding{
				CheckID:    "zombie-workloads",
				Target:     fmt.Sprintf("job/%s/%s", j.Namespace, j.Name),
				Reason:     fmt.Sprintf("Job finished %dd ago, no ttlSecondsAfterFinished", int(age.Hours()/24)),
				Confidence: Certain,
				Action:     "set ttlSecondsAfterFinished on the Job spec, or delete it",
			})
		}
	}
	return out
}

func isCrashLooping(p corev1.Pod) bool {
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
			return true
		}
	}
	return false
}
```

`All()`에 `ZombieCheck{}` 추가.

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/check/ -v`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/check/
git commit -m "feat: zombie-workloads check - crashloop cost, stale finished pods/jobs"
```

---

### Task 7: request 미설정 체크

**Files:**
- Create: `internal/check/noresources.go`
- Modify: `internal/check/check.go` — `All()`에 `NoRequestsCheck{}` 추가
- Test: `internal/check/noresources_test.go`

**Interfaces:**
- Consumes: `s.Pods`, `groupKey`
- Produces: `check.NoRequestsCheck` (ID: `"no-requests"`) — 워크로드 그룹당 Finding 1개, MonthlyCost 0 (경고성)

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/check/noresources_test.go`:

```go
package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

func TestNoRequestsCheck(t *testing.T) {
	mkPod := func(name, rsName string, withRequests bool) corev1.Pod {
		c := corev1.Container{Name: "app"}
		if withRequests {
			c.Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			}}
		}
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rsName}},
			},
			Spec:   corev1.PodSpec{Containers: []corev1.Container{c}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
	}
	s := &snapshot.Snapshot{Pods: []corev1.Pod{
		mkPod("bad-abc12-x1", "bad-abc12", false),
		mkPod("bad-abc12-x2", "bad-abc12", false), // same group → dedupe to 1
		mkPod("good-def34-y1", "good-def34", true),
	}}
	fs := NoRequestsCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1 (deduped per workload), got %+v", len(fs), fs)
	}
	if fs[0].Target != "default/ReplicaSet/bad" || fs[0].MonthlyCost != 0 {
		t.Fatalf("unexpected: %+v", fs[0])
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/check/ -run TestNoRequestsCheck -v`
Expected: FAIL — `undefined: NoRequestsCheck`.

- [ ] **Step 3: 구현**

`internal/check/noresources.go`:

```go
package check

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/cost"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

// NoRequestsCheck flags workloads whose containers set no resource
// requests: their cost is unschedulable/unpredictable and they distort
// node utilization math. Warning only — no dollar amount.
type NoRequestsCheck struct{}

func (NoRequestsCheck) ID() string { return "no-requests" }

func (NoRequestsCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	seen := map[string]bool{}
	var out []Finding
	for _, p := range s.Pods {
		if p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodPending {
			continue
		}
		missing := false
		for _, c := range p.Spec.Containers {
			_, hasCPU := c.Resources.Requests[corev1.ResourceCPU]
			_, hasMem := c.Resources.Requests[corev1.ResourceMemory]
			if !hasCPU || !hasMem {
				missing = true
				break
			}
		}
		if !missing {
			continue
		}
		key := groupKey(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Finding{
			CheckID:    "no-requests",
			Target:     key,
			Reason:     "containers without CPU/memory requests — cost unpredictable, scheduling unprotected",
			Confidence: Certain,
			Action:     "set resources.requests on every container",
		})
	}
	return out
}
```

`All()`에 `NoRequestsCheck{}` 추가.

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/check/ -v`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/check/
git commit -m "feat: no-requests check - warn on workloads without resource requests"
```

---

### Task 8: 유휴 GPU 체크

**Files:**
- Create: `internal/check/gpu.go`
- Modify: `internal/check/check.go` — `All()`에 `GPUCheck{}` 추가
- Test: `internal/check/gpu_test.go`

**Interfaces:**
- Consumes: `s.Nodes`, `s.Pods`, `(*cost.Model).NodeMonthlyUSD`
- Produces: `check.GPUCheck` (ID: `"idle-gpu"`)
- 룰: GPU allocatable > 0인 노드에서 GPU request 합계가 0 → 노드 전체 비용 finding(estimate). 일부만 사용 → 미사용 개수 경고($0).

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/check/gpu_test.go`:

```go
package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

func TestGPUCheck(t *testing.T) {
	gpu := corev1.ResourceName("nvidia.com/gpu")
	mkNode := func(name string, gpus string) corev1.Node {
		return corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name,
				Labels: map[string]string{"node.kubernetes.io/instance-type": "p3.2xlarge"}},
			Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{gpu: resource.MustParse(gpus)}},
		}
	}
	gpuPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "train", Namespace: "ml"},
		Spec: corev1.PodSpec{
			NodeName: "gpu-used",
			Containers: []corev1.Container{{Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{gpu: resource.MustParse("1")}}}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	s := &snapshot.Snapshot{
		Nodes: []corev1.Node{
			mkNode("gpu-idle", "1"), // no gpu pods → full node cost finding
			mkNode("gpu-used", "1"), // fully requested → no finding
		},
		Pods: []corev1.Pod{gpuPod},
	}
	fs := GPUCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1, got %+v", len(fs), fs)
	}
	// p3.2xlarge: 3.06 * 730 = 2233.8
	if fs[0].Target != "node/gpu-idle" || fs[0].MonthlyCost < 2233 || fs[0].MonthlyCost > 2235 {
		t.Fatalf("unexpected: %+v", fs[0])
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/check/ -run TestGPUCheck -v`
Expected: FAIL — `undefined: GPUCheck`.

- [ ] **Step 3: 구현**

`internal/check/gpu.go`:

```go
package check

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/cost"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

var gpuResource = corev1.ResourceName("nvidia.com/gpu")

// GPUCheck finds GPU nodes whose GPUs nobody requested. GPU instances
// are the most expensive line item in most clusters; an idle one is
// the single biggest finding a scan can produce.
type GPUCheck struct{}

func (GPUCheck) ID() string { return "idle-gpu" }

func (GPUCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	var out []Finding
	for _, n := range s.Nodes {
		capQ, ok := n.Status.Allocatable[gpuResource]
		if !ok || capQ.IsZero() {
			continue
		}
		var requested int64
		for _, p := range s.Pods {
			if p.Spec.NodeName != n.Name ||
				p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
				continue
			}
			for _, c := range p.Spec.Containers {
				if q, ok := c.Resources.Requests[gpuResource]; ok {
					requested += q.Value()
				}
			}
		}
		switch {
		case requested == 0:
			usd, basis := m.NodeMonthlyUSD(n)
			out = append(out, Finding{
				CheckID:     "idle-gpu",
				Target:      "node/" + n.Name,
				Reason:      fmt.Sprintf("node has %s GPU(s), zero requested by any pod", capQ.String()),
				MonthlyCost: usd,
				CostBasis:   "whole node priced; " + basis,
				Confidence:  Estimate, // allocation-based, not utilization-based
				Action:      "drain and scale down the GPU node pool if no GPU workloads are planned",
			})
		case requested < capQ.Value():
			out = append(out, Finding{
				CheckID:    "idle-gpu",
				Target:     "node/" + n.Name,
				Reason:     fmt.Sprintf("%d of %s GPU(s) unrequested", capQ.Value()-requested, capQ.String()),
				Confidence: Estimate,
				Action:     "consolidate GPU workloads to free a whole node",
			})
		}
	}
	return out
}
```

`All()`에 `GPUCheck{}` 추가.

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/check/ -v`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/check/
git commit -m "feat: idle-gpu check - unrequested GPU nodes priced at full node cost"
```

---

### Task 9: 저활용 노드 체크

**Files:**
- Create: `internal/check/nodeutil.go`
- Modify: `internal/check/check.go` — `All()`에 `NodeUtilCheck{}` 추가
- Test: `internal/check/nodeutil_test.go`

**Interfaces:**
- Consumes: `s.Nodes`, `s.Pods`, `podRequests`, `(*cost.Model).NodeMonthlyUSD`
- Produces: `check.NodeUtilCheck` (ID: `"underutilized-nodes"`)
- 룰: control-plane 라벨 노드 제외. 노드 위 Running/Pending 파드 requests 합계가 allocatable의 CPU·메모리 **모두 50% 미만** → 통합 후보 finding (노드 비용, estimate).

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/check/nodeutil_test.go`:

```go
package check

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

func TestNodeUtilCheck(t *testing.T) {
	alloc := corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("4"),
		corev1.ResourceMemory: resource.MustParse("16Gi"),
	}
	mkNode := func(name string, labels map[string]string) corev1.Node {
		return corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
			Status:     corev1.NodeStatus{Allocatable: alloc},
		}
	}
	mkPod := func(name, node, cpu, mem string) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: corev1.PodSpec{NodeName: node, Containers: []corev1.Container{{
				Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse(cpu),
					corev1.ResourceMemory: resource.MustParse(mem),
				}}}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning},
		}
	}
	s := &snapshot.Snapshot{
		Nodes: []corev1.Node{
			mkNode("empty-node", nil),
			mkNode("busy-node", nil),
			mkNode("cp", map[string]string{"node-role.kubernetes.io/control-plane": ""}),
		},
		Pods: []corev1.Pod{
			mkPod("big", "busy-node", "3", "12Gi"),
			mkPod("tiny", "empty-node", "100m", "256Mi"),
		},
	}
	fs := NodeUtilCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1, got %+v", len(fs), fs)
	}
	if fs[0].Target != "node/empty-node" {
		t.Fatalf("unexpected target: %s", fs[0].Target)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/check/ -run TestNodeUtilCheck -v`
Expected: FAIL — `undefined: NodeUtilCheck`.

- [ ] **Step 3: 구현**

`internal/check/nodeutil.go`:

```go
package check

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/cost"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

const nodeUtilThresholdPct = 50

// NodeUtilCheck finds worker nodes where both CPU and memory requests
// sit under 50% of allocatable: consolidation candidates. Node-level
// findings carry the biggest single dollar amounts after GPUs.
type NodeUtilCheck struct{}

func (NodeUtilCheck) ID() string { return "underutilized-nodes" }

func (NodeUtilCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	reqCPU := map[string]int64{}
	reqMem := map[string]int64{}
	for _, p := range s.Pods {
		if p.Spec.NodeName == "" ||
			(p.Status.Phase != corev1.PodRunning && p.Status.Phase != corev1.PodPending) {
			continue
		}
		c, mem := podRequests(p)
		reqCPU[p.Spec.NodeName] += c
		reqMem[p.Spec.NodeName] += mem
	}

	var out []Finding
	for _, n := range s.Nodes {
		if _, isCP := n.Labels["node-role.kubernetes.io/control-plane"]; isCP {
			continue
		}
		allocCPU := n.Status.Allocatable.Cpu().MilliValue()
		allocMem := n.Status.Allocatable.Memory().Value()
		if allocCPU == 0 || allocMem == 0 {
			continue
		}
		cpuPct := reqCPU[n.Name] * 100 / allocCPU
		memPct := reqMem[n.Name] * 100 / allocMem
		if cpuPct >= nodeUtilThresholdPct || memPct >= nodeUtilThresholdPct {
			continue
		}
		usd, basis := m.NodeMonthlyUSD(n)
		out = append(out, Finding{
			CheckID:     "underutilized-nodes",
			Target:      "node/" + n.Name,
			Reason:      fmt.Sprintf("requests at %d%% CPU / %d%% memory of allocatable — consolidation candidate", cpuPct, memPct),
			MonthlyCost: usd,
			CostBasis:   "full node price if drained away; " + basis,
			Confidence:  Estimate,
			Action:      "consider bin-packing via cluster-autoscaler/Karpenter consolidation, then remove the node",
		})
	}
	return out
}
```

`All()`에 `NodeUtilCheck{}` 추가.

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/check/ -v`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/check/
git commit -m "feat: underutilized-nodes check - sub-50% request nodes as consolidation candidates"
```

---

### Task 10: 러프 Right-sizing 체크 (metrics-server)

**Files:**
- Create: `internal/check/rightsizing.go`
- Modify: `internal/check/check.go` — `All()`에 `RightsizingCheck{}` 추가
- Test: `internal/check/rightsizing_test.go`

**Interfaces:**
- Consumes: `s.HasMetrics`, `s.PodUsage`, `s.Pods`, `groupKey`, `podRequests`, `fmtMem`, `max64`, `(*cost.Model).RequestsMonthlyUSD`
- Produces: `check.RightsizingCheck` (ID: `"overprovisioned-requests"`)
- 룰 (러프 — 단일 시점 샘플임을 CostBasis에 명시):
  - `HasMetrics == false` → 빈 결과 (Reporter가 Notes로 스킵 사유 표기)
  - Running 파드를 워크로드 그룹으로 합산. usage 없는 파드는 계산에서 제외 (이상치 방어).
  - 과대 판단: 그룹 합산 기준 `usage < request×40%` 이고 slack이 CPU ≥200m 또는 Mem ≥256Mi
  - 권장값: `usage × 1.5` (파드당 CPU 최소 50m, Mem 최소 64Mi 플로어). 과대 아닌 차원은 기존 request 유지.
  - 절감액 = (현 request − 권장) 을 RequestsMonthlyUSD로 환산.

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/check/rightsizing_test.go`:

```go
package check

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

func rsPod(name, rsName, cpu, mem string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "prod",
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rsName}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(mem),
			}}}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

func TestRightsizingSkipsWithoutMetrics(t *testing.T) {
	s := &snapshot.Snapshot{HasMetrics: false, Pods: []corev1.Pod{rsPod("a-x1-p", "a-x1", "2", "4Gi")}}
	if fs := RightsizingCheck{}.Run(s, testModel()); len(fs) != 0 {
		t.Fatalf("expected no findings without metrics, got %+v", fs)
	}
}

func TestRightsizingFindsOverprovisioned(t *testing.T) {
	// requests: 2 vCPU / 4Gi, usage: 200m / 512Mi → way over
	s := &snapshot.Snapshot{
		HasMetrics: true,
		Pods:       []corev1.Pod{rsPod("web-abc12-p1", "web-abc12", "2", "4Gi")},
		PodUsage: map[string]snapshot.PodUsage{
			snapshot.Key("prod", "web-abc12-p1"): {CPUMilli: 200, MemBytes: 512 * MiB},
		},
	}
	fs := RightsizingCheck{}.Run(s, testModel())
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1, got %+v", len(fs), fs)
	}
	f := fs[0]
	if f.Target != "prod/ReplicaSet/web" || f.Confidence != Estimate {
		t.Fatalf("unexpected: %+v", f)
	}
	if f.MonthlyCost <= 0 {
		t.Fatalf("expected positive savings, got %v", f.MonthlyCost)
	}
	if !strings.Contains(f.CostBasis, "point-in-time") {
		t.Fatalf("basis must disclose rough sampling: %q", f.CostBasis)
	}
}

func TestRightsizingIgnoresWellSized(t *testing.T) {
	// usage 80% of request → no finding
	s := &snapshot.Snapshot{
		HasMetrics: true,
		Pods:       []corev1.Pod{rsPod("ok-abc12-p1", "ok-abc12", "1", "1Gi")},
		PodUsage: map[string]snapshot.PodUsage{
			snapshot.Key("prod", "ok-abc12-p1"): {CPUMilli: 800, MemBytes: 820 * MiB},
		},
	}
	if fs := RightsizingCheck{}.Run(s, testModel()); len(fs) != 0 {
		t.Fatalf("expected no findings, got %+v", fs)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/check/ -run TestRightsizing -v`
Expected: FAIL — `undefined: RightsizingCheck`.

- [ ] **Step 3: 구현**

`internal/check/rightsizing.go`:

```go
package check

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/DPS0340/kubeoptimizer/pkg/cost"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

const (
	overprovisionPct = 40  // usage below this % of request = overprovisioned
	minSlackCPUMilli = 200 // ignore trivial slack
	minSlackMemBytes = 256 * MiB
	floorCPUMilli    = 50 // per-pod recommendation floors
	floorMemBytes    = 64 * MiB
)

// RightsizingCheck (rough tier): compares live metrics-server usage
// against requests per workload. One point-in-time sample — findings
// are estimates and say so. The paid tier replaces this with
// Prometheus p95/p99 over a window.
type RightsizingCheck struct{}

func (RightsizingCheck) ID() string { return "overprovisioned-requests" }

type rsAgg struct {
	reqCPU, reqMem, useCPU, useMem int64
	pods                           int64
}

func (RightsizingCheck) Run(s *snapshot.Snapshot, m *cost.Model) []Finding {
	if !s.HasMetrics {
		return nil
	}
	groups := map[string]*rsAgg{}
	for _, p := range s.Pods {
		if p.Status.Phase != corev1.PodRunning {
			continue
		}
		u, ok := s.PodUsage[snapshot.Key(p.Namespace, p.Name)]
		if !ok {
			continue // no usage sample — exclude rather than guess
		}
		rc, rm := podRequests(p)
		if rc == 0 && rm == 0 {
			continue // covered by no-requests check
		}
		g := groups[groupKey(p)]
		if g == nil {
			g = &rsAgg{}
			groups[groupKey(p)] = g
		}
		g.reqCPU += rc
		g.reqMem += rm
		g.useCPU += u.CPUMilli
		g.useMem += u.MemBytes
		g.pods++
	}

	var out []Finding
	for key, g := range groups {
		overCPU := g.reqCPU >= minSlackCPUMilli && g.useCPU*100 < g.reqCPU*overprovisionPct
		overMem := g.reqMem >= minSlackMemBytes && g.useMem*100 < g.reqMem*overprovisionPct
		if !overCPU && !overMem {
			continue
		}
		recCPU, recMem := g.reqCPU, g.reqMem
		if overCPU {
			recCPU = max64(g.useCPU*3/2, floorCPUMilli*g.pods)
		}
		if overMem {
			recMem = max64(g.useMem*3/2, floorMemBytes*g.pods)
		}
		usd, basis := m.RequestsMonthlyUSD(g.reqCPU-recCPU, g.reqMem-recMem)
		out = append(out, Finding{
			CheckID: "overprovisioned-requests",
			Target:  key,
			Reason: fmt.Sprintf("requests far above live usage (cpu %dm req vs %dm used, mem %s req vs %s used, %d pods)",
				g.reqCPU, g.useCPU, fmtMem(g.reqMem), fmtMem(g.useMem), g.pods),
			MonthlyCost: usd,
			CostBasis:   "rough point-in-time metrics-server sample (usage ×1.5 headroom); " + basis,
			Confidence:  Estimate,
			Action: fmt.Sprintf("reduce total requests to ~cpu %dm / mem %s, verify over a longer window first",
				recCPU, fmtMem(recMem)),
		})
	}
	return out
}
```

`All()`에 `RightsizingCheck{}` 추가 — 최종 레지스트리:

```go
func All() []Check {
	return []Check{
		RightsizingCheck{},
		NodeUtilCheck{},
		GPUCheck{},
		PVCheck{},
		LBCheck{},
		ZombieCheck{},
		NoRequestsCheck{},
	}
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/check/ -v`
Expected: PASS (전체 체크 테스트)

- [ ] **Step 5: 커밋**

```bash
git add internal/check/
git commit -m "feat: overprovisioned-requests rough right-sizing via metrics-server"
```

---

### Task 11: Report 집계 + 터미널/JSON 렌더러

**Files:**
- Create: `internal/report/report.go`
- Create: `internal/report/render.go`
- Test: `internal/report/report_test.go`

**Interfaces:**
- Consumes: `check.Finding`, `snapshot.Snapshot`
- Produces:
  - `report.Report{ Cluster string; CollectedAt time.Time; Findings []check.Finding; TotalMonthlyUSD float64; Notes []string }` — JSON 태그 포함
  - `report.Build(cluster string, s *snapshot.Snapshot, findings []check.Finding) Report` — 비용 내림차순 정렬 + 합계 + Notes 구성
  - `report.RenderTable(w io.Writer, r Report)`
  - `report.RenderJSON(w io.Writer, r Report) error`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/report/report_test.go`:

```go
package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DPS0340/kubeoptimizer/pkg/check"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

func fixtures() (*snapshot.Snapshot, []check.Finding) {
	s := &snapshot.Snapshot{HasMetrics: false, Errors: []string{"jobs: forbidden"}}
	fs := []check.Finding{
		{CheckID: "unused-pv", Target: "pv/small", MonthlyCost: 10, Confidence: check.Certain},
		{CheckID: "idle-gpu", Target: "node/big", MonthlyCost: 2233.8, Confidence: check.Estimate},
	}
	return s, fs
}

func TestBuildSortsAndTotals(t *testing.T) {
	s, fs := fixtures()
	r := Build("https://cluster.example", s, fs)
	if r.Findings[0].CheckID != "idle-gpu" {
		t.Fatal("findings must be sorted by cost desc")
	}
	if r.TotalMonthlyUSD < 2243 || r.TotalMonthlyUSD > 2244 {
		t.Fatalf("total = %v, want ~2243.8", r.TotalMonthlyUSD)
	}
	joined := strings.Join(r.Notes, "\n")
	if !strings.Contains(joined, "metrics-server not detected") {
		t.Fatalf("notes must mention missing metrics: %v", r.Notes)
	}
	if !strings.Contains(joined, "jobs: forbidden") {
		t.Fatalf("notes must surface collection errors: %v", r.Notes)
	}
}

func TestRenderTable(t *testing.T) {
	s, fs := fixtures()
	var buf bytes.Buffer
	RenderTable(&buf, Build("https://cluster.example", s, fs))
	out := buf.String()
	for _, want := range []string{"idle-gpu", "node/big", "$2233.80", "TOTAL", "$2243.80"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderJSON(t *testing.T) {
	s, fs := fixtures()
	var buf bytes.Buffer
	if err := RenderJSON(&buf, Build("c", s, fs)); err != nil {
		t.Fatal(err)
	}
	var round Report
	if err := json.Unmarshal(buf.Bytes(), &round); err != nil {
		t.Fatalf("output must be valid JSON: %v", err)
	}
	if len(round.Findings) != 2 {
		t.Fatalf("roundtrip findings = %d", len(round.Findings))
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/report/ -v`
Expected: FAIL — `undefined: Build`.

- [ ] **Step 3: 구현**

`internal/report/report.go`:

```go
package report

import (
	"sort"
	"time"

	"github.com/DPS0340/kubeoptimizer/pkg/check"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

type Report struct {
	Cluster         string          `json:"cluster"`
	CollectedAt     time.Time       `json:"collected_at"`
	TotalMonthlyUSD float64         `json:"total_monthly_usd"`
	Findings        []check.Finding `json:"findings"`
	Notes           []string        `json:"notes,omitempty"`
}

// Build sorts findings by monthly cost (desc), totals them, and turns
// skipped data sources / collection failures into visible notes —
// never silent.
func Build(cluster string, s *snapshot.Snapshot, findings []check.Finding) Report {
	sort.SliceStable(findings, func(i, j int) bool {
		return findings[i].MonthlyCost > findings[j].MonthlyCost
	})
	var total float64
	for _, f := range findings {
		total += f.MonthlyCost
	}
	var notes []string
	if !s.HasMetrics {
		notes = append(notes, "metrics-server not detected — usage-based checks skipped (right-sizing)")
	}
	for _, e := range s.Errors {
		notes = append(notes, "collection failed: "+e)
	}
	return Report{
		Cluster:         cluster,
		CollectedAt:     s.CollectedAt,
		TotalMonthlyUSD: total,
		Findings:        findings,
		Notes:           notes,
	}
}
```

`internal/report/render.go`:

```go
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

func RenderTable(w io.Writer, r Report) {
	fmt.Fprintf(w, "kubeoptimizer scan — %s\n\n", r.Cluster)
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "EST./MO\tCONF\tCHECK\tTARGET\tREASON")
	for _, f := range r.Findings {
		cost := "-"
		if f.MonthlyCost > 0 {
			cost = fmt.Sprintf("$%.2f", f.MonthlyCost)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", cost, f.Confidence, f.CheckID, f.Target, f.Reason)
	}
	fmt.Fprintf(tw, "\nTOTAL\t\t\t\t$%.2f/mo estimated waste (%d findings)\n",
		r.TotalMonthlyUSD, len(r.Findings))
	tw.Flush()

	if len(r.Findings) > 0 {
		fmt.Fprintln(w, "\nTop actions:")
		max := len(r.Findings)
		if max > 5 {
			max = 5
		}
		for _, f := range r.Findings[:max] {
			if f.Action != "" {
				fmt.Fprintf(w, "  • [%s] %s\n", f.Target, f.Action)
			}
		}
	}
	for _, n := range r.Notes {
		fmt.Fprintf(w, "\n⚠ %s", n)
	}
	fmt.Fprintln(w)
}

func RenderJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/report/ -v`
Expected: PASS (3 tests)

- [ ] **Step 5: 커밋**

```bash
git add internal/report/
git commit -m "feat: report aggregation with table and JSON renderers"
```

---

### Task 12: CLI (cobra) + main

**Files:**
- Create: `cmd/root.go`
- Create: `cmd/scan.go`
- Create: `main.go`
- Test: `cmd/scan_test.go`

**Interfaces:**
- Consumes: `snapshot.Collect`, `check.All`, `cost.NewModel/DefaultRates`, `report.Build/RenderTable/RenderJSON`
- Produces: `kubeoptimizer scan` 명령. 플래그: `--kubeconfig`, `--context` (root persistent), `-o/--output table|json`, `--cpu-rate`, `--mem-rate` (scan)

- [ ] **Step 1: 실패하는 테스트 작성**

`cmd/scan_test.go`:

```go
package cmd

import "testing"

func TestValidateOutput(t *testing.T) {
	if err := validateOutput("table"); err != nil {
		t.Fatal(err)
	}
	if err := validateOutput("json"); err != nil {
		t.Fatal(err)
	}
	if err := validateOutput("yaml"); err == nil {
		t.Fatal("yaml must be rejected")
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./cmd/ -v`
Expected: FAIL — `undefined: validateOutput`.

- [ ] **Step 3: 구현**

`cmd/root.go`:

```go
package cmd

import (
	"github.com/spf13/cobra"
)

var (
	kubeconfig  string
	kubecontext string
)

var rootCmd = &cobra.Command{
	Use:   "kubeoptimizer",
	Short: "Read-only Kubernetes cost waste scanner",
	Long: "kubeoptimizer scans a cluster with get/list access only and reports\n" +
		"estimated monthly waste. It never mutates anything and never phones home.",
	SilenceUsage: true,
}

func Execute() error { return rootCmd.Execute() }

func init() {
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig (default: standard loading rules)")
	rootCmd.PersistentFlags().StringVar(&kubecontext, "context", "", "kubeconfig context to use")
}
```

`cmd/scan.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/DPS0340/kubeoptimizer/pkg/check"
	"github.com/DPS0340/kubeoptimizer/pkg/cost"
	"github.com/DPS0340/kubeoptimizer/pkg/report"
	"github.com/DPS0340/kubeoptimizer/pkg/snapshot"
)

var (
	outputFormat string
	cpuRate      float64
	memRate      float64
)

func validateOutput(f string) error {
	if f != "table" && f != "json" {
		return fmt.Errorf("invalid --output %q (want table or json)", f)
	}
	return nil
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan the cluster and report estimated monthly waste",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateOutput(outputFormat); err != nil {
			return err
		}
		cfg, err := buildConfig()
		if err != nil {
			return fmt.Errorf("kubeconfig: %w", err)
		}
		kube, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return err
		}
		// Client construction never contacts the cluster; actual
		// availability is probed by Collect and reflected in the report.
		mc, err := metricsclient.NewForConfig(cfg)
		if err != nil {
			mc = nil
		}

		rates := cost.DefaultRates()
		if cpuRate > 0 {
			rates.CPUHourlyUSD = cpuRate
		}
		if memRate > 0 {
			rates.MemGBHourlyUSD = memRate
		}
		model := cost.NewModel(rates)

		snap := snapshot.Collect(cmd.Context(), kube, mc)
		var findings []check.Finding
		for _, c := range check.All() {
			findings = append(findings, c.Run(snap, model)...)
		}
		rep := report.Build(cfg.Host, snap, findings)
		if outputFormat == "json" {
			return report.RenderJSON(os.Stdout, rep)
		}
		report.RenderTable(os.Stdout, rep)
		return nil
	},
}

func buildConfig() (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubecontext}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
}

func init() {
	scanCmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "output format: table|json")
	scanCmd.Flags().Float64Var(&cpuRate, "cpu-rate", 0, "override $/vCPU-hour (for on-prem or custom pricing)")
	scanCmd.Flags().Float64Var(&memRate, "mem-rate", 0, "override $/GiB-hour")
	rootCmd.AddCommand(scanCmd)
}
```

`main.go`:

```go
package main

import (
	"os"

	"github.com/DPS0340/kubeoptimizer/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 4: 통과 + 빌드 확인**

Run: `go test ./cmd/ -v && go build ./... && go vet ./...`
Expected: PASS, 빌드/vet 클린.

- [ ] **Step 5: 스모크 (수동, 클러스터 있으면)**

```bash
go run . scan --output json | head -30
```

Expected: 현재 kubeconfig 컨텍스트 기준 JSON 리포트 (클러스터 없으면 kubeconfig 에러 메시지 — 정상).

- [ ] **Step 6: 커밋**

```bash
git add main.go cmd/
git commit -m "feat: kubeoptimizer scan CLI with output/rate flags"
```

---

### Task 13: RBAC 매니페스트 + README + LICENSE

**Files:**
- Create: `deploy/rbac.yaml`
- Create: `README.md`
- Create: `LICENSE`

**Interfaces:**
- Consumes: 전체 제품 동작 (문서화 대상)
- Produces: 공개 가능한 저장소 상태

- [ ] **Step 1: RBAC 매니페스트 작성**

`deploy/rbac.yaml`:

```yaml
# Minimal read-only access for kubeoptimizer. get/list only — the tool
# has no mutating code paths at all.
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: kubeoptimizer-readonly
rules:
  - apiGroups: [""]
    resources:
      - nodes
      - pods
      - persistentvolumes
      - persistentvolumeclaims
      - services
      - endpoints
    verbs: [get, list]
  - apiGroups: [batch]
    resources: [jobs]
    verbs: [get, list]
  - apiGroups: [metrics.k8s.io]
    resources: [pods, nodes]
    verbs: [get, list]
```

- [ ] **Step 2: README 작성**

`README.md` (전문):

````markdown
# kubeoptimizer

**Read-only Kubernetes cost waste scanner.** Point it at any cluster
with a kubeconfig and get an estimated monthly waste report in ~30
seconds. No agent, no telemetry, no mutations — `get`/`list` only.

```
$ kubeoptimizer scan

kubeoptimizer scan — https://my-cluster

EST./MO    CONF      CHECK                     TARGET              REASON
$2233.80   estimate  idle-gpu                  node/gpu-node-1     node has 1 GPU(s), zero requested by any pod
$140.16    estimate  underutilized-nodes       node/worker-7       requests at 12% CPU / 31% memory of allocatable
$10.00     certain   unused-pv                 pv/pvc-8f3a...      PersistentVolume is Released — not bound to any claim
...
TOTAL      $2402.51/mo estimated waste (9 findings)
```

## Install

```
go install github.com/DPS0340/kubeoptimizer@latest
```

## What it finds

| Check | Needs | What it catches |
|---|---|---|
| `overprovisioned-requests` | metrics-server | requests far above live usage (rough right-sizing) |
| `underutilized-nodes` | — | nodes under 50% requested — consolidation candidates |
| `idle-gpu` | — | GPU nodes with zero GPU requests |
| `unused-pv` | — | Released/unbound PVs, PVCs no pod mounts |
| `idle-loadbalancer` | — | LoadBalancer services with no ready endpoints |
| `zombie-workloads` | — | long-term CrashLoop (reserved requests), stale finished pods/jobs |
| `no-requests` | — | containers without resource requests (unpredictable cost) |

Data sources are auto-detected. No metrics-server? API-only checks
still run and the report says exactly what was skipped.

## Pricing model

Node costs come from an embedded on-demand pricing table keyed by the
`node.kubernetes.io/instance-type` label, falling back to per-resource
rates. On-prem? Override with `--cpu-rate` / `--mem-rate`. Every dollar
figure carries its derivation and a confidence level (`certain` /
`estimate`) — no inflated numbers.

## Security

- **Read-only by construction:** no mutating API verbs exist in the codebase.
- Minimal RBAC: [`deploy/rbac.yaml`](deploy/rbac.yaml).
- Zero network calls besides the Kubernetes API. No telemetry, ever.

## Roadmap

Free (this repo): everything above. Planned paid tier: Prometheus-based
p95/p99 precision right-sizing, HTML executive reports, trend tracking,
CI cost-regression mode, multi-cluster aggregation.

## License

Apache-2.0
````

- [ ] **Step 3: LICENSE 추가**

```bash
curl -s https://www.apache.org/licenses/LICENSE-2.0.txt > LICENSE
```

Expected: Apache-2.0 전문이 담긴 LICENSE 파일.

- [ ] **Step 4: 전체 테스트 재확인**

Run: `go test ./... && go vet ./...`
Expected: 전 패키지 PASS.

- [ ] **Step 5: 커밋**

```bash
git add deploy/ README.md LICENSE
git commit -m "docs: README, minimal read-only RBAC manifest, Apache-2.0 license"
```

---

### Task 14: kind E2E 스모크 (선택 — kind/docker 필요)

**Files:**
- Create: `hack/e2e-kind.sh`
- Create: `hack/fixtures.yaml`

**Interfaces:**
- Consumes: 빌드된 `kubeoptimizer` 바이너리 전체
- Produces: 실클러스터 스모크 검증 스크립트 (CI용)

- [ ] **Step 1: 픽스처 매니페스트 작성**

`hack/fixtures.yaml`:

```yaml
# Deliberate waste for the e2e smoke test.
apiVersion: v1
kind: PersistentVolume
metadata:
  name: e2e-unbound-pv
spec:
  capacity: { storage: 10Gi }
  accessModes: [ReadWriteOnce]
  hostPath: { path: /tmp/e2e-unbound }
---
apiVersion: v1
kind: Service
metadata:
  name: e2e-idle-lb
  namespace: default
spec:
  type: LoadBalancer
  selector: { app: does-not-exist }
  ports: [{ port: 80 }]
---
apiVersion: v1
kind: Pod
metadata:
  name: e2e-no-requests
  namespace: default
spec:
  containers:
    - name: pause
      image: registry.k8s.io/pause:3.9
```

- [ ] **Step 2: 스모크 스크립트 작성**

`hack/e2e-kind.sh`:

```bash
#!/usr/bin/env bash
# E2E smoke: plant known waste in a kind cluster, expect the scanner
# to find it. Requires: kind, docker, kubectl, jq.
set -euo pipefail
cd "$(dirname "$0")/.."

CLUSTER=kubeoptimizer-e2e
kind create cluster --name "$CLUSTER" --wait 120s
trap 'kind delete cluster --name "$CLUSTER"' EXIT

kubectl apply -f hack/fixtures.yaml
kubectl wait --for=condition=Ready pod/e2e-no-requests --timeout=60s

go build -o /tmp/kubeoptimizer .
OUT=$(/tmp/kubeoptimizer scan --output json)

echo "$OUT" | jq -e '.findings[] | select(.check == "unused-pv" and .target == "pv/e2e-unbound-pv")' >/dev/null
echo "$OUT" | jq -e '.findings[] | select(.check == "idle-loadbalancer" and .target == "svc/default/e2e-idle-lb")' >/dev/null
echo "$OUT" | jq -e '.findings[] | select(.check == "no-requests")' >/dev/null
echo "E2E smoke: all expected findings present ✓"
```

- [ ] **Step 3: 실행 권한 + (kind 있으면) 실행**

```bash
chmod +x hack/e2e-kind.sh
command -v kind >/dev/null && ./hack/e2e-kind.sh || echo "kind not installed - script committed for CI"
```

Expected: kind 있으면 `E2E smoke: all expected findings present ✓`, 없으면 안내 메시지.

- [ ] **Step 4: 커밋**

```bash
git add hack/
git commit -m "test: kind e2e smoke script with planted waste fixtures"
```

---

## Self-Review 결과

- **스펙 커버리지:** 체크 #1(러프)=T10, #2=T9, #3=T8, #4=T4, #5=T5, #6=T6, #7=T7 ✓ / 3단계 데이터 소스 자동 감지=T2+T12 ✓ / Cost Model 단가표+폴백=T1 ✓ / 터미널·JSON=T11 ✓ / 조용한 실패 금지(Notes)=T2+T11 ✓ / RBAC·README·무텔레메트리=T13 ✓ / 테스트 전략(fake client, 경계값, 렌더러, kind e2e)=각 태스크+T14 ✓. **Phase 1 항목(Prometheus 정밀, HTML, 라이선스 게이트, krew/brew 배포)은 의도적으로 이 계획 밖** — 별도 계획으로 작성.
- **플레이스홀더:** 없음 (모든 스텝에 실제 코드/명령 포함).
- **타입 일관성:** `Finding`/`Check`/`Snapshot`/`Model` 시그니처를 Interfaces 블록 기준으로 전 태스크 대조 완료. 렌더러 테스트의 `$2233.80` 값은 T1 단가표 `p3.2xlarge 3.06×730`과 일치.
