FROM golang:1.14-alpine as builder
RUN apk update && apk add --no-cache ca-certificates && update-ca-certificates

FROM alpine:3.12
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY markdown2confluence /bin/markdown2confluence
CMD ["/bin/markdown2confluence"]
