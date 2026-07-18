# 레거시 데이터 마이그레이션

이 절차는 기존 NestJS/Prisma PostgreSQL을 읽기 전용 소스로 연결하고 새 `remak-go` PostgreSQL 18 데이터베이스로 복사합니다. 구 데이터베이스는 수정하지 않습니다. 새 데이터베이스가 비어 있지 않으면 import가 즉시 중단되며, import 본체는 한 transaction이라 중간 실패 시 target 변경도 전부 rollback됩니다.

## 무엇을 보존하고 무엇을 버리는가

| 레거시 데이터 | 처리 |
|---|---|
| `users.uid`, `document.doc_id`가 정상 UUID | 공개 API 호환을 위해 그대로 보존 |
| 깨진/비 UUID 공개 ID | UUIDv7 재발급, `legacy_migration.*_id_map`에 원본과 mapping 보존 |
| bcrypt 비밀번호 (`$2a$`, `$2b$`, `$2y$`) | hash 그대로 보존 |
| 다른 형식/평문 비밀번호 | 가져오지 않고 password reset 필요 |
| BASIC / PLUS / ADMIN | USER+FREE / USER+PLUS / ADMIN+FREE로 변환 |
| 문서 원본 시간 | 구 Prisma가 UTC로 기록했다는 전제 아래 `created_at`, `updated_at` 보존 |
| 완료 문서, summary, 기존 vector(1536) | version 1과 `legacy-embedding-1536` chunk로 보존 |
| 처리 중/실패 문서 | pending으로 되돌리고 새 job 생성; 웹은 outbox도 생성해 재수집 |
| file/image S3 key | 기존 `doc_id` key를 `raw_artifact_key`로 그대로 참조 |
| tag, collection, 관계 | 새 UUID로 변환하고 동일 owner 관계만 보존 |
| 평문 OTP `email` table | 폐기. 새 HMAC challenge 체계와 호환되지 않음 |
| `embedded_query` 검색어 cache | 폐기. 개인별 hybrid retrieval로 대체 |
| refresh/session | 폐기. 전 사용자 재로그인 |

tag/collection은 기존 HTTP 계약이 이름 기반이므로 내부 numeric ID를 보존하지 않습니다. owner가 다른 문서에 잘못 연결된 관계는 보안상 복사하지 않고 검증 결과에 개수를 표시합니다.

## 사전 조건

- source와 target의 일관된 backup을 먼저 만듭니다: DB dump, document bucket, 필요하면 webpage artifact bucket.
- target은 PostgreSQL 18이고 `vector`, `pgcrypto`, `postgres_fdw` extension을 만들 수 있어야 합니다.
- target schema migration을 먼저 최신까지 적용합니다.
- import 중에는 새 API/worker를 띄우지 않습니다. 특히 생성되는 scrape outbox가 S3/Lambda 준비 전에 발행되면 안 됩니다.
- source DB는 target PostgreSQL host에서 접속 가능해야 합니다. source 계정에는 `SELECT`만 부여합니다.
- 구 Prisma `DateTime`이 UTC로 저장되었는지 확인합니다. UTC가 아니라면 `import.sql`의 `AT TIME ZONE 'UTC'`를 실제 zone으로 바꾼 뒤 rehearsal 합니다.

## 1. 백업과 target 준비

```sh
pg_dump --format=custom --no-owner --file remak-legacy-$(date +%F).dump "$LEGACY_DATABASE_URL"

export DATABASE_URL='postgres://remak:...@new-postgres:5432/remak?sslmode=require'
go run ./cmd/migrate up
```

운영 PG17 volume을 PG18 container에 직접 연결하면 안 됩니다. major version 변경은 `pg_dump`/restore 또는 `pg_upgrade`가 필요합니다. 이 프로젝트의 데이터 변환은 구 DB와 새 DB를 동시에 유지하는 방식이므로, dump는 rollback 안전망이고 실제 변환은 FDW source에서 수행합니다.

## 2. 읽기 전용 source 계정

구 DB에서 전용 계정을 만들 수 있다면 다음처럼 권한을 제한합니다.

```sql
CREATE ROLE remak_migrator LOGIN PASSWORD 'replace-me';
GRANT CONNECT ON DATABASE remak TO remak_migrator;
GRANT USAGE ON SCHEMA public TO remak_migrator;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO remak_migrator;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO remak_migrator;
```

## 3. rehearsal/import 실행

`run.sh`가 환경 변수를 검사한 뒤 `psql`을 실행합니다. source password는 command line argument가 아니라 `psql \getenv`로 읽고, 완료 후 foreign server와 user mapping을 삭제합니다.

```sh
export DATABASE_URL='postgres://remak:...@new-postgres:5432/remak?sslmode=require'
export LEGACY_DB_HOST='old-postgres.internal'
export LEGACY_DB_PORT='5432'
export LEGACY_DB_NAME='remak'
export LEGACY_DB_USER='remak_migrator'
export LEGACY_DB_PASSWORD='...'
export LEGACY_DB_SSLMODE='require'

./db/legacy/run.sh | tee legacy-import.log
```

스크립트는 다음 순서로 동작합니다.

1. `postgres_fdw` foreign table을 생성합니다.
2. 한 repeatable-read remote transaction에서 source를 `legacy_stage`에 materialize합니다. 이 단계가 import 도중 source 변경과 Prisma enum pushdown 문제를 막습니다.
3. target이 비어 있는지, 이메일 대소문자 중복과 알 수 없는 enum이 없는지 검사합니다.
4. mapping, account, document/version, taxonomy, chunk, job, outbox를 한 serializable transaction으로 기록합니다.
5. 필수 count, version, job/status invariant를 검사한 뒤에만 commit합니다.
6. 상세 검증표를 출력하고 source 연결 정보와 stage snapshot을 제거합니다.

## 4. 검증 기준

로그에서 최소한 다음을 확인합니다.

- accounts/documents/tags/collections의 source/target count가 동일함
- `documents_without_versions = 0`
- `invalid_current_chunks = 0`
- 강제 password reset 수가 예상과 일치함
- 재발급 account/document UUID 수가 예상과 일치함
- cross-owner tag/collection link 수가 0이거나, 0이 아니라면 실제 오염 데이터로 승인됨
- `SCRAPE_* → SCRAPE_PENDING`, `EMBED_* → ENRICH_PENDING`, `COMPLETED → COMPLETED` 상태표가 예상과 일치함
- scrape pending 수와 `SCRAPE/QUEUED` job 및 unpublished outbox 수가 일치함

추가 sampling도 수행합니다.

```sql
SELECT email, role, plan, created_at FROM accounts ORDER BY created_at LIMIT 20;
SELECT id, type, status, current_version, title FROM documents ORDER BY created_at LIMIT 50;
SELECT document_id, version, raw_artifact_key, extraction_method FROM document_versions WHERE raw_artifact_key IS NOT NULL LIMIT 50;
SELECT embedding_model, count(*), count(embedding) FROM chunks GROUP BY embedding_model;
SELECT id, legacy_public_id, new_id FROM legacy_migration.document_id_map WHERE generated_new_id;
```

## 5. S3 전환

같은 document bucket을 계속 사용하면 file/image의 기존 key(`doc_id`)는 복사하지 않아도 됩니다. 새 bucket으로 바꿀 때는 DB 전환 전에 전체 sync하고, sample object의 size/checksum/metadata를 대조합니다.

```sh
aws s3 sync s3://old-remak-documents s3://new-remak-documents --only-show-errors
```

새 업로드는 `accounts/{accountId}/documents/{documentId}/original` key를 사용합니다. 레거시 key는 다운로드와 Go worker 추출에서 계속 지원되며, 문서를 삭제하면 cleanup job이 어느 형태의 key든 제거합니다. 즉 전환 당일 모든 key를 새 prefix로 rename할 필요는 없습니다.

## 6. cutover와 rollback

1. 새 worker를 켜기 전에 SQS URL, S3 bucket, scraper Lambda event source, AI provider를 확인합니다.
2. worker를 켜고 pending/rejected/job/outbox/DLQ를 관찰합니다.
3. 내부 계정으로 login, list, download, search, RAG, webpage scrape를 smoke test합니다.
4. API DNS를 새 OCI/Caddy로 전환합니다.
5. 구 DB와 구 S3는 acceptance 기간 동안 read-only로 보존합니다.

문제가 생기면 새 API/worker를 내리고 DNS를 구 backend로 되돌립니다. source DB는 import에서 수정하지 않았기 때문에 DB reverse migration이 필요 없습니다. cutover 이후 새 서비스에서 만들어진 데이터는 구 schema에 자동 병합되지 않으므로, rollback window 중에는 write freeze 또는 별도 export 정책을 정해야 합니다.

acceptance가 끝난 뒤에만 audit mapping을 지웁니다.

```sh
psql "$DATABASE_URL" --file db/legacy/cleanup.sql
```

## 자동 rehearsal fixture

`test/fixtures/legacy.sql`은 정상 UUID, 깨진 UUID, bcrypt/비호환 비밀번호, 완료 memo/file, processing webpage, tag/collection, vector를 포함합니다. 임시 source/target DB에 fixture와 최신 migration을 적용한 뒤 `db/legacy/run.sql`을 실행하면 운영 전 전체 경로를 반복 검증할 수 있습니다. 테스트 DB 이름은 반드시 명시적으로 정하고 운영 DB와 다른지 확인한 뒤 생성/삭제합니다.

