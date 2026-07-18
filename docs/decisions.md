# 설계 결정 기록

## Go 모듈러 모놀리스

친구와 공부할 언어로 Go를 선택하되 OCI 한 대라는 비용 조건을 우선했습니다. API와 worker는 별도 process지만 같은 module/schema를 사용합니다. microservice별 DB, gRPC, service mesh는 사용하지 않습니다. 추후 트래픽이나 팀 소유권이 실제로 갈릴 때 event contract 경계를 기준으로 분리할 수 있습니다.

## 브라우저만 Node Lambda

Chromium 생태계와 Lambda의 burst/격리를 활용할 부분만 Node에 남겼습니다. Lambda의 DB 직접 접근, VPC attachment, status update, embedding queue 발행은 제거했습니다. 이로써 DB credential 유출 범위와 Go/Node 사이 shared-schema coupling을 없앴습니다.

## UUIDv7와 timestamp

새 PK는 application에서 UUIDv7로 생성합니다. 시간순 locality가 있어 random UUIDv4보다 B-tree insert가 낫고 분산 생성이 가능합니다. UUID timestamp는 운영상 식별 보조일 뿐이므로 `created_at`을 버리지 않습니다. business time, timezone, migration 원본 시간을 UUID에서 역산하지 않습니다. 레거시 account/document 공개 ID는 정상 UUID면 API 호환을 위해 보존하고, 깨진 ID만 PostgreSQL 18 `uuidv7()`로 다시 만들며 mapping table에 원본을 남깁니다.

## file/image 추출과 artifact lifecycle

텍스트 파일은 upload에서 UTF-8을 검증하고, PDF는 Go worker의 pure-Go parser가 페이지 표식을 포함한 텍스트를 만듭니다. 이미지는 설정된 multimodal AI provider가 시각 설명과 OCR을 함께 생성합니다. 이미지 전용 PDF처럼 text layer가 없는 PDF는 조용히 완료 처리하지 않고 명시적으로 `ENRICH_REJECTED`가 됩니다. 추후 OCR provider를 추가할 확장 지점입니다.

S3 upload와 DB transaction은 하나의 원자 transaction이 될 수 없으므로 DB 실패 시 API가 업로드를 보상 삭제합니다. 정상 문서 삭제는 DB soft delete와 cleanup job 생성을 한 transaction으로 묶고 worker가 S3를 재시도 삭제합니다. 사용자 요청 latency와 S3 일시 장애를 분리하면서 orphan artifact를 방치하지 않습니다.

## cursor는 created_at + id

기존 `updated_at` cursor는 문서 수정 시 page 사이를 이동해 중복/누락이 생깁니다. immutable `created_at DESC, id DESC`를 사용합니다. 기존 frontend가 보내는 `cursor=updatedAt&doc-id=`는 당분간 doc-id가 가리키는 immutable 위치로 해석하고, 신규 client는 opaque `X-Next-Cursor`/`page-token`을 사용합니다.

## PostgreSQL 18 + pgvector 0.8.5

현재 사용 쿼리와 Go pgvector client는 호환됩니다. owner/current-version 필터가 HNSW 후보를 버릴 때 결과가 모자라지 않도록 0.8의 query-local iterative scan을 사용하고, relaxed 결과는 strict distance 순으로 재정렬합니다. PG17 data directory를 PG18 image에 직접 연결하지 않습니다. major upgrade는 dump/restore 또는 `pg_upgrade`가 필요합니다. Docker PG18은 `/var/lib/postgresql`을 volume root로 사용합니다.

## MiniStack

로컬에서 필요한 AWS surface가 S3와 SQS/DLQ뿐이므로 LocalStack 대신 MiniStack 1.4.2를 pin했습니다. endpoint `:4566`과 AWS SDK 설정은 유지됩니다. init script는 endpoint/region을 명시해 image 내부 AWS CLI 차이를 피하고, readiness는 init 실패 0건까지 검사합니다. 실제 AWS parity의 최종 보증은 staging contract test가 담당합니다.

## API 호환 adapter

core model은 깨끗하게 유지하고 기존 Nuxt/extension 계약은 HTTP adapter에서 맞춥니다. 성공 envelope `{message:"success",data:...}`, `docId`, BASIC/PLUS/ADMIN compatibility role, 기존 route, chat SSE event 이름을 유지합니다. 다음 major API에서만 제거합니다.
