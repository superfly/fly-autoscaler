FROM golang:1.22 AS builder
ARG FAS_VERSION=
ARG FAS_COMMIT=
COPY . .
RUN go build -ldflags "-s -w -X 'main.Version=${FAS_VERSION}' -X 'main.Commit=${FAS_COMMIT}' -extldflags '-static'" -tags osusergo,netgo -o /usr/local/bin/fly-autoscaler ./cmd/fly-autoscaler

FROM alpine
COPY --from=builder /usr/local/bin/fly-autoscaler /usr/local/bin/fly-autoscaler
ENTRYPOINT fly-autoscaler
CMD ["serve"]
