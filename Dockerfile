FROM amd64/golang:1.23.1-alpine3.20 as builder

WORKDIR /usr/src/pdsync

COPY . .
RUN CGO_ENABLED=0 go build -mod vendor -o /pdsync

FROM gcr.io/distroless/static-debian11 as runner
COPY --from=builder /pdsync /
ENTRYPOINT ["/pdsync"]
