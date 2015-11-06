FROM kiasaki/alpine-golang

WORKDIR /gopath/src/app
ADD varnish-purge-proxy.go /gopath/src/app/varnish-purge-proxy.go
RUN go get app

CMD []
ENTRYPOINT ["/gopath/bin/app"]
