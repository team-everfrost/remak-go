# syntax=docker/dockerfile:1.7
FROM golang:1.27.0-alpine AS build
WORKDIR /src
ARG COMMAND=api
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/remak ./cmd/${COMMAND} && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/healthcheck ./cmd/healthcheck

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/remak /app/remak
COPY --from=build /out/healthcheck /app/healthcheck
USER nonroot:nonroot
ENTRYPOINT ["/app/remak"]
