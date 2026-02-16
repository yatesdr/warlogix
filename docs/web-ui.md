# Web UI Guide

## Overview

WarLink includes a browser-based management interface as an alternative to the TUI. The web UI provides the same core functionality — managing PLCs, configuring services, browsing tags, and monitoring system state — through a standard web browser accessible from any device on the network.

## Starting the Web Server

### Quick Start from the Command Line

The fastest way to enable the web UI is with the `--admin-user` and `--admin-pass` flags. This creates an admin account, enables the web server, and saves the configuration:

```bash
./warlink --admin-user admin --admin-pass yourpassword
```

The web UI will be available at `http://localhost:8080` and you can log in with the credentials you provided.

You can also override the port and bind address:

```bash
./warlink --admin-user admin --admin-pass yourpassword -p 9090 --host 127.0.0.1
```

### Other Ways to Enable

- **From the TUI** — Navigate to the REST API tab and toggle the web server on
- **Via configuration** — Set `web.enabled: true` and `web.ui.enabled: true` in `config.yaml`

Once started, the web UI is available at `http://<host>:8080` by default (port and bind address are configurable).

### Daemon Mode

The web UI works in daemon mode as well. Once enabled in the configuration (or via the `--admin-user` flag), it starts automatically alongside the SSH server:

```bash
./warlink -d --ssh-pass secret --admin-user admin --admin-pass yourpassword
```

This gives you both SSH access to the TUI and browser access to the web UI simultaneously.

See [Configuration Reference](configuration.md) for full details on the `web:` configuration key.

## First-Run Setup

When the web UI is first enabled with no users configured, you are redirected to a setup page to create an initial admin account. After creating the admin account, you are prompted to configure a namespace for this WarLink instance if one is not already set.

## Logging In

After initial setup, all access requires authentication through the login page.

### Roles

| Role | Capabilities |
|------|-------------|
| **admin** | Full access: view data, modify configuration, manage PLCs, manage users |
| **viewer** | Read-only access: view PLCs, tag values, service status, and logs |

Admins can create additional users and assign roles from the Users page.

## Pages

### Dashboard

The dashboard is the landing page after login. It provides an at-a-glance overview of the system status.

### PLCs

View all configured PLCs with connection status indicators. From this page you can:

- Connect or disconnect individual PLCs
- Add new PLCs manually or via network discovery
- Edit PLC settings (address, slot, family, poll rate)
- Delete PLCs

> ![PLCs page](screenshots/web-plcs.png)
> *Screenshot: PLCs management page showing connected PLCs with status indicators*

### Republisher

The tag browser tree displays all tags from connected PLCs with real-time value updates. Use this page to:

- Browse tags organized by PLC in a collapsible tree
- See live values update as they change on the PLC
- Enable or disable individual tags for publishing
- Write values to writable tags
- Manage ignored struct members for UDT change detection filtering
- Publish child struct members independently from their parent

> ![Republisher page](screenshots/web-republisher.png)
> *Screenshot: Tag browser tree with real-time values and publishing controls*

### MQTT

Manage MQTT broker connections. From this page you can:

- View configured brokers and their connection status
- Add, edit, or delete broker configurations
- Start or stop individual broker connections

> ![MQTT page](screenshots/web-mqtt.png)
> *Screenshot: MQTT broker list with connection status and controls*

### Valkey

Manage Valkey/Redis server connections. From this page you can:

- View configured servers and their connection status
- Add, edit, or delete server configurations
- Start or stop individual server connections

> ![Valkey page](screenshots/web-valkey.png)
> *Screenshot: Valkey server list with connection status and controls*

### Kafka

Manage Kafka cluster connections. From this page you can:

- View configured clusters and their connection status
- Add, edit, or delete cluster configurations
- Connect or disconnect individual clusters

> ![Kafka page](screenshots/web-kafka.png)
> *Screenshot: Kafka cluster list with connection status and controls*

### TagPacks

Create and manage TagPacks — groups of tags that publish together as a single atomic unit. From this page you can:

- Create new TagPacks or edit existing ones
- Add or remove member tags from any connected PLC
- Toggle per-service publishing (MQTT, Kafka, Valkey) for each pack

> ![TagPacks page](screenshots/web-tagpacks.png)
> *Screenshot: TagPack editor showing members and per-service publishing toggles*

### Rule Manager

Configure automation rules that monitor PLC tag conditions and execute actions. From this page you can:

- Create new rules or edit existing ones
- Enable or disable rules
- Test fire a rule manually to verify action behavior
- Configure multiple conditions with AND/OR logic
- Add publish, webhook, and writeback actions

### REST API

View the status of the built-in REST API, including whether it is currently enabled and serving requests.

> ![REST API page](screenshots/web-rest-api.png)
> *Screenshot: REST API status page*

### Debug Log

A live scrolling view of WarLink's debug log output. Messages appear in real time as they are generated. Use the clear button to reset the log view.

> ![Debug Log page](screenshots/web-debug-log.png)
> *Screenshot: Live debug log with scrolling output*

### Users

Manage web UI user accounts (admin only). From this page you can:

- Add new users with a username, password, and role
- Edit existing users (change password or role)
- Delete user accounts

> ![Users page](screenshots/web-users.png)
> *Screenshot: User management page showing accounts and role assignments*

### Change Password

Users can change their own password from this page. Admins can also force a password change on other users — when flagged, the user is redirected to the change password page on their next login.

## Dark Mode

The web UI supports dark mode. Use the theme toggle in the navigation header to switch between light and dark themes. The preference is stored in your browser.

## Real-Time Updates

The web UI uses Server-Sent Events (SSE) to push live data from the server to the browser. Tag values, connection status, and log messages update automatically without polling or manual refresh.

## Security Considerations

- **HTTPS** — The built-in web server serves plain HTTP. For production deployments, place it behind a reverse proxy (nginx, Caddy, Traefik) that terminates TLS.
- **Authentication** — Sessions are cookie-based. Passwords are stored as bcrypt hashes in the configuration file.
- **Bind address** — By default the server binds to `0.0.0.0` (all interfaces). In environments where the server should only be reachable locally, set the host to `127.0.0.1`.
