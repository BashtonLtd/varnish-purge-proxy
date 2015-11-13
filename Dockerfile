FROM alpine:3.2
RUN apk --update add ca-certificates curl
EXPOSE 8000
COPY varnish-purge-proxy /usr/bin/
ENTRYPOINT ["/usr/bin/varnish-purge-proxy"]
