# fdb - FoundryDB CLI

A command-line interface for the [FoundryDB](https://foundrydb.com) managed database platform. Manage PostgreSQL, MySQL, MongoDB, Valkey, Kafka, OpenSearch, and MSSQL services from your terminal.

## Installation

### Download pre-built binary

Download the latest release from the [releases page](https://github.com/anorph/foundrydb-cli/releases).

```bash
# macOS (Apple Silicon)
curl -L https://github.com/anorph/foundrydb-cli/releases/latest/download/fdb-darwin-arm64 -o /usr/local/bin/fdb
chmod +x /usr/local/bin/fdb

# macOS (Intel)
curl -L https://github.com/anorph/foundrydb-cli/releases/latest/download/fdb-darwin-amd64 -o /usr/local/bin/fdb
chmod +x /usr/local/bin/fdb

# Linux (amd64)
curl -L https://github.com/anorph/foundrydb-cli/releases/latest/download/fdb-linux-amd64 -o /usr/local/bin/fdb
chmod +x /usr/local/bin/fdb
```

### Build from source

Requires Go 1.24+.

```bash
git clone https://github.com/anorph/foundrydb-cli.git
cd foundrydb-cli
CGO_ENABLED=0 go build -o fdb ./cmd/fdb/
sudo mv fdb /usr/local/bin/
```

## Configuration

### Login (recommended)

```bash
fdb auth login
```

This prompts for your API URL, username, and password, verifies the credentials, and saves them to `~/.fdb/config.toml`.

### Config file

Credentials are stored at `~/.fdb/config.toml`:

```toml
api_url = "https://api.foundrydb.com"
username = "admin"
password = "your-password"
```

The file is created with `0600` permissions (owner read/write only).

### Environment variables

All config values can be set via environment variables with the `FDB_` prefix:

```bash
export FDB_API_URL=https://api.foundrydb.com
export FDB_USERNAME=admin
export FDB_PASSWORD=your-password
```

### Global flags

These flags work with every command:

```
--api-url string    API base URL (default: https://api.foundrydb.com)
--username string   Username (default: admin)
--password string   Password
--org string        Organization UUID or slug (sets X-Active-Org-ID header)
--json              Output raw JSON instead of formatted tables
--config string     Config file path (default: ~/.fdb/config.toml)
```

## Commands

### Organizations

```bash
# List all organizations you belong to
fdb org list

# List as JSON
fdb org list --json
```

### Authentication

```bash
# Save credentials
fdb auth login

# Save with flags (non-interactive)
fdb auth login --api-url https://api.foundrydb.com --username admin --password secret

# Check authentication status
fdb auth status

# Remove saved credentials
fdb auth logout
```

### Services

```bash
# List all services
fdb services list

# List services as JSON
fdb services list --json

# Get service details (by ID or name)
fdb services get my-postgres
fdb services get 8f3a2c1d-...

# Create a service (interactive prompts for missing fields)
fdb services create

# Create a service with flags (non-interactive)
fdb services create \
  --name my-postgres \
  --type postgresql \
  --version 17 \
  --plan tier-2 \
  --zone se-sto1 \
  --storage-size 50 \
  --storage-tier maxiops

# Create within a specific organization
fdb services create \
  --org my-org-slug \
  --name my-postgres \
  --type postgresql \
  --version 17

# Create with allowed CIDRs
fdb services create \
  --name my-postgres \
  --type postgresql \
  --version 17 \
  --allowed-cidrs "1.2.3.4/32,10.0.0.0/8"

# Delete a service (prompts for name confirmation)
fdb services delete <service-id>

# Delete without confirmation prompt
fdb services delete <service-id> --confirm
```

Supported database types: `postgresql`, `mysql`, `mongodb`, `valkey`, `kafka`, `opensearch`, `mssql`

Supported versions by type:
- **postgresql**: 14, 15, 16, 17, 18
- **mysql**: 8.4
- **mongodb**: 6.0, 7.0, 8.0
- **valkey**: 7.2, 8.0, 8.1, 9.0
- **kafka**: 3.6, 3.7, 3.8, 3.9, 4.0
- **opensearch: 2
- **mssql**: 4.8 (Babelfish/SQL Server compatible, TDS protocol)

### Connect (interactive shell)

Opens a native database shell using locally installed client tools.

```bash
# Connect to a service (auto-selects first user)
fdb connect my-postgres

# Connect as a specific user
fdb connect my-postgres --user app_user

# Connect to a specific database
fdb connect my-postgres --user app_user --database myapp
```

Required local tools by database type:

| Database   | Required tool |
|------------|---------------|
| PostgreSQL | `psql`        |
| MySQL      | `mysql`       |
| MongoDB    | `mongosh`     |
| Valkey     | `redis-cli`   |

### Connection Strings

```bash
# URL format (default)
fdb connection-string <service-id> --user app_user

# Shell environment variables
fdb connection-string <service-id> --user app_user --format env

# psql command
fdb connection-string <service-id> --user app_user --format psql

# mysql command
fdb connection-string <service-id> --user app_user --format mysql

# mongosh URI
fdb connection-string <service-id> --user app_user --format mongosh

# redis-cli command
fdb connection-string <service-id> --user app_user --format redis-cli

# Specify database name
fdb connection-string <service-id> --user app_user --database myapp --format env
```

### Database Users

```bash
# List users for a service
fdb users list <service-id>
fdb users list my-postgres

# Reveal password for a user
fdb users reveal-password <service-id> <username>
fdb users reveal-password my-postgres app_user
```

### Backups

```bash
# List backups for a service
fdb backups list <service-id>
fdb backups list my-postgres

# Trigger a manual backup
fdb backups trigger <service-id>
fdb backups trigger my-postgres
```

### Logs

```bash
# Get last 100 lines of logs (default)
fdb logs <service-id>

# Get last 500 lines
fdb logs <service-id> --lines 500

# Output as JSON
fdb logs <service-id> --json
```

### Metrics

```bash
# Show current metrics
fdb metrics <service-id>
fdb metrics my-postgres

# Output as JSON
fdb metrics my-postgres --json
```

### App Services

```bash
# List all app services
fdb apps list

# Get details of an app service
fdb apps get <id-or-name>

# Create an app service
fdb apps create --name my-app --image registry.example.com/myapp:latest --port 8080 --plan tier-2 --zone se-sto1

# Delete an app service (prompts for name confirmation)
fdb apps delete <id>

# Restart an app service
fdb apps restart <id>

# Retrieve logs
fdb apps logs <id> --lines 200
```

### App Custom Domains

```bash
# List custom domains for an app service
fdb apps domains list <app-id>

# Add a custom domain (starts in pending_verification status)
fdb apps domains add <app-id> --domain app.example.com

# Trigger an immediate verification pass for a pending domain
fdb apps domains verify <app-id> <domain-id>

# Remove a custom domain
fdb apps domains remove <app-id> <domain-id>
```

After adding a domain, point a DNS CNAME record at the `CNAME target` shown in the output. Then call `verify` to kick off the certificate issuance without waiting for the background worker.

### App Edge Settings

```bash
# Show edge status (enabled, home PoP, per-PoP convergence)
fdb apps edge status <app-id>

# Add a cache rule for a path prefix
fdb apps edge update-settings <app-id> --cache-path-prefix /static --cache-ttl 86400

# Set a rate limit (keyed by IP)
fdb apps edge update-settings <app-id> --rate-limit-rps 100 --rate-limit-burst 200 --rate-limit-key ip

# Enable WAF detect mode
fdb apps edge update-settings <app-id> --waf-mode detect

# Disable WAF
fdb apps edge update-settings <app-id> --waf-mode off

# Combine multiple settings in one call
fdb apps edge update-settings <app-id> \
  --cache-path-prefix /api \
  --cache-ttl 60 \
  --rate-limit-rps 50 \
  --rate-limit-burst 100 \
  --waf-mode detect

# Output as JSON
fdb apps edge status <app-id> --json
fdb apps edge update-settings <app-id> --waf-mode off --json
```

### Compliance

Generate and download signed compliance evidence packets for SOC2 or GDPR Article 30 (Records of Processing Activities).

```bash
# Generate a SOC2 evidence packet for an organization
fdb compliance generate --org <org-id> --framework soc2

# Generate a GDPR Art. 30 ROPA report
fdb compliance generate --org <org-id> --framework gdpr_ropa

# List all reports for an organization
fdb compliance list --org <org-id>

# List as JSON
fdb compliance list --org <org-id> --json

# Download a report as JSON (to stdout)
fdb compliance download --org <org-id> --report <report-id>

# Download a report as JSON to a file
fdb compliance download --org <org-id> --report <report-id> --out report.json

# Download the PDF variant
fdb compliance download --org <org-id> --report <report-id> --pdf --out report.pdf

# List published Ed25519 signing keys (for signature verification)
fdb compliance keys

# Output signing keys as JSON
fdb compliance keys --json
```

The `--org` flag may be omitted when the `--org` global flag or `FDB_ORG` environment variable is already set.

## Examples

### Full workflow: create and connect

```bash
# 1. Login
fdb auth login

# 2. Create a PostgreSQL service
fdb services create \
  --name dev-pg \
  --type postgresql \
  --version 17 \
  --plan tier-2 \
  --zone se-sto1 \
  --storage-size 50 \
  --storage-tier maxiops

# 3. Wait for it to be running
fdb services get dev-pg

# 4. List available users
fdb users list dev-pg

# 5. Connect interactively
fdb connect dev-pg --user app_user

# 6. Or get connection string for your app
fdb connection-string dev-pg --user app_user --format env
```

### Backup workflow

```bash
# Trigger a backup
fdb backups trigger my-postgres

# List all backups
fdb backups list my-postgres

# Output as JSON for scripting
fdb backups list my-postgres --json | jq '.backups[] | select(.status == "completed")'
```

### Scripting with JSON output

```bash
# Get all running services
fdb services list --json | jq '.services[] | select(.status == "running") | .name'

# Get service ID by name
SERVICE_ID=$(fdb services list --json | jq -r '.services[] | select(.name == "my-postgres") | .id')

# Reveal password and export as env var
eval "$(fdb connection-string "$SERVICE_ID" --user app_user --format env)"
echo "Connected to $PGHOST"
```

## License

Apache 2.0. See [LICENSE](LICENSE).
