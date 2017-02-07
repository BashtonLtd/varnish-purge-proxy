# varnish-purge-proxy

[![Build Status](https://travis-ci.org/BashtonLtd/varnish-purge-proxy.svg?branch=master)](https://travis-ci.org/BashtonLtd/varnish-purge-proxy)

Proxy purge requests to multiple varnish servers

Works with AWS or GCE.

## Global options

You can also specify host and port to listen on:

`./varnish-purge-proxy aws --listen=127.0.0.1 --port=8000`

You can also specify the destination port to target:

`./varnish-purge-proxy aws --destport=6081`

varnish-purge-proxy will cache the IP lookup for 60 seconds, you can change this as follows:

`./varnish-purge-proxy aws --cache=120`

## AWS

Specify tags to limit instances that receive the purge request, multiple tags can be used. You must specify at least one tag.

### Example

`./varnish-purge-proxy aws Service:varnish Environment:live`


### Authentication

AWS access key and secret key can be added as environment variables, using either `AWS_ACCESS_KEY_ID` or `AWS_SECRET_ACCESS_KEY`.  If these are not available then IAM credentials for the instance will be checked.


## GCE

Specify an instance name prefix to limit instances that receive the purge request with the `--nameprefix` argument.

### Example

`varnish-purge-proxy gce --credentials=creds.json --region=us-central1 --project=my-project --nameprefix=varnish`

### Authentication

GCE credentials should be provided using the `--credentials` argument.

## Building

Build a binary by running:

`go build varnish-purge-proxy.go`
