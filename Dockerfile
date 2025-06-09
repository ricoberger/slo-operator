FROM golang:1.24.2 AS builder
WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/controller/ internal/controller/

RUN CGO_ENABLED=0 go build -a -o manager cmd/main.go

FROM alpine:3.21.3
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532
ENTRYPOINT ["/manager"]
