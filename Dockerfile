FROM scratch
EXPOSE 8000
COPY varnish-purge-proxy /
ENTRYPOINT ["/varnish-purge-proxy"]
