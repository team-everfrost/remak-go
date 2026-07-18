# 비동기 이벤트 계약

Go와 Node 사이의 유일한 runtime 계약은 SQS JSON event와 S3 object입니다. Lambda는 PostgreSQL schema나 account를 알지 못합니다. 모든 event는 `schemaVersion`, `eventId`, `eventType`, `traceId`, `occurredAt`, `data` envelope를 사용합니다.

## scrape request v1

event type: `document.scrape.requested.v1`

```json
{
  "schemaVersion": "1.0",
  "eventId": "019f...",
  "eventType": "document.scrape.requested.v1",
  "traceId": "http-request-id",
  "occurredAt": "2026-07-18T12:00:00Z",
  "data": {
    "jobId": "019f...",
    "documentId": "019f...",
    "documentVersion": 1,
    "url": "https://example.com/article"
  }
}
```

API transaction이 document/version/SCRAPE job/outbox를 함께 commit합니다. dispatcher는 outbox row를 lease한 뒤 request queue에 envelope 전체를 그대로 보냅니다. SQS 전송 성공 뒤 DB mark 전에 죽어도 다시 전송될 수 있으므로 consumer는 at-least-once를 전제로 합니다.

## scrape completed v1

event type: `document.scrape.completed.v1`

```json
{
  "schemaVersion": "1.0",
  "eventId": "request와 같은 eventId",
  "eventType": "document.scrape.completed.v1",
  "traceId": "http-request-id",
  "occurredAt": "2026-07-18T12:00:04Z",
  "data": {
    "jobId": "019f...",
    "documentId": "019f...",
    "documentVersion": 1,
    "title": "문서 제목",
    "content": "짧은 payload에서는 선택적으로 포함",
    "rawArtifactKey": "scrapes/{documentId}/{version}/{jobId}/raw.html",
    "contentArtifactKey": "scrapes/{documentId}/{version}/{jobId}/content.txt",
    "contentHash": "sha256 hex",
    "thumbnailUrl": "",
    "extractionMethod": "chromium-inner-text-v1",
    "fileSize": 12345
  }
}
```

raw HTML과 rendered innerText는 먼저 deterministic S3 key에 기록합니다. 결과 payload의 content가 비어 있으면 Go worker가 `contentArtifactKey`에서 읽습니다. worker는 artifact size와 UTF-8을 다시 검증합니다.

## scrape failed v1

event type: `document.scrape.failed.v1`

```json
{
  "schemaVersion": "1.0",
  "eventId": "request와 같은 eventId",
  "eventType": "document.scrape.failed.v1",
  "traceId": "http-request-id",
  "occurredAt": "2026-07-18T12:00:10Z",
  "data": {
    "jobId": "019f...",
    "documentId": "019f...",
    "documentVersion": 1,
    "title": "",
    "rawArtifactKey": "",
    "contentArtifactKey": "",
    "contentHash": "",
    "extractionMethod": "chromium-inner-text-v1",
    "fileSize": 0,
    "errorCode": "scrape_failed",
    "errorMessage": "redacted bounded message"
  }
}
```

Lambda는 중간 실패에서 `batchItemFailures`로 request message를 재전달하고, `ApproximateReceiveCount == MAX_SCRAPE_ATTEMPTS`인 마지막 실패에서만 failed event를 발행합니다. failed event 발행이 실패하면 request message도 성공 처리하지 않습니다.

## Go result consumer invariant

처리 순서는 다음과 같습니다.

1. strict JSON/event version/type/UUID/size 검증
2. `inbox_events(eventId)` 등록; 이미 완료한 event는 message delete
3. event의 job/document/version과 DB job을 대조
4. 현재 version보다 오래된 결과인지 확인
5. 성공이면 version artifact/content를 기록하고 ENRICH job 생성, 실패면 SCRAPE_REJECTED
6. inbox 완료를 같은 DB transaction에서 기록
7. transaction commit 뒤에만 SQS message delete

같은 eventId 중복은 inbox가 막고, 다른 eventId로 같은 terminal job 결과가 와도 job state/version 조건이 no-op 처리합니다. version 2가 만들어진 뒤 도착한 version 1 결과는 현재 document를 덮어쓰지 않습니다.

## schema 변경 규칙

- 기존 field의 의미/타입을 바꾸지 않습니다.
- optional field 추가는 같은 v1에서 가능하지만 Node/Go strict parser 양쪽 test를 먼저 갱신합니다.
- required field 삭제, rename, semantic 변경은 새 `eventType ...v2`와 새 data struct로 만듭니다.
- producer는 consumer 배포가 끝난 뒤 새 version 발행을 시작합니다.
- unknown event type은 outbox/queue에서 무한 retry하지 않고 명시적 dead-letter 대상으로 처리합니다.

