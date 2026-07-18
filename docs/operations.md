# 운영과 배포 runbook

현재 비용 조건에서는 OCI 인스턴스 한 대에 Caddy, Go API, Go worker, PostgreSQL을 Docker Compose로 띄우고 AWS에는 S3, SQS/DLQ, SES, scraping Lambda만 둡니다. API와 worker는 같은 image/code/schema를 쓰지만 process와 장애 범위는 분리됩니다.

## OCI 호스트 준비

권장 최소 구성은 ARM/AMD64 Linux, Docker Engine + Compose plugin, 4GB RAM입니다. Chromium은 Lambda에서 실행하므로 OCI에 브라우저를 설치하지 않습니다. 외부에는 80/443만 열고 PostgreSQL 5432는 compose internal network에만 둡니다. SSH는 허용 IP를 제한합니다.

```sh
git clone https://github.com/team-everfrost/remak-go.git
cd remak-go/deploy
cp .env.example .env
chmod 600 .env
```

`.env`에서 다음 값은 반드시 바꿉니다.

- `REMAK_DOMAIN`, `ALLOWED_ORIGINS`, Chrome extension origin
- `POSTGRES_PASSWORD`, 이에 맞춘 `DATABASE_URL`
- 충분히 긴 서로 다른 `JWT_SECRET`, `CHALLENGE_SECRET`
- scoped IAM access key, 두 SQS URL, 두 S3 bucket, SES `EMAIL_FROM`
- production AI base URL/key/model. image 이해를 지원하는 chat model이어야 image upload가 완료됨

secret 생성 예:

```sh
openssl rand -base64 48
```

## AWS core resource

`infra/aws/core.yaml`은 public access가 차단되고 SSE/versioning이 켜진 document/artifact/code bucket, request/result queue와 각 DLQ, OCI용 managed IAM policy를 만듭니다. bucket 삭제 정책은 `Retain`이라 stack 삭제가 사용자 파일을 지우지 않습니다.

```sh
aws cloudformation deploy \
  --stack-name remak-core \
  --template-file ../infra/aws/core.yaml \
  --capabilities CAPABILITY_NAMED_IAM \
  --parameter-overrides \
    ArtifactBucketName=your-remak-artifacts \
    DocumentBucketName=your-remak-documents \
    LambdaCodeBucketName=your-remak-lambda-code \
    SESIdentityArn=arn:aws:ses:ap-northeast-2:ACCOUNT:identity/example.com

aws cloudformation describe-stacks \
  --stack-name remak-core \
  --query 'Stacks[0].Outputs'
```

출력된 `OCIServicePolicyArn`을 OCI에서 사용할 별도 IAM user에 붙이고 access key를 한 번만 발급합니다. root key를 쓰지 않습니다. `.env`를 저장소에 commit하지 않고 파일 권한을 600으로 유지합니다. scraper Lambda는 별도 execution role을 사용하므로 OCI key를 공유하지 않습니다.

그 다음 `scraping-lambda`에서 package ZIP을 code bucket에 올리고 그 저장소의 `infra/template.yaml`을 배포합니다. `MaxScrapeAttempts`와 request queue `maxReceiveCount`는 둘 다 3으로 유지합니다.

## 첫 배포와 갱신

```sh
cd deploy
docker compose config >/dev/null
docker compose build
docker compose up -d
docker compose ps
curl -fsS https://$REMAK_DOMAIN/health/live
curl -fsS https://$REMAK_DOMAIN/health/ready
```

`migrate` service가 성공해야 API/worker가 시작됩니다. API healthcheck가 ready가 된 뒤 Caddy가 연결됩니다. application container는 non-root, read-only filesystem, `no-new-privileges`로 실행됩니다.

일반 갱신은 migration이 backward-compatible한지 확인한 후 다음처럼 수행합니다.

```sh
git pull --ff-only
docker compose build api worker migrate
docker compose up -d --remove-orphans
docker compose ps
```

현재 한 호스트 구성에서는 migration과 process 교체 사이에 짧은 재시작 구간이 있습니다. 무중단이 실제 요구가 될 때에만 API replica/외부 DB/load balancer를 추가합니다.

## 관찰할 지표

구조화 JSON log를 기본 출력합니다. 최소 alert 조건은 다음입니다.

- API `/health/ready` 연속 실패, container restart 증가
- PostgreSQL disk 70/85/95%, connection 수, slow query
- `outbox_events` unpublished oldest age, `dead_lettered_at` 증가
- `ingestion_jobs` type/state/oldest `available_at`, attempt 5 도달
- `artifact_cleanup_jobs` FAILED 또는 attempt 10 도달
- SQS ApproximateAgeOfOldestMessage, DLQ visible message > 0
- Lambda errors/throttles/duration, reserved concurrency 2 포화
- AI 429/timeout, enrichment reject 증가
- S3 size/object count와 계정 storage 합계의 비정상 괴리

빠른 DB 점검:

```sql
SELECT count(*), min(created_at) FROM outbox_events WHERE published_at IS NULL AND dead_lettered_at IS NULL;
SELECT type, state, count(*), min(available_at) FROM ingestion_jobs GROUP BY type, state;
SELECT state, count(*), max(attempt_count) FROM artifact_cleanup_jobs GROUP BY state;
SELECT status, count(*) FROM documents WHERE deleted_at IS NULL GROUP BY status;
```

## backup과 restore rehearsal

DB와 S3는 서로 다른 장애 도메인에 backup해야 합니다. `deploy/backup.sh`는 PostgreSQL custom-format dump를 만들고 archive listing으로 무결성을 검사합니다. `BACKUP_S3_URI`가 있으면 별도 backup bucket으로 복사합니다. application document bucket과 같은 bucket을 backup destination으로 쓰지 않습니다.

```sh
cd deploy
BACKUP_DIR=/var/backups/remak \
BACKUP_S3_URI=s3://your-remak-backups/postgres \
./backup.sh
```

cron/systemd timer로 매일 실행하고 AWS bucket lifecycle/retention을 별도로 정합니다. backup 성공 로그만 믿지 말고 매월 임시 DB에 restore합니다.

```sh
docker compose exec -T postgres createdb -U remak remak_restore_test
docker compose exec -T postgres pg_restore -U remak -d remak_restore_test --clean --if-exists < /var/backups/remak/FILE.dump
docker compose exec -T postgres psql -U remak -d remak_restore_test -c 'SELECT count(*) FROM documents;'
docker compose exec -T postgres dropdb -U remak remak_restore_test
```

명령의 `remak_restore_test`가 운영 DB 이름과 다른지 반드시 확인합니다.

## 장애별 대응

- AWS/SQS 일시 장애: API의 memo/file DB write는 가능하지만 webpage outbox가 쌓입니다. worker가 복구 후 발행합니다.
- AI 장애: search는 lexical 결과로 degrade합니다. enrichment job은 backoff 후 최대 5회 재시도하며 문서는 reject 상태로 보입니다.
- scraper 장애: request message는 최대 3회 재시도되고 마지막 실패 결과가 문서를 `SCRAPE_REJECTED`로 종결합니다. DLQ가 있으면 수동 원인 확인 후 redrive합니다.
- S3 delete 장애: 사용자는 이미 soft delete 결과를 받고 cleanup job이 최대 10회 재시도합니다. FAILED가 남으면 권한/bucket/key를 고친 뒤 job의 `attempt_count`, `available_at`, `state`를 승인된 운영 절차로 재queue합니다.
- maintenance 장애: worker는 시작 시와 매시간 보존기간 정리를 실행합니다. 개별 삭제 문서는 30일, 즉시 익명화된 탈퇴 계정은 늦은 queue 결과를 위해 7일 후 hard delete합니다. 실패해도 API path는 멈추지 않고 다음 주기에 재시도하므로 log alert 후 DB lock/권한 원인을 확인합니다.
- PostgreSQL 장애: API ready가 실패하고 Caddy가 unhealthy API에 연결하지 않습니다. volume을 임의로 초기화하지 말고 먼저 disk, log, backup을 확인합니다.

## 비용 방어선

- Lambda reserved concurrency 기본 2, memory 2048MB, timeout 180초
- API upload: 파일당 10MiB, 요청당 10개/기본 100MiB, 계정 quota FREE 1GiB/PLUS 10GiB
- SQS long polling 20초, MiniStack은 로컬에서만 사용
- AI 입력은 summary 12k rune, RAG context 24k rune로 제한
- S3 noncurrent version은 core template에서 30일 뒤 만료, Lambda package는 90일 뒤 만료
