varnish-purge-proxy
===================

[![Build Status](https://travis-ci.org/BashtonLtd/varnish-purge-proxy.svg?branch=master)](https://travis-ci.org/BashtonLtd/varnish-purge-proxy)

Proxy purge requests to multiple varnish servers

Specify tags to limit instances that receive the purge request, multiple tags can be used. You must specify at least one tag.

`./varnish-purge-proxy Service:varnish Environment:live`

You can also specify host and port to listen on:

`./varnish-purge-proxy --listen=127.0.0.1 --port=8000`

You can also specify the destination port to target:

`./varnish-purge-proxy --destport=6081`

varnish-purge-proxy will cache the IP lookup for 60 seconds, you can change this as follows:

`./varnish-purge-proxy --cache=120`

Authentication
--------------

AWS access key and secret key can be added as environment variables, using either `AWS_ACCESS_KEY_ID` or `AWS_SECRET_ACCESS_KEY`.  If these are not available then IAM credentials for the instance will be checked.

Building
--------

Build a binary by running:

`go build varnish-purge-proxy.go`
