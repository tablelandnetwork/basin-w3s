FROM golang:1.21.5-alpine as builder

RUN apk --no-cache add make build-base git

RUN mkdir /app
WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download -x
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
  BIN_BUILD_FLAGS="GOOS=linux" make build

FROM alpine

COPY --from=builder /app/api /app/api
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["./api"]