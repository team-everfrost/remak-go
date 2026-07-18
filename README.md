# remak-go

Remak의 계정, 문서 수집 상태, 검색, RAG를 담당하는 Go 모듈러 모놀리스입니다. 브라우저 렌더링만 기존 `scraping-lambda`의 Node.js Lambda에 남기고, 데이터의 최종 소유권과 상태 전이는 이 서비스가 가집니다.

## 빠른 시작

필수 도구는 Go 1.26.5, OrbStack/Docker, Make입니다.

```sh
cp .env.example .env
docker compose up -d --wait
set -a; source .env; set +a
go run ./cmd/migrate up
go run ./cmd/api
```

다른 터미널에서 같은 환경 변수를 불러온 뒤 worker를 실행합니다.

```sh
set -a; source .env; set +a
go run ./cmd/worker
```

로컬 의존성은 PostgreSQL 18.4 + pgvector 0.8.5와 MiniStack S3/SQS입니다. API는 `http://localhost:8080`, MiniStack은 `http://localhost:4566`입니다.

## 검사

```sh
go test ./...
go test -race ./...
go test -count=1 -tags=integration ./...
```

## 문서

- [시스템 아키텍처](docs/architecture.md)
- [데이터 모델과 상태 전이](docs/data-model.md)
- [2023 결함과 개선 결과](docs/modernization.md)
- [비동기 이벤트 계약](docs/events.md)
- [로컬 개발 환경](docs/local-development.md)
- [기능과 API 호환성](docs/api.md)
- [OpenAPI 3.1 명세](docs/openapi.yaml)
- [레거시 마이그레이션](docs/migration.md)
- [테스트 전략](docs/testing.md)
- [설계 결정](docs/decisions.md)
- [운영과 배포](docs/operations.md)

운영 배포와 레거시 이관 전에는 `.env.example`의 개발용 secret/credential을 그대로 쓰지 말고 `docs/migration.md`의 backup, rehearsal, cutover 순서를 따릅니다.
