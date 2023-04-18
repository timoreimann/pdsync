FROM amd64/golang:1.20.3-alpine3.17 as builder

WORKDIR /usr/src/pdsync

COPY . .
RUN CGO_ENABLED=0 go build -mod vendor -o /pdsync

FROM gcr.io/distroless/static-debian11 as runner
COPY --from=builder /pdsync /
ENTRYPOINT ["/pdsync"]
