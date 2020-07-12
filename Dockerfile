FROM golang:1.14-alpine as builder
RUN apk update && apk add --no-cache ca-certificates && update-ca-certificates

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY markdown2confluence /markdown2confluence
ENTRYPOINT ["/markdown2confluence"]