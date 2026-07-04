# YaCy Wire Fixtures

This directory keeps golden request and response forms for YaCy peer protocol
surfaces. Files use plain `key=value` lines so they can be reviewed and compared
with upstream servlet behavior.

Naming convention:

- `<endpoint>-request.properties` for HTTP form fields sent to the endpoint.
- `<endpoint>-response.properties` for YaCy key-value response bodies.

Each fixture is validated by Go tests that parse the static wire form with
`yagoproto`, encode it again, and parse the encoded form back into the same
protocol value.
