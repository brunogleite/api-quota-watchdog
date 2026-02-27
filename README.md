<p align="center">
  <img src="./assets/logo.png" alt="API Quota Watchdog" width="350"/>
</p>

<h1 align="center">API Quota Watchdog</h1>
<p align="center"><em>Monitor and protect your API usage</em></p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=flat-square" />
  <img src="https://img.shields.io/badge/Status-In%20Development-orange?style=flat-square" />
</p>

---

## What is API Quota Watchdog?

API Quota Watchdog is a self-hosted, centralized proxy service that sits between your internal services and your external API dependencies. Instead of your services calling OpenAI, Twilio, Google Maps, or any other provider directly, they call the Watchdog — which forwards the request, tracks usage, enforces quotas, and alerts you before you hit a limit or burn through your budget.

No more surprise bills. No more silent quota exhaustion breaking your product at 2am.

---

## The Problem

Modern applications depend on many external APIs. Each one has its own limits, its own billing, and its own way of telling you when you've gone too far — usually after it's already too late.

Without a centralized view you're flying blind:

- A bug causes an infinite loop of OpenAI calls — you find out when your bill arrives
- Your Google Maps quota runs out at 6pm but your app runs until midnight
- You have no idea which of your internal services is consuming the most quota
- Rotating an API key means hunting through multiple codebases

API Quota Watchdog solves all of this in one place.

---

## How It Works

Every outgoing request to an external API is routed through the Watchdog:

```
Your Service  ──►  Watchdog  ──►  External API (OpenAI, Twilio, etc.)
                      │
                      ▼
               Logs request
               Checks quota
               Updates counters
               Fires alerts if needed
```

Your internal services don't need to know anything about quotas or limits. They just call the Watchdog like they would call the external API directly.

---

## Features

- **Centralized Proxy** — all external API calls flow through one place
- **Quota Enforcement** — block or throttle requests when limits are approached
- **Real-Time Dashboard** — live usage streamed via SSE (Server-Sent Events)
- **Threshold Alerts** — get notified at 80%, 90%, or any custom threshold via webhook
- **Per-Service Breakdown** — see which internal service is consuming the most quota
- **Persistent Usage History** — survives restarts, tracks historical patterns
- **Hot-Reloadable Config** — add or update providers via YAML without restarting
- **Graceful Degradation** — return cached or fallback responses when quota is exhausted
- **Cost Tracking** — estimate spend per provider based on request volume

---

## Quick Start

```bash
# Clone the repository
git clone https://github.com/yourname/api-quota-watchdog
cd api-quota-watchdog

# Configure your providers
cp config.example.yaml config.yaml

# Run the service
go run ./cmd/watchdog
```

---

## Configuration

Define your external API providers and their quota rules in `config.yaml`:

```yaml
providers:
  - name: OpenAI
    key: sk-your-key-here
    limits:
      per_minute: 60
      per_day: 10000
    thresholds:
      warn_at: 80
      block_at: 100
    cost_per_request: 0.002

  - name: Twilio
    key: your-twilio-key
    limits:
      per_day: 100
    thresholds:
      warn_at: 70
      block_at: 100

  - name: GoogleMaps
    key: your-maps-key
    limits:
      per_day: 500
    thresholds:
      warn_at: 75
      block_at: 100
```

---

## Proxying a Request

Instead of calling an external API directly, point your service at the Watchdog:

```bash
# Before
POST https://api.openai.com/v1/chat/completions

# After
POST http://localhost:8080/proxy/openai/v1/chat/completions
```

The Watchdog handles the rest transparently.

---

<p align="center">Built with ☕ and Go</p>