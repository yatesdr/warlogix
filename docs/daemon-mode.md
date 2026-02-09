# Daemon Mode

Daemon mode runs WarLogix as a background service with SSH access, allowing remote management of the TUI from any SSH client. This is useful for headless servers, edge devices, or situations where you want WarLogix running continuously while connecting to configure or monitor it as needed.

## Quick Start

```bash
# Start daemon with password authentication
./warlogix -d --ssh-password "your-password"

# Connect from any SSH client
ssh -p 2222 localhost
```

## Command Line Options

| Flag | Description |
|------|-------------|
| `-d` | Enable daemon mode |
| `-p <port>` | SSH port (default: 2222) |
| `--ssh-password <pw>` | Password for SSH authentication |
| `--ssh-keys <path>` | Path to authorized_keys file or directory |
| `--namespace <name>` | Set namespace (required if not already configured) |

## Authentication

Daemon mode requires at least one authentication method. You can use password authentication, key-based authentication, or both.

### Password Authentication

```bash
./warlogix -d --ssh-password "secret123"
```

Any SSH client can connect using this password.

### Key-Based Authentication

Key-based authentication is more secure than passwords and recommended for production deployments.

#### Generating SSH Keys

If you don't already have an SSH key pair, generate one:

```bash
# Generate an Ed25519 key (recommended)
ssh-keygen -t ed25519 -C "warlogix-access"

# Or generate an RSA key (wider compatibility)
ssh-keygen -t rsa -b 4096 -C "warlogix-access"
```

This creates two files:
- `~/.ssh/id_ed25519` (or `id_rsa`) - Your private key (keep secret)
- `~/.ssh/id_ed25519.pub` (or `id_rsa.pub`) - Your public key (share with WarLogix)

#### Setting Up Authorized Keys

Create an authorized_keys file containing the public keys of users who should have access:

```bash
# Copy your public key to an authorized_keys file
cat ~/.ssh/id_ed25519.pub >> ~/.warlogix/authorized_keys

# Add additional users' public keys
cat /path/to/other_user.pub >> ~/.warlogix/authorized_keys
```

The authorized_keys file uses the standard OpenSSH format (one public key per line):

```
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... user1@host1
ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAB... user2@host2
```

#### Starting with Key Authentication

```bash
# Use a specific authorized_keys file
./warlogix -d --ssh-keys ~/.warlogix/authorized_keys

# Use a directory containing an authorized_keys file
./warlogix -d --ssh-keys ~/.warlogix/
```

#### Connecting with Keys

```bash
# SSH automatically uses keys from ~/.ssh/
ssh -p 2222 localhost

# Specify a particular key
ssh -p 2222 -i ~/.ssh/id_ed25519 localhost
```

### Combined Authentication

```bash
./warlogix -d --ssh-password "backup" --ssh-keys ~/.ssh/authorized_keys
```

Clients can authenticate with either method.

## Namespace Requirement

Daemon mode requires a namespace to be configured. The namespace identifies this WarLogix instance and is used in MQTT topics, Kafka topics, and Valkey keys.

```bash
# Set namespace when starting daemon
./warlogix -d --namespace "factory1" --ssh-password "secret"

# Or configure namespace first in local mode, then start daemon
./warlogix --namespace "factory1"
# (configure PLCs, brokers, etc.)
# Then start daemon mode
./warlogix -d --ssh-password "secret"
```

## Connecting

Connect using any SSH client:

```bash
# Default port
ssh -p 2222 user@hostname

# Custom port
ssh -p 3333 user@hostname
```

The username is ignored - any username works. Only the password or key matters for authentication.

## Session Behavior

### Shared View

All connected SSH sessions share the same TUI view. This is a PTY multiplexing design - everyone sees the same screen and can interact with it. This is useful for collaborative troubleshooting or monitoring.

### Keyboard Shortcuts

Most keyboard shortcuts work the same as local mode, with one exception:

| Key | Local Mode | Daemon Mode |
|-----|------------|-------------|
| `Shift+Q` | Quit application | Disconnect SSH session |

In daemon mode, `Shift+Q` disconnects your SSH session (and all other connected sessions) but leaves the daemon running. The daemon continues polling PLCs and publishing data.

## Stopping the Daemon

The daemon responds to standard signals:

```bash
# Graceful shutdown
kill <pid>
# or
kill -TERM <pid>

# Also works
kill -INT <pid>
# or press Ctrl+C in the terminal where daemon was started
```

Shutdown sequence:
1. Disconnects all SSH sessions
2. Stops the TUI
3. Disconnects from PLCs
4. Stops all broker connections
5. Exits cleanly

## Running in Background

### Using nohup

```bash
nohup ./warlogix -d --ssh-password "secret" > /var/log/warlogix.log 2>&1 &
```

### Using systemd (Ubuntu / Debian / Rocky Linux)

#### 1. Create a service user

```bash
# Create system user with no login shell
sudo useradd -r -s /usr/sbin/nologin -d /opt/warlogix -m warlogix

# Create config directory
sudo mkdir -p /opt/warlogix/.warlogix
sudo chown warlogix:warlogix /opt/warlogix/.warlogix
```

#### 2. Install the binary

```bash
# Download (adjust version and architecture as needed)
sudo curl -L -o /usr/local/bin/warlogix \
  https://github.com/yatesdr/warlogix/releases/download/v1.0.0/warlogix-linux-amd64
sudo chmod +x /usr/local/bin/warlogix
```

#### 3. Create the systemd service

Create `/etc/systemd/system/warlogix.service`:

```ini
[Unit]
Description=WarLogix PLC Gateway
Documentation=https://github.com/yatesdr/warlogix
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=warlogix
Group=warlogix
WorkingDirectory=/opt/warlogix

# Basic setup with password auth
ExecStart=/usr/local/bin/warlogix -d -p 2222 --ssh-password "changeme" --config /opt/warlogix/.warlogix/config.yaml

# Or use key-based auth (recommended)
#ExecStart=/usr/local/bin/warlogix -d -p 2222 --ssh-keys /opt/warlogix/.warlogix/authorized_keys --config /opt/warlogix/.warlogix/config.yaml

# Restart policy
Restart=on-failure
RestartSec=10
StartLimitInterval=60
StartLimitBurst=3

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/opt/warlogix/.warlogix

# Resource limits (adjust as needed)
LimitNOFILE=65535
MemoryMax=512M

[Install]
WantedBy=multi-user.target
```

#### 4. Set up authentication

For password auth, edit the service file and set a strong password.

For key-based auth:

```bash
# Create authorized_keys file
sudo touch /opt/warlogix/.warlogix/authorized_keys
sudo chown warlogix:warlogix /opt/warlogix/.warlogix/authorized_keys
sudo chmod 600 /opt/warlogix/.warlogix/authorized_keys

# Add your public key
echo "ssh-ed25519 AAAA... your-email@example.com" | sudo tee -a /opt/warlogix/.warlogix/authorized_keys
```

#### 5. Initialize configuration

Before starting the daemon, create an initial config with a namespace:

```bash
# Run once interactively to set namespace and configure PLCs
sudo -u warlogix /usr/local/bin/warlogix --config /opt/warlogix/.warlogix/config.yaml --namespace "factory1"
# Press Q to quit after initial setup
```

Or create a minimal config directly:

```bash
cat <<EOF | sudo tee /opt/warlogix/.warlogix/config.yaml
namespace: factory1
poll_rate: 250
plcs: []
mqtt: []
kafka: []
valkey: []
EOF
sudo chown warlogix:warlogix /opt/warlogix/.warlogix/config.yaml
```

#### 6. Enable and start

```bash
sudo systemctl daemon-reload
sudo systemctl enable warlogix
sudo systemctl start warlogix

# Check status
sudo systemctl status warlogix

# View logs
sudo journalctl -u warlogix -f
```

#### Rocky Linux / RHEL SELinux Note

On Rocky Linux, AlmaLinux, or RHEL with SELinux enabled, you may need to allow the service to bind to the SSH port:

```bash
# Allow warlogix to listen on port 2222
sudo semanage port -a -t ssh_port_t -p tcp 2222

# If using a custom port, replace 2222 with your port
```

### Using OpenRC (Alpine Linux)

Alpine Linux uses OpenRC instead of systemd. Here's how to set up WarLogix on Alpine.

#### 1. Install dependencies

```bash
# Update package index
apk update

# Install required packages (musl-compatible binary needed)
apk add curl
```

#### 2. Create service user

```bash
# Create system user
adduser -D -S -h /opt/warlogix -s /sbin/nologin warlogix

# Create config directory
mkdir -p /opt/warlogix/.warlogix
chown warlogix:warlogix /opt/warlogix/.warlogix
```

#### 3. Install the binary

```bash
# Download the musl/static binary for Alpine
curl -L -o /usr/local/bin/warlogix \
  https://github.com/yatesdr/warlogix/releases/download/v1.0.0/warlogix-linux-amd64-static
chmod +x /usr/local/bin/warlogix
```

Note: Alpine uses musl libc, not glibc. You need a statically-linked binary or one built for musl. If the standard binary doesn't work, build from source on Alpine:

```bash
apk add go git
git clone https://github.com/yatesdr/warlogix.git
cd warlogix
CGO_ENABLED=0 go build -o /usr/local/bin/warlogix ./cmd/warlogix
```

#### 4. Create the OpenRC init script

Create `/etc/init.d/warlogix`:

```bash
#!/sbin/openrc-run

name="warlogix"
description="WarLogix PLC Gateway"

command="/usr/local/bin/warlogix"
command_args="-d -p 2222 --ssh-keys /opt/warlogix/.warlogix/authorized_keys --config /opt/warlogix/.warlogix/config.yaml"
command_user="warlogix:warlogix"
command_background="yes"
pidfile="/run/${RC_SVCNAME}.pid"

output_log="/var/log/warlogix.log"
error_log="/var/log/warlogix.err"

depend() {
    need net
    after firewall
}

start_pre() {
    checkpath -d -m 0755 -o warlogix:warlogix /opt/warlogix/.warlogix

    # Verify config exists
    if [ ! -f /opt/warlogix/.warlogix/config.yaml ]; then
        eerror "Config file not found. Run warlogix manually first to initialize."
        return 1
    fi
}
```

Make it executable:

```bash
chmod +x /etc/init.d/warlogix
```

#### 5. Set up authentication

```bash
# Create authorized_keys
touch /opt/warlogix/.warlogix/authorized_keys
chown warlogix:warlogix /opt/warlogix/.warlogix/authorized_keys
chmod 600 /opt/warlogix/.warlogix/authorized_keys

# Add your public key
echo "ssh-ed25519 AAAA... your-email@example.com" >> /opt/warlogix/.warlogix/authorized_keys
```

For password auth, modify the init script's `command_args`:

```bash
command_args="-d -p 2222 --ssh-password 'your-password' --config /opt/warlogix/.warlogix/config.yaml"
```

#### 6. Initialize configuration

```bash
# Create initial config
cat <<EOF > /opt/warlogix/.warlogix/config.yaml
namespace: factory1
poll_rate: 250
plcs: []
mqtt: []
kafka: []
valkey: []
EOF
chown warlogix:warlogix /opt/warlogix/.warlogix/config.yaml
```

#### 7. Enable and start

```bash
# Add to default runlevel
rc-update add warlogix default

# Start the service
rc-service warlogix start

# Check status
rc-service warlogix status

# View logs
tail -f /var/log/warlogix.log
```

#### 8. Alpine firewall (if using awall)

```bash
# Allow SSH access to warlogix port
cat <<EOF > /etc/awall/optional/warlogix.json
{
  "filter": [
    {
      "in": "internet",
      "out": "_fw",
      "service": { "proto": "tcp", "port": 2222 },
      "action": "accept"
    }
  ]
}
EOF

awall enable warlogix
awall activate
```

#### Alpine in Docker

Alpine is commonly used as a Docker base image:

```dockerfile
FROM alpine:3.19

# Install ca-certificates for TLS connections
RUN apk add --no-cache ca-certificates

# Copy statically-linked binary
COPY warlogix /usr/local/bin/warlogix

# Create user
RUN adduser -D -S -h /opt/warlogix warlogix

USER warlogix
WORKDIR /opt/warlogix

EXPOSE 2222 8080

ENTRYPOINT ["/usr/local/bin/warlogix"]
CMD ["-d", "-p", "2222", "--ssh-password", "changeme"]
```

Build with a static binary:

```bash
CGO_ENABLED=0 GOOS=linux go build -o warlogix-static ./cmd/warlogix
docker build -t warlogix:alpine .
```

### Using Docker

```dockerfile
FROM debian:bookworm-slim
COPY warlogix /usr/local/bin/
EXPOSE 2222 8080
CMD ["warlogix", "-d", "--ssh-password", "secret"]
```

```bash
docker run -d -p 2222:2222 -p 8080:8080 -v ~/.warlogix:/root/.warlogix warlogix
```

## Logging

Daemon mode supports the same logging options as local mode:

```bash
# Log to file
./warlogix -d --ssh-password "secret" --log /var/log/warlogix.log

# Enable protocol debugging
./warlogix -d --ssh-password "secret" --log-debug

# Debug specific protocols
./warlogix -d --ssh-password "secret" --log-debug=mqtt,kafka
```

**Warning:** Debug logging (`--log-debug`) generates extremely verbose output including protocol-level hex dumps. Log files can grow to gigabytes within hours on active systems. Use debug logging only for troubleshooting specific issues, not in typical deployments. Always specify a protocol filter (e.g., `--log-debug=s7`) rather than logging all protocols when possible.

## Configuration

### Configuration File Location

WarLogix uses a single configuration file, by default at `~/.warlogix/config.yaml`. Use `--config` to specify an alternate location:

```bash
./warlogix -d --config /etc/warlogix/config.yaml --ssh-password "secret"
```

### When Configuration is Loaded

Configuration is loaded **once at startup**. The daemon reads the config file when it starts and uses those settings for the duration of its run.

### Live Configuration Changes

Changes made through the TUI (via SSH) are saved to disk immediately:
- Adding/editing/removing PLCs
- Adding/editing/removing brokers
- Enabling/disabling tags
- Changing TagPack or Trigger settings

These changes take effect immediately in the running daemon and are persisted to the config file.

### External Configuration Changes

If you modify the config file externally (with a text editor, Ansible, etc.) while the daemon is running:

- **The running daemon will NOT see these changes** - it continues using the config it loaded at startup
- **Changes will be overwritten** - the next time the daemon saves (any TUI change), it will overwrite the file with its in-memory config

To apply external config changes:

```bash
# Restart the daemon to pick up external changes
sudo systemctl restart warlogix

# Or stop and start manually
kill <pid>
./warlogix -d --ssh-password "secret"
```

### Configuration Deployment Workflow

For automated deployments (Ansible, Puppet, etc.):

1. Stop the daemon
2. Deploy the new config file
3. Start the daemon

## Ansible Deployment

WarLogix is well-suited for Ansible-based deployment to edge devices and factory servers.

### Example Playbook

```yaml
---
- name: Deploy WarLogix
  hosts: plc_gateways
  become: yes
  vars:
    warlogix_version: "1.0.0"
    warlogix_ssh_port: 2222
    warlogix_namespace: "{{ inventory_hostname }}"

  tasks:
    - name: Create warlogix user
      user:
        name: warlogix
        system: yes
        shell: /bin/false
        home: /opt/warlogix

    - name: Create config directory
      file:
        path: /opt/warlogix/.warlogix
        state: directory
        owner: warlogix
        mode: '0755'

    - name: Download warlogix binary
      get_url:
        url: "https://github.com/yatesdr/warlogix/releases/download/v{{ warlogix_version }}/warlogix-linux-amd64"
        dest: /usr/local/bin/warlogix
        mode: '0755'

    - name: Deploy configuration
      template:
        src: warlogix-config.yaml.j2
        dest: /opt/warlogix/.warlogix/config.yaml
        owner: warlogix
        mode: '0600'
      notify: restart warlogix

    - name: Deploy authorized_keys
      copy:
        src: warlogix_authorized_keys
        dest: /opt/warlogix/.warlogix/authorized_keys
        owner: warlogix
        mode: '0600'
      notify: restart warlogix

    - name: Deploy systemd service
      template:
        src: warlogix.service.j2
        dest: /etc/systemd/system/warlogix.service
      notify:
        - reload systemd
        - restart warlogix

    - name: Enable and start warlogix
      systemd:
        name: warlogix
        enabled: yes
        state: started

  handlers:
    - name: reload systemd
      systemd:
        daemon_reload: yes

    - name: restart warlogix
      systemd:
        name: warlogix
        state: restarted
```

### Systemd Service Template

`warlogix.service.j2`:

```ini
[Unit]
Description=WarLogix PLC Gateway
After=network.target

[Service]
Type=simple
User=warlogix
WorkingDirectory=/opt/warlogix
ExecStart=/usr/local/bin/warlogix -d -p {{ warlogix_ssh_port }} --ssh-keys /opt/warlogix/.warlogix/authorized_keys --config /opt/warlogix/.warlogix/config.yaml
Restart=on-failure
RestartSec=10

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/opt/warlogix/.warlogix

[Install]
WantedBy=multi-user.target
```

### Configuration Template

`warlogix-config.yaml.j2`:

```yaml
namespace: {{ warlogix_namespace }}
poll_rate: 250

plcs:
{% for plc in warlogix_plcs %}
  - name: {{ plc.name }}
    address: {{ plc.address }}
    family: {{ plc.family }}
    slot: {{ plc.slot | default(0) }}
    enabled: {{ plc.enabled | default(true) }}
{% endfor %}

mqtt:
{% for broker in warlogix_mqtt_brokers | default([]) %}
  - name: {{ broker.name }}
    broker: {{ broker.host }}
    port: {{ broker.port | default(1883) }}
    enabled: {{ broker.enabled | default(true) }}
{% endfor %}
```

### Deployment Considerations

1. **Use key-based auth for Ansible-managed nodes** - Avoid storing passwords in playbooks. Deploy authorized_keys instead.

2. **Namespace per host** - Use `{{ inventory_hostname }}` or a similar pattern to ensure unique namespaces across your fleet.

3. **Config changes require restart** - The `notify: restart warlogix` handler ensures config changes are applied.

4. **Avoid TUI changes on managed nodes** - If using Ansible as the source of truth, discourage manual TUI changes that would drift from the Ansible-managed config.

5. **Health checks** - Add a health check task to verify the daemon is running:

```yaml
- name: Verify warlogix is running
  uri:
    url: "http://localhost:8080/"
    status_code: 200
  when: warlogix_rest_enabled | default(false)
```

6. **Log aggregation** - Use `--log` to write to a file that your log collector can pick up:

```ini
ExecStart=/usr/local/bin/warlogix -d ... --log /var/log/warlogix/warlogix.log
```

7. **Firewall rules** - Open the SSH port for management access:

```yaml
- name: Allow warlogix SSH
  ufw:
    rule: allow
    port: "{{ warlogix_ssh_port }}"
    proto: tcp
```

### Backing Up Configuration

The single-file config makes backup simple:

```bash
# Backup
cp ~/.warlogix/config.yaml ~/.warlogix/config.yaml.bak

# Restore
cp ~/.warlogix/config.yaml.bak ~/.warlogix/config.yaml
# Then restart daemon to apply
```

## Auto-Start Behavior

In daemon mode, WarLogix automatically:

1. Connects to PLCs marked as auto-connect
2. Starts the REST API if enabled
3. Connects to MQTT brokers marked as auto-connect
4. Connects to Valkey servers marked as auto-connect
5. Connects to Kafka clusters marked as auto-connect
6. Arms triggers marked as enabled

This means a properly configured WarLogix can start publishing data immediately on boot without any manual intervention.

## Platform Support

| Platform | Daemon Mode |
|----------|-------------|
| Linux | Supported |
| macOS | Supported |
| Windows | Not supported |

Windows does not support the PTY functionality required for daemon mode. On Windows, run WarLogix in local mode or use WSL.

## Security Considerations

- Use strong passwords or key-based authentication
- Consider firewall rules to restrict SSH access
- The SSH server uses a new host key on each start (no persistent key)
- All sessions share the same view - don't connect untrusted users
- Configuration changes made via SSH are saved to disk
