# struggledns

A simple server which allows you to forward dns requests to multiple dns servers
(regardless of which are the authoritative servers) and return the first usable
response.

This is a small utility which we found useful in our development environments as
something which can run locally inside vagrant. It is not recommended to use
this in production or any high-load situation.

## Build

    go get github.com/levenlabs/struggledns

## Usage

Parameters can be set using (in order of precedence):

- Command line parameters
- Environment variables (e.g. `STRUGGLEDNS_LISTEN_ADDR`)
- Configuration file (see `--example` command line parameter)

## Example

    struggledns --fwd-to 10.0.0.5:53 --fwd-to 8.8.8.8:53

When a dns request is received using this configuration, struggledns will first
see if the server at `10.0.0.5:53` is alive and returns a dns response with a
non-empty answer section. If so that is returned, otherwise the server at
`8.8.8.8:53` will be checked. The order of checking is always in the order the
`--fwd-to` parameters are specified.

In reality struggledns will make a dns request for every `--fwd-to` at the same
time, for every incoming request. It then checks responses in order. This way
there's not much of a speed penalty, at the cost of more bandwidth and more hits
to the remote servers.
