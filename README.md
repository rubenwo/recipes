# Mise

AI-powered recipe generation and meal planning. Uses Ollama (local LLM) to generate recipes with web search, database search, and optional Edamam API integration.

## Running locally (development)

```bash
go run build.go
cd backend && ./server.exe   # Windows
cd backend && ./server       # Linux/macOS
```

## Docker

The image is automatically built and pushed to [ghcr.io/rubenwo/mise](https://ghcr.io/rubenwo/mise) on every push to `master`.

### Pull the image

```bash
docker pull ghcr.io/rubenwo/mise:latest
```

### Run with Docker

Create a `config.yaml` based on `backend/config.example.yaml`, then:

```bash
docker run -d \
  --name mise \
  -p 8080:8080 \
  -v /path/to/config.yaml:/app/config.yaml \
  -v /path/to/images:/app/images \
  ghcr.io/rubenwo/mise:latest
```

**Volume mounts:**
- `/app/config.yaml` — required, your production config file
- `/app/images` — persistent storage for downloaded recipe images

**Production `config.yaml` notes:**
- Set `server.cors_origin` to your host URL (e.g. `http://192.168.1.100:8080`) instead of `localhost:5173`
- Point `database.host` to your PostgreSQL instance
- Point `ollama.host` to your Ollama instance

---

## Deploying on TrueNAS Scale

The steps below use the **Custom App** feature in the TrueNAS web UI.

### Prerequisites

1. A running PostgreSQL instance accessible from TrueNAS (can be a TrueNAS app or external)
2. A running Ollama instance accessible from TrueNAS
3. A directory on TrueNAS for the config file and images (e.g. `/mnt/tank/mise`)

### Step 1 — Prepare config and storage

SSH into TrueNAS and create the required directories:

```bash
mkdir -p /mnt/tank/mise/images
```

Create `/mnt/tank/mise/config.yaml` based on `backend/config.example.yaml`.
Update the hosts to point to your actual PostgreSQL and Ollama addresses:

```yaml
server:
  port: 8080
  cors_origin: "http://<truenas-ip>:8080"

database:
  host: <postgres-host>
  port: 5432
  user: postgres
  password: "yourpassword"
  name: mise
  sslmode: disable

ollama:
  host: "http://<ollama-host>:11434"
  model: "qwen2.5:7b"
  generation_timeout: 60s
  max_tool_iterations: 5

search:
  timeout: 10s
  cache_ttl: 5m
```

### Step 2 — Make the GHCR package public

Go to **GitHub → Your profile → Packages → mise → Package settings → Change visibility → Public**.

This allows TrueNAS to pull the image without credentials.

### Step 3 — Create the Custom App in TrueNAS

1. Open the TrueNAS web UI
2. Go to **Apps → Discover Apps → Custom App**
3. Fill in the form:

| Field | Value |
|---|---|
| Application Name | `mise` |
| Image Repository | `ghcr.io/rubenwo/mise` |
| Image Tag | `latest` |
| Container Port | `8080` |
| Node Port | `8080` (or any free port) |

4. Under **Storage** add two mounts:

| Host Path | Mount Path |
|---|---|
| `/mnt/tank/mise/config.yaml` | `/app/config.yaml` |
| `/mnt/tank/mise/images` | `/app/images` |

5. Click **Install**

### Step 4 — Verify

Open `http://<truenas-ip>:8080` in your browser. The Mise app should be running.

---

## Updating to a new image version (manual)

When a new image is pushed to GHCR, TrueNAS does not auto-update. To update manually:

1. Go to **Apps → Installed Apps → mise**
2. Click the three-dot menu → **Edit**
3. Change the Image Tag to the specific SHA you want (visible on the [GHCR page](https://github.com/rubenwo/mise/pkgs/container/mise))
4. Click **Save**

