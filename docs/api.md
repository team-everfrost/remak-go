# 기능과 API 호환성

모든 일반 JSON 성공 응답은 `{ "message": "success", "data": ... }`입니다. 오류는 `{ "error": { "code", "message" } }`입니다. access token은 `Authorization: Bearer`로 전달합니다.

기계가 읽는 전체 route/request/response 명세는 [`openapi.yaml`](openapi.yaml)에 있습니다.

## 기능 목록

- 인증: signup/reset/withdraw OTP, local signup/login/logout, refresh token rotation
- 계정: profile 조회/수정, storage usage/limit; client가 보낸 role/plan은 무시
- 문서: memo/webpage/file 생성, 수정, 조회, soft delete+비동기 S3 정리, stable cursor list
- 분류: AI 분석이 만드는 3~5개 tag의 교체/목록/검색, collection CRUD와 문서 추가/제거
- 검색: text와 lexical+vector RRF hybrid
- RAG: `/chat/rag`가 `documents`, `chat` SSE event를 HTTP 201로 전송
- 비동기 수집: versioned scrape request/result, outbox/inbox, enrichment

## 기존 client 호환 route

`remak-frontend`가 쓰는 `/auth/*`, `/user`, `/document`, `/document/memo`, `/document/webpage`, `/document/file`, `/document/search/{text,hybrid,tag,collection}`, `/tag`, `/collection`, `/chat/rag`를 유지합니다. Chrome extension의 `GET /user`, `POST /document/webpage`, `DELETE /document/:id`와 success message도 유지합니다.

목록은 기존 `doc-id`, `docid`, `cursor`를 받아들이지만 정렬 위치는 doc-id의 `(created_at,id)`로 결정합니다. 신규 호출은 응답 `X-Next-Cursor`를 다음 요청의 `page-token`으로 보내는 방식이 권장됩니다.

## 의도적인 차이

- profile PATCH의 `role`로 ADMIN 승격할 수 없습니다.
- unknown JSON field는 400입니다. 단, compatibility DTO가 명시한 과거 field는 받되 무시할 수 있습니다.
- webpage body의 과거 `content`는 wire compatibility용이며 browser result 전에는 신뢰하거나 저장하지 않습니다.
- file URL은 공개 object URL이 아니라 10분짜리 presigned URL입니다.
- file upload 직후 상태는 `ENRICH_PENDING`입니다. UTF-8 text와 text-layer PDF는 Go worker가 추출하고, image는 production multimodal AI가 설명/OCR한 뒤 `COMPLETED`가 됩니다. text layer가 없는 PDF는 OCR provider가 추가되기 전까지 `ENRICH_REJECTED`로 명확히 표시합니다.
