varnish-purge-proxy
===================

Proxy purge requests to multiple varnish servers

Specify tags to limit instances that receive the purge request, multiple tags can be used. You must specify at least one tag.

`./varnish-purge-proxy Service:varnish Environment:live`

You can also specify listen port:

`./varnish-purge-proxy --port=8000`

You can also specify the destination port to target:

`./varnish-purge-proxy --destport=6081 

varnish-purge-proxy will cache the IP lookup for 60 seconds, you can change this as follows:

`./varnish-purge-proxy --cache=120`

Authentication
--------------

AWS access key and secret key can be added as environment variables, using either `AWS_ACCESS_KEY_ID` or `AWS_ACCESS_KEY` and `AWS_SECRET_ACCESS_KEY` or `AWS_SECRET_KEY`.  If these are not available then IAM credentials for the instance will be checked.

Building
--------

Build a binary by running:

`go build varnish-purge-proxy.go`


Build adocker image container by running:

`./make-container-image.sh [name of image]`

Running in Docker
----------------

`docker build -t varnish-proxy .`
`docker run -e AWS_ACCESS_KEY=key-id -e AWS_ACCESS_SECRET=secret -p 8000:8000 varnish-proxy Service:varnish Environment:test`
