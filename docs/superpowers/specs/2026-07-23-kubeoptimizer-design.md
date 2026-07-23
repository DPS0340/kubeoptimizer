# 설계 문서 — kubeoptimizer: 쿠버네티스 비용 낭비 스캐너 (오픈코어 CLI)

- **작성일:** 2026-07-23
- **프로젝트:** kubeoptimizer
- **단계:** 브레인스토밍 완료 / 구현 계획(writing-plans) 대기
- **결정 경로:** 개발자/인프라팀 대상 → FinOps → k8s 특화 → API+메트릭 둘 다(3단계 자동 감지) → 오픈코어 CLI → Go

---

## 1. 배경 & 목표

경험 있는 백엔드/인프라 개발자(k8s·DB·보안·Go/Rust/Python)가 자신의 불공정 우위(쿠버네티스 깊이)를 활용해 **돈 버는 개발자 도구**를 만든다.

- **선택 근거:** 클라우드 비용 절감(FinOps)은 ROI가 리포트 자체로 증명되는 영역이라, 개발자 도구 중 드물게 현금화가 빠름. k8s 비용은 파악 자체가 어려워 "얼마나 새는지 모르는" 회사가 대부분. 시장 리더(Kubecost)가 IBM에 인수된 뒤 가볍고 독립적인 대안 니치가 비어 있음.
- **수익 모델:** 오픈코어. 무료 티어로 GitHub 스타·krew·입소문을 만들고, 팀/회사가 필요로 하는 기능(정밀 분석·경영 리포트·CI·멀티클러스터)을 라이선스 키로 판매.
- **핵심 논리:** 개인 개발자는 평생 무료로 쓰며 소문내고, 팀장이 "상사에게 보여줄 리포트"가 필요해지는 순간 회사 카드를 긁는다.

## 2. 제품 정의

> **kubeconfig만 있으면 30초 안에 "이 클러스터에서 월 얼마 새는지" 알려주는 읽기전용 Go CLI.**

- **이름:** `kubeoptimizer` (단일 바이너리). krew 플러그인 배포 병행(예: `kubectl optimize scan`).
- **설치:** `brew install` / `go install` / krew / GitHub Releases 단일 바이너리.
- **핵심 UX:** `kubeoptimizer scan` 한 방 → 낭비 항목별 추정 월 비용이 터미널 테이블로 출력.

## 3. 시스템 아키텍처

### 3-1. 3단계 데이터 소스 자동 감지 (핵심 설계 축)

메트릭 의존성이 데모 문턱을 높이지 않도록, 데이터 소스는 **있으면 쓰고 없으면 우아하게 스킵**한다.

```
DataSources (자동 감지)
 ├─ k8s API (client-go)        ← 항상 사용. 이것만으로도 리포트가 나옴
 ├─ metrics-server             ← 있으면: 현재 사용량 기반 러프 right-sizing
 └─ Prometheus (--prom-url)    ← 있으면: p95/p99 시계열 기반 정밀 right-sizing
        ↓
Check Engine — 각 체크는 동일 인터페이스 Check.Run(ctx, snapshot) → []Finding
        ↓
Cost Model — 노드 라벨(instance-type, region) → 클라우드 단가 매핑
             AWS/GCP/Azure 온디맨드 단가표 내장, 온프렘은 $/vCPU·$/GB 수동 설정
        ↓
Reporter — 터미널 테이블 / JSON / HTML 리포트
```

### 3-2. 컴포넌트

1. **Snapshot Collector** — 클러스터 상태를 한 번 수집해 불변 스냅샷으로 만든다. 체크들은 API를 직접 치지 않고 스냅샷만 읽는다 (API 부하 최소화 + 체크 단위테스트 용이).
2. **Check Engine** — 체크 하나 = 파일 하나. 동일 인터페이스. **룰이 곧 제품이므로 이 경계가 가장 중요.** 체크 추가가 곧 제품 성장.
3. **Cost Model** — 노드의 `node.kubernetes.io/instance-type`, region/zone 라벨로 내장 단가표 조회. 매핑 실패 시 사용자 설정 단가($/vCPU·h, $/GB·h)로 폴백. 모든 추정치에 산출 근거를 남긴다.
4. **Reporter** — 터미널(사람용), JSON(파이프라인용), HTML(유료: 경영 리포트용).
5. **License Gate** — 유료 기능 분기. 같은 바이너리에서 라이선스 키(오프라인 서명 검증)로 해금. 폰홈 없음.

### 3-3. Finding 구조

```
Finding {
  check: 어떤 체크가
  target: 무엇을 (ns/kind/name)
  reason: 왜 낭비인지
  monthly_cost: 추정 월 비용 (근거 포함)
  confidence: 확실(certain) | 추정(estimate)
  action: 권장 조치 (kubectl 명령 수준으로 구체적으로)
}
```

## 4. v1 체크 목록 (돈 큰 순)

| # | 체크 | 데이터 소스 | 비고 |
|---|---|---|---|
| 1 | 과대 request (request vs 실사용 p95) | metrics/Prom | **돈 제일 큼.** 무료=러프, 유료=정밀 |
| 2 | 저활용 노드 → 통합(consolidation) 제안 | API+metrics | 노드 단위 = 큰 금액 |
| 3 | 유휴 GPU 노드 / 미사용 GPU request | API+metrics | 단가 최강. 차별화 포인트 |
| 4 | Released/미부착 PV, 고아 PVC | API | 확실(certain) 등급 낭비 |
| 5 | 엔드포인트 없는 LoadBalancer 서비스 | API | 확실 등급. LB 단가 명확 |
| 6 | 좀비 워크로드 (장기 CrashLoop·Failed, 방치된 완료 Job/Pod) | API | |
| 7 | request/limit 미설정 워크로드 | API | 비용 예측불가 리스크로 표기 (금액 아닌 경고) |

비용과 무관한 순수 정리성 항목(미사용 ConfigMap/Secret 등)은 노이즈이므로 넣지 않는다.

## 5. 오픈코어 경계 (무료 vs 유료)

| | 무료 (OSS) | 유료 (라이선스 키) |
|---|---|---|
| 낭비 체크 전부 (#1~#7 — 무료는 metrics-server 수준의 러프 분석) | ✅ | ✅ |
| metrics-server 러프 right-sizing | ✅ | ✅ |
| 터미널/JSON 출력 | ✅ | ✅ |
| Prometheus 정밀 분석 (기간 지정 p95/p99) | — | ✅ |
| HTML 경영 리포트 ("상사에게 보여주는 문서") | — | ✅ |
| 히스토리/트렌드 추적 | — | ✅ |
| CI 모드 (비용 회귀 감지) | — | ✅ |
| 멀티클러스터 집계 | — | ✅ |

- **가격 스케치:** 클러스터당 연 $199 안팎. 개인 무료, 회사 유료. 출시 후 반응 보고 조정.
- **원칙:** 무료 티어만으로도 "월 $X 샌다"는 충격 숫자가 나와야 함. 유료는 그 숫자를 **정밀하게, 지속적으로, 보고 가능하게** 만드는 값.

## 6. 신뢰 설계 (남의 프로덕션에 들어가는 물건)

- **읽기전용 보장:** get/list만 사용. 최소 권한 RBAC(ClusterRole) 매니페스트 제공. mutate 동사 자체가 코드에 없음 — README 첫 줄에 명시.
- **조용한 실패 금지:** 데이터 소스 미감지 시 "Prometheus 미감지 — 정밀 분석 스킵됨"을 리포트에 명시. 권한 부족한 리소스는 "스킵됨(권한 없음)"으로 표기.
- **숫자 신뢰:** 모든 비용 추정에 산출 근거(단가 × 수량 × 시간) 표시. 뻥튀기 숫자는 신뢰를 죽인다. 불확실하면 confidence=estimate로 낮춰 표기.
- **텔레메트리 없음:** 클러스터 데이터 외부 전송 0. 라이선스 검증도 오프라인. 이것 자체가 영업 포인트.

## 7. 에러 처리

- API 호출 실패: 리소스 단위로 격리 — 한 리소스 목록 실패가 전체 스캔을 죽이지 않는다. 실패 항목은 리포트에 "수집 실패" 섹션으로 노출.
- 단가 매핑 실패(알 수 없는 인스턴스 타입): 해당 Finding은 금액 없이 항목만 표기 + 수동 단가 설정 안내.
- 이상치 방어: 메트릭 0/음수/결측 구간은 right-sizing 계산에서 제외하고 표본 수를 근거에 표시.

## 8. 테스트 전략

- **Check Engine:** fake client + fixture 스냅샷(YAML)으로 단위테스트. 실클러스터 불필요. 낭비 시나리오별 fixture가 곧 회귀 테스트.
- **Cost Model:** 단가표 경계값·매핑 실패 폴백 테스트.
- **Reporter:** golden file 테스트 (터미널/JSON/HTML).
- **E2E:** kind 클러스터에 낭비 시나리오(미부착 PV, 좀비 워크로드 등)를 심고 스캔 결과 검증. CI에서 실행.

## 9. 로드맵 & 고투마켓

### Phase 0 — OSS 런칭 (1~2주)
- API 체크 전부(#4~#7 중심) + metrics-server 러프 right-sizing + 터미널/JSON 출력.
- GitHub 공개, krew 등록, `brew tap`, r/kubernetes·HN·k8s Slack 런칭.
- **목표 지표:** 스타·이슈 유입, "우리 클러스터에서 월 $X 나왔다" 스크린샷 확보 (이게 최고의 마케팅 자산).

### Phase 1 — 과금 시작 (3~6주)
- Prometheus 정밀 right-sizing + HTML 경영 리포트 + 라이선스 키 게이트.
- 결제: Gumroad/Lemon Squeezy 등 저마찰 채널로 시작.

### Phase 2 — 확장 (수요 확인 후)
- 트렌드/CI 모드/멀티클러스터. SaaS 대시보드는 유료 고객이 요구하기 전까지 만들지 않는다.

## 10. 범위에서 제외 (YAGNI)

- **자동 수정(mutate) 기능** — 신뢰 설계와 정면충돌. 권장 조치 출력까지만.
- **SaaS 대시보드** — Phase 2 수요 확인 전 금지.
- **클라우드 청구서(빌링 API) 연동** — 노드 라벨 기반 추정으로 충분. 경쟁 레드오션.
- **Rust 재작성** — 하지 않는다. 승부처는 언어가 아니라 룰 정확도와 출시 속도.
