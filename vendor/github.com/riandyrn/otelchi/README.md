# otelchi

[![compatibility-test](https://github.com/riandyrn/otelchi/actions/workflows/compatibility-test.yaml/badge.svg)](https://github.com/riandyrn/otelchi/actions/workflows/compatibility-test.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/riandyrn/otelchi)](https://goreportcard.com/report/github.com/riandyrn/otelchi)
[![Documentation](https://godoc.org/github.com/riandyrn/otelchi?status.svg)](https://pkg.go.dev/mod/github.com/riandyrn/otelchi)

OpenTelemetry instrumentation for [go-chi/chi](https://github.com/go-chi/chi).

Essentially this is an adaptation from [otelmux](https://github.com/open-telemetry/opentelemetry-go-contrib/tree/main/instrumentation/github.com/gorilla/mux/otelmux) but instead of using `gorilla/mux`, we use `go-chi/chi`.

Currently, this library can only instrument traces and metrics.

Contributions are welcomed!

## Install

```bash
$ go get github.com/riandyrn/otelchi
```

## Examples

See [examples](./examples) for details.

## Metrics

The `metric` package provides OpenTelemetry semantic-convention compliant HTTP server metric middleware:

- `http.server.request.duration`
- `http.server.active_requests`
- `http.server.request.body.size`
- `http.server.response.body.size`

Legacy metric middleware for `request_duration_millis`, `requests_inflight`, and `response_size_bytes` is still available but deprecated.

## Why Port This?

I was planning to make this project as part of the Open Telemetry Go instrumentation project. However, based on [this comment](https://github.com/open-telemetry/opentelemetry-go-contrib/pull/986#issuecomment-941280855) they no longer accept new instrumentation. This is why I maintain this project here.
