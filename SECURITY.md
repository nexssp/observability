# Security Policy

Please do not disclose exploitable vulnerabilities in public issues. Report them privately to the repository maintainers with a reproduction, affected version, impact, and proposed mitigation.

## Telemetry & Context Security Model

Nexss Observability intercepts application contexts and attaches trace correlation metadata across boundaries:

- **PII and Sensitive Data:** Do not pass unmasked credentials, tokens, or personal identifiers into span attributes or log fields.
- **Trace Propagation:** Outbound HTTP clients wrapped via `httptrace.WrapClient` inject W3C standard traceparent headers. Ensure target upstream services are trusted before distributing correlation contexts.
- **Health Probes:** Readiness probes exposed by `HealthHandler()` should not leak confidential infrastructure details in failure payloads.
