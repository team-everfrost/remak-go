# 테스트 전략

## 계층

1. Unit: UUIDv7, JWT audience, refresh/password, cursor, chunker, RRF, UTF-8/PDF/image artifact 추출, event envelope, JSON strict decode
2. Repository integration: 실제 PostgreSQL 18 + pgvector에서 transaction, ownership, stable cursor, HNSW/cosine 검색
3. AWS contract: MiniStack에서 bucket, SQS/DLQ/redrive, Go SDK endpoint, outbox/result consumer
4. Cross-repo E2E: Go API → outbox → SQS → packaged Node Lambda/Chromium → S3 → result SQS → Go worker → COMPLETED
5. Production smoke: 실제 AWS staging의 SQS event source partial batch와 IAM 권한만 소량 검증
6. Migration rehearsal: 별도 source/target PG18 DB에서 legacy fixture → FDW snapshot → import → count/UUID/password/vector/job/outbox invariant 검증
7. Supply chain: Go 1.26.5에서 `govulncheck ./...`, scraper는 production/dev `npm audit`, CloudFormation/OpenAPI/shell 정적 검증
8. Runtime package: scraper ZIP entrypoint import smoke test 후 공식 AWS Lambda Node24 x86_64 image에서 실제 Chromium → MiniStack S3/SQS → Go worker E2E

테스트는 고정 email을 재사용하지 않고 UUID를 붙입니다. cleanup은 DB pool close보다 먼저 실행되도록 `t.Cleanup`의 LIFO 순서를 지킵니다. integration 결과 cache를 숨기지 않으려면 `-count=1`을 사용합니다.

```sh
docker compose up -d --wait
go run ./cmd/migrate up
go test ./...
go test -race ./...
go test -count=1 -tags=integration ./...
./test/legacy-rehearsal.sh
npx --yes @redocly/cli@2.39.0 lint --config .redocly.yaml docs/openapi.yaml
cfn-lint infra/aws/core.yaml
```

Node scraper는 `make check`, `make package`로 검사합니다. 실제 Chromium은 macOS에서 Lambda용 x86 Linux binary를 직접 실행하지 않고 `public.ecr.aws/lambda/nodejs:24 --platform linux/amd64` 이미지에서 배포 산출물을 실행합니다.

## 실패 주입 필수 항목

- SQS publish 성공 후 DB mark 전 process kill
- 같은 result event 2회와 다른 eventId의 동일 terminal result
- document version 2가 생성된 뒤 version 1 결과 도착
- S3 artifact 없음/16MB 초과/잘못된 UTF-8
- AI 429/timeout/잘못된 dimension
- multipart 초과, 위장 MIME, quota 동시 업로드
- file soft delete 뒤 S3 delete 실패/재시도/중복 key, text-layer 없는 PDF의 명시적 reject
- SSRF loopback/private/metadata/redirect/DNS rebinding 방어
