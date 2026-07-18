CREATE EXTENSION vector;

CREATE TYPE "Role" AS ENUM ('BASIC', 'PLUS', 'ADMIN');
CREATE TYPE "DocumentType" AS ENUM ('WEBPAGE', 'MEMO', 'IMAGE', 'FILE');
CREATE TYPE "Status" AS ENUM (
  'SCRAPE_PENDING', 'SCRAPE_PROCESSING', 'SCRAPE_REJECTED',
  'EMBED_PENDING', 'EMBED_PROCESSING', 'EMBED_REJECTED', 'COMPLETED'
);

CREATE TABLE users (
  id BIGINT PRIMARY KEY,
  uid TEXT NOT NULL UNIQUE,
  email TEXT NOT NULL UNIQUE,
  password TEXT,
  name TEXT,
  image_url TEXT,
  role "Role" NOT NULL,
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL
);

CREATE TABLE document (
  id BIGINT PRIMARY KEY,
  doc_id TEXT NOT NULL UNIQUE,
  title TEXT,
  type "DocumentType" NOT NULL,
  url TEXT,
  content TEXT,
  summary TEXT,
  status "Status" NOT NULL,
  thumbnail_url TEXT,
  file_size BIGINT,
  user_id BIGINT NOT NULL REFERENCES users(id),
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL
);

CREATE TABLE tag (
  id BIGINT PRIMARY KEY,
  name TEXT NOT NULL,
  user_id BIGINT NOT NULL REFERENCES users(id),
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL
);

CREATE TABLE collection (
  id BIGINT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT,
  user_id BIGINT NOT NULL REFERENCES users(id),
  created_at TIMESTAMP(3) NOT NULL,
  updated_at TIMESTAMP(3) NOT NULL
);

CREATE TABLE embedded_text (
  id BIGINT PRIMARY KEY,
  document_id BIGINT NOT NULL REFERENCES document(id),
  user_id BIGINT NOT NULL REFERENCES users(id),
  type "DocumentType",
  chapter TEXT,
  content TEXT,
  start_page_number INTEGER,
  start_line_number INTEGER,
  end_page_number INTEGER,
  end_line_number INTEGER,
  created_at TIMESTAMP(3) NOT NULL,
  vector vector(1536)
);

CREATE TABLE "_DocumentToTag" (
  "A" BIGINT NOT NULL REFERENCES document(id),
  "B" BIGINT NOT NULL REFERENCES tag(id),
  PRIMARY KEY ("A", "B")
);

CREATE TABLE "_CollectionToDocument" (
  "A" BIGINT NOT NULL REFERENCES collection(id),
  "B" BIGINT NOT NULL REFERENCES document(id),
  PRIMARY KEY ("A", "B")
);

CREATE TABLE email (
  id BIGINT PRIMARY KEY,
  email TEXT NOT NULL,
  signup_code TEXT NOT NULL,
  verified BOOLEAN NOT NULL
);

CREATE TABLE embedded_query (
  id BIGINT PRIMARY KEY,
  query TEXT NOT NULL,
  vector vector(1536) NOT NULL
);

INSERT INTO users VALUES
  (1, '550e8400-e29b-41d4-a716-446655440000', 'basic@example.com', '$2b$10$u1n5t0jKXqSWIgDgJ5BUgeYqPgI0bL4G/JEltjshO6UkfL0bUQnxm', 'Basic', NULL, 'BASIC', '2023-01-01', '2023-02-01'),
  (2, 'not-a-uuid', 'plus@example.com', 'legacy-plaintext', 'Plus', NULL, 'PLUS', '2023-03-01', '2023-04-01');

INSERT INTO document VALUES
  (10, '650e8400-e29b-41d4-a716-446655440000', '완료 메모', 'MEMO', NULL, '기존 메모 내용', '기존 요약', 'COMPLETED', NULL, NULL, 1, '2023-01-02', '2023-02-02'),
  (11, 'invalid-document-id', '재시도 웹', 'WEBPAGE', 'https://example.com', NULL, NULL, 'SCRAPE_PROCESSING', NULL, 12, 2, '2023-03-02', '2023-04-02'),
  (12, '750e8400-e29b-41d4-a716-446655440000', '기존 파일.pdf', 'FILE', NULL, NULL, NULL, 'COMPLETED', NULL, 1024, 1, '2023-01-03', '2023-02-03');

INSERT INTO tag VALUES
  (20, 'legacy-tag', 1, '2023-01-03', '2023-01-03'),
  (21, repeat('긴태그', 30), 2, '2023-03-03', '2023-03-03');

INSERT INTO collection VALUES
  (30, 'legacy-collection', '설명', 1, '2023-01-04', '2023-01-04');

INSERT INTO embedded_text VALUES
  (40, 10, 1, 'MEMO', '첫 절', '기존 메모 내용', 1, 1, 1, 1, '2023-02-02', array_fill(0.001::real, ARRAY[1536])::vector);

INSERT INTO "_DocumentToTag" VALUES (10, 20), (11, 21);
INSERT INTO "_CollectionToDocument" VALUES (30, 10);
INSERT INTO email VALUES (50, 'basic@example.com', '123456', true);
INSERT INTO embedded_query VALUES (60, '버려질 캐시', array_fill(0.001::real, ARRAY[1536])::vector);
