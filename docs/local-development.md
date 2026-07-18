# 로컬 개발 환경

## 필요한 것

- Go 1.26.5
- OrbStack 또는 Docker Desktop + Compose v2
- Make, Git, curl, jq
- 선택: `psql`(legacy import), AWS CLI(직접 MiniStack 확인)

Node/Chromium scraper까지 실제로 돌릴 때는 Node 24와 `scraping-lambda`가 추가로 필요합니다. PostgreSQL, pgvector, S3, SQS는 호스트에 직접 설치하지 않습니다.

## 시작

```sh
cd ~/Workspace/remak-go
cp .env.example .env
docker compose up -d --wait
set -a; source .env; set +a
go run ./cmd/migrate up
```

터미널 1:

```sh
set -a; source .env; set +a
go run ./cmd/api
```

터미널 2:

```sh
set -a; source .env; set +a
go run ./cmd/worker
```

확인:

```sh
curl -fsS http://localhost:8080/health/live
curl -fsS http://localhost:8080/health/ready
docker compose ps
```

개발 환경은 `EXPOSE_DEBUG_VERIFICATION_CODES=true`라 OTP 요청 응답의 `debugCode`로 가입할 수 있습니다. production에서는 config validation이 이 옵션을 거부하며 SES가 실제 코드를 발송합니다. `AI_BASE_URL`이 비어 있으면 deterministic local hash/summary를 써서 비용 없이 검색 흐름을 테스트합니다. 이 local provider는 image vision을 가장하지 않으므로 image enrichment는 명시적으로 reject됩니다.

## MiniStack

MiniStack 1.4.2가 `localhost:4566`에서 S3/SQS를 제공합니다. `infra/ministack/init.sh`가 다음 resource를 멱등 생성합니다.

- `remak-documents`, `remak-artifacts`
- `remak-scrape-request`, `remak-scrape-request-dlq`
- `remak-scrape-result`, `remak-scrape-result-dlq`

AWS SDK endpoint만 바꾸므로 application code는 실제 AWS와 같습니다. 다만 IAM, Lambda event source, AWS 고유 timeout/redrive semantics는 staging smoke test가 최종 보증합니다.

## scraper 포함 E2E

`scraping-lambda`에서 먼저 검사/package합니다.

```sh
cd ~/Workspace/scraping-lambda
npm ci
make check
make package
```

macOS에서는 Lambda용 Linux Chromium binary를 직접 실행하지 않습니다. 저장소 README의 명령처럼 `public.ecr.aws/lambda/nodejs:24 --platform linux/amd64` container에서 packaged handler를 실행합니다. 흐름은 Go API → DB outbox → MiniStack request queue → packaged Node/Chromium → MiniStack S3/result queue → Go worker → COMPLETED입니다.

## 자주 겪는 문제

- port 5432/4566/8080 충돌: 기존 process/container를 확인하고 무작정 volume을 지우지 않습니다.
- PG17 volume: PG18 image에 직접 연결하지 않습니다. 현재 volume root는 `/var/lib/postgresql`입니다.
- AWS credential error: `.env`의 개발용 `AWS_ACCESS_KEY_ID=test`, `AWS_SECRET_ACCESS_KEY=test`, endpoint가 shell에 export됐는지 확인합니다.
- image가 reject: local hash provider는 vision이 없습니다. mock server 또는 실제 multimodal-compatible AI를 설정합니다.
- scanned PDF가 reject: text layer가 없는 PDF는 현재 OCR 대상임을 명시하는 정상 실패입니다. text PDF는 Go worker에서 처리됩니다.
- queue poison message: DLQ/redrive를 확인하고 payload를 삭제하기 전에 원본을 저장합니다.

## 종료

```sh
docker compose down
```

이 명령은 container/network만 내리고 named volume은 보존합니다. `docker compose down -v`는 DB/S3 local data를 지우므로 명시적으로 초기화하려는 경우가 아니면 실행하지 않습니다.
