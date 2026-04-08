# GoClaw Monitoring Setup

This directory contains configuration files for monitoring GoClaw using Prometheus and Grafana.

## Quick Start

### 1. Start Prometheus

```bash
docker run -d \
  --name prometheus \
  -p 9090:9090 \
  -v $(pwd)/prometheus.yml:/etc/prometheus/prometheus.yml \
  prom/prometheus:latest
```

### 2. Start Grafana

```bash
docker run -d \
  --name grafana \
  -p 3000:3000 \
  -v grafana-storage:/var/lib/grafana \
  grafana/grafana:latest
```

### 3. Import Dashboard

1. Open Grafana at http://localhost:3000 (default credentials: admin/admin)
2. Navigate to "Dashboards" → "Import"
3. Upload `dashboard.json` or paste its contents
4. Select your Prometheus datasource

## Available Metrics

### Agent Metrics
- `agent_run_total` - Total number of agent runs (labels: agent_name, status)
- `agent_run_duration_seconds` - Agent run duration histogram (labels: agent_name, status)
- `active_runs` - Number of currently active runs

### Tool Metrics
- `tool_execution_total` - Total number of tool executions (labels: tool_name, status)
- `tool_execution_duration_seconds` - Tool execution duration histogram (labels: tool_name, status)

### Thread Metrics
- `active_threads` - Number of active threads

### Queue Metrics
- `queue_size` - Current queue size

### HTTP Metrics
- `http_request_total` - Total HTTP requests (labels: method, path, status)
- `http_request_duration_seconds` - HTTP request duration histogram (labels: method, path, status)

### Go Runtime Metrics
Standard Go runtime metrics are also exposed via the Prometheus client library.

## Health Check Endpoints

- `GET /health` - Service health status
- `GET /ready` - Readiness probe
- `GET /live` - Liveness probe (includes memory stats)
- `GET /metrics` - Prometheus metrics endpoint

## Alerting Rules (Example)

```yaml
groups:
  - name: goclaw-alerts
    rules:
      - alert: HighErrorRate
        expr: rate(agent_run_total{status="error"}[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High agent error rate detected"

      - alert: HighLatency
        expr: histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m])) > 5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High request latency detected"
```
