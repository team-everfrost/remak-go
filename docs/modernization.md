# 2023 시스템 결함과 개선 결과

이 문서는 “언어만 Go로 번역”하지 않고 기존 backend, embedding Lambda, scraping Lambda, frontend/extension 계약을 검토해 바꾼 이유를 기록합니다.

| 2023 구조/결함 | 위험 | 현재 처리 | 남은 제한 |
|---|---|---|---|
| Nest profile update가 client `role`을 받음 | 자기 자신을 PLUS/ADMIN으로 올릴 권한 상승 | wire field는 받되 service가 무시; role/plan은 관리자 정책만 변경 | 관리자용 billing/control plane은 아직 없음 |
| OTP code를 DB 평문 저장 | DB 유출 시 즉시 인증 우회 | challenge별 HMAC hash, 10분 TTL, 5회 제한, 시간당 rate limit, verified/consumed 분리 | IP/device rate limit은 reverse proxy/WAF 단계 추가 가능 |
| access token 위주, 세션 rotation 부재 | 탈취 token 장기 사용/강제 로그아웃 어려움 | 15분 JWT + random refresh hash 저장 + rotation/revoke | 전 기기 session UI는 아직 없음 |
| Lambda들이 DB credential과 table을 공유 | schema coupling, VPC/secret 범위 확대, 경합 status update | scraper는 S3/SQS event만 사용; 상태 소유자는 Go | AWS와 OCI 사이 network 장애는 queue 지연으로 보임 |
| DB commit 뒤 바로 SQS 호출 | commit/SQS 사이 dual-write 유실 | transactional outbox + lease/backoff/dead-letter | outbox oldest-age alert 필요 |
| result 중복/늦은 결과 방어 부족 | 재전달이 최신 content/status를 덮어씀 | inbox event idempotency + job/document/version/current 검사 | SQS standard queue라 순서는 여전히 보장하지 않으며 설계가 이를 흡수 |
| document 한 row를 덮어쓰기 | 비동기 작업의 기준 version 불명확 | immutable `document_versions` + `current_version` | version history UI/복원 API는 아직 없음 |
| `updated_at` cursor | 수정할 때 page 사이 이동, 중복/누락 | immutable `(created_at,id)` cursor + opaque page token | 기존 `cursor` query는 호환용으로만 수용 |
| bigint 내부 ID + UUID string 공개 ID 이중화 | join/API mapping 복잡, 분산 생성 불편 | UUIDv7 단일 PK/public ID, 정상 legacy UUID 보존 | UUID timestamp를 business time으로 쓰지 않음 |
| ada-002/vector raw SQL, global query embedding cache | model 고정, privacy/invalid cache, lexical 부재 | provider adapter, vector(1536), weighted FTS+trigram+vector RRF, AI 실패 lexical degrade | dimension 변경은 별도 column/index migration 필요 |
| PDF/image/text 및 summary/tag가 오래된 embedding Lambda에 묶임 | DB 직접 접근, Node 2023 dependency, Azure OCR 추가 서비스 | UTF-8/PDF는 Go worker, image는 multimodal AI 설명+OCR, 구조화 summary/tag+공통 chunk/embed transaction | scanned PDF OCR adapter는 아직 명시적 reject |
| upload가 파일별 quota 조회 후 순차 DB write | 동시 요청 quota race, 중간 성공 orphan S3 | account row lock+합산 quota transaction, DB 실패 시 S3 보상 삭제 | S3/DB의 원자 commit은 불가능하므로 cleanup 관찰 필요 |
| soft delete/탈퇴와 S3 delete 동기 결합 | S3 장애나 늦은 Lambda 결과가 사용자 요청 실패/orphan 유발 | soft delete+cleanup job transaction, late-result requeue, 30일 뒤 hard delete maintenance | versioned S3 noncurrent object는 lifecycle 30일 보존 |
| scraper Node18, 2023 Chromium/deps | runtime EOL/보안/현대 사이트 호환 저하 | Node24, current Puppeteer/Chromium, strict TS, reproducible package | x86 Lambda package 약 75MiB, cold start 비용 존재 |
| Chromium ESM dependency를 CommonJS ZIP으로 묶음 | typecheck/unit test는 통과하지만 Lambda init에서 `ERR_REQUIRE_ESM` | 배포 ZIP을 ESM으로 만들고 package 단계에서 완성 entrypoint import smoke test, 공식 Lambda Node24 image E2E | architecture별 실제 AWS canary는 배포 뒤 추가 |
| browser가 initial URL만 신뢰/위험 flags | redirect/subresource SSRF, metadata 접근 | 최초+모든 request DNS/IP 검증, private/reserved 차단, userinfo/내부 suffix 차단, insecure flags 제거 | DNS check/connection 사이 rebinding은 VPC egress로 보강 권장 |
| SQS batch 전체 실패 | 성공 message도 재처리 | `ReportBatchItemFailures`, batch size 1, attempt 일치 | 실제 AWS event-source smoke test 필요 |
| Serverless Framework v4 자동 배포 | login/license/장기 key 종속 | esbuild ZIP + 순수 CloudFormation, CI는 검증만 | OIDC 배포 workflow는 계정 정책 결정 후 추가 |
| LocalStack 전체 surface | 로컬 자원 사용/라이선스/복잡도 | 필요한 S3/SQS만 MiniStack 1.4.2 | IAM/Lambda parity는 staging 책임 |
| microservice를 먼저 나눌 유혹 | OCI 한 대에서 network hop/운영비/장애점 증가 | Go modular monolith, API/worker process만 분리 | 트래픽/팀 ownership이 실제로 갈릴 때 event 경계로 분리 |
| Go patch version을 느슨하게 둠 | 오래된 표준 라이브러리 보안 수정 누락 | 최소/CI/Docker를 Go 1.26.5로 고정하고 `govulncheck` 0건 확인 | 월별 dependency/툴체인 update PR 자동화 필요 |
| Nuxt 3.8/Vite 4 기반 2023 frontend lockfile | 현재 audit 79건(critical 7), 1.7MiB document chunk, deprecated API 경고 | 기존 API 계약과 production build는 현 Go API에서 유지·검증 | 별도 frontend 현대화 PR에서 Nuxt/Vite/Sentry/PDF stack과 code splitting 갱신 필요 |
| Plasmo 0.83 기반 2023 extension toolchain | production audit 93건(high 79), 오래된 Parcel/Sharp/Plasmo | 현재 extension build와 `/user`, `/document/webpage`, `/document/:id` 계약은 유지·검증 | 별도 extension PR에서 Plasmo 0.90+, 권한/고정 API URL/lockfile을 함께 개편 필요 |

## 의도적으로 하지 않은 것

- Kubernetes, Kafka, Redis, service mesh: 현재 한 OCI와 소규모 사용자에서 비용/운영 복잡도가 이익보다 큽니다.
- Lambda에서 PostgreSQL 직접 접근: browser burst와 DB 보안 경계를 다시 결합하므로 금지합니다.
- 모든 파일을 LLM에 그대로 전달: 비용, prompt injection, data egress를 줄이기 위해 deterministic extraction → bounded chunk → model 호출 순서를 지킵니다.
- UUID의 timestamp로 `created_at` 제거: migration 원본 시간과 business timezone을 표현하지 못합니다.
- API 계약 즉시 정리: frontend/extension이 쓰는 route/envelope/field는 adapter에서 유지하고 major API에서만 제거합니다.
