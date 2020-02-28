FROM golang:alpine as cert-store
RUN apk --no-cache add ca-certificates

FROM scratch

COPY --from=cert-store /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY ./pdsync /
ENTRYPOINT ["/pdsync"]
