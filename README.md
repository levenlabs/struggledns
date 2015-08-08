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

### Groups

    struggledns --fwd-to 10.0.0.5:53,8.8.8.8:53 --fwd-to 10.0.0.6:53,8.8.4.4:53

Each `--fwd-to` can be a group of comma-delimited addresses to query in
parallel. Every address in a group is queried in parallel and groups are queried
in serial unless `--parallel` is specified, which causes **all** sent addresses
from all groups to be queried in parallel. The response still respects the order
of the addresses despite being sent in parallel. In the above example, when a
query is received it will be first sent to both `10.0.0.5:53` and `8.8.8.8:53`
and if both of those fail then it will make a request to both `10.0.0.6:53` and
`8.8.4.4:53`.
