# Open Turkey

A command-line productivity blocker for Linux, backed by a `systemd` service.

Open Turkey is an open-source, Linux-native alternative to [Cold Turkey](https://getcoldturkey.com/): it blocks distracting websites and applications and keeps them blocked, even against your own attempts to undo it on impulse. Where Cold Turkey is paid and centered on Windows/macOS, Open Turkey is free, built in Go, and designed around the way Linux actually enforces policy.

It organizes blocking into **blocks** — named groups of:

- **Sites** (domains, e.g. `youtube.com`)
- **Apps** (process names, e.g. `discord`, `telegram-desktop`)

When you **activate** a block, a daemon continuously ensures the blocking layers stay applied.

Blocks can also have a **daily time limit**. A limited block stays available while it still has time left for the local day; Open Turkey monitors network traffic to the block's sites, counts traffic activity as usage, and automatically starts blocking the block once the daily quota is exhausted.

## How it works — 4 enforcement layers

Open Turkey doesn't rely on a single, easily-bypassed mechanism. Each active block is enforced on four independent layers, and a `systemd` daemon re-applies them every 5 seconds if anything is tampered with:

1. **`/etc/hosts`** — resolves blocked domains to `0.0.0.0` (affects every program, not just browsers).
2. **Firewall (iptables)** — blocks network connections to the blocked sites.
3. **Browser enterprise policies** — managed `URLBlocklist`/`WebsiteFilter` policies for Firefox, Chromium, Google Chrome and Brave. The user cannot disable these from inside the browser, not even in private/incognito mode.
4. **Process kill** — terminates blocked apps that are running (SIGKILL).

Because the hosts and firewall layers act below the browser, blocking works even for browsers installed via Snap or Flatpak (where the system-wide browser policy file may not be read).

## Installation

Prerequisites:

- Linux with `systemd`
- `iptables` available
- Go (to build), at `/usr/local/go/bin` or otherwise on your `PATH`

Recommended:

```bash
sudo ./install.sh
```

This:

- compiles the binary (`CGO_ENABLED=0`, pure-Go SQLite via `modernc.org/sqlite`)
- installs `open-turkey` (a wrapper) and `open-turkey-bin` (the real binary) into `/usr/local/bin`
- adds a rule in `/etc/sudoers.d/open-turkey` so it runs without a password prompt
- installs and starts the `systemd` service `open-turkey.service`
- creates the database at `/var/lib/open-turkey/open-turkey.db`

The installed program is independent of the source directory — once installed, you can remove the project folder and it keeps working.

## Concepts

- **Block**: a set of sites/apps you want to block together.
- **Active**: a block that is currently in effect (the system is enforcing it).
- **Lock (`--lock`)**: when enabled, prevents deactivation via `stop`. The only way to deactivate a locked block is `unlock`, which requires completing a typing challenge — deliberate friction to outlast an impulse.

## Commands

### 1) Create and configure a block

```bash
open-turkey block create social-media
open-turkey block add-site social-media instagram.com x.com facebook.com
open-turkey block add-app  social-media discord telegram
```

Show details:

```bash
open-turkey block info social-media
```

List all blocks:

```bash
open-turkey block list
```

### 2) Activate / deactivate

Activate:

```bash
open-turkey start social-media
```

Activate with a lock:

```bash
open-turkey start social-media --lock
```

Show status:

```bash
open-turkey status
```

Deactivate (only if not locked):

```bash
open-turkey stop social-media
```

Unlock a locked block (this also **deactivates** it):

```bash
open-turkey unlock social-media
```

### 3) Daily time limits

Configure a block to allow up to 30 minutes of site traffic per local day:

```bash
open-turkey limit set social-media --daily 30m
open-turkey start social-media --lock
```

With a daily limit configured, you do not need to run a command to unlock time. While the block is active and still has quota left, its sites remain available and Open Turkey installs firewall counting rules. When traffic to those sites is detected, the daemon counts the block as active for 60 seconds. New traffic extends that activity window; once the daily quota is exhausted, the normal blocking layers are applied until the next local day.

Show limit usage:

```bash
open-turkey limit status
open-turkey limit status social-media
```

Remove a limit:

```bash
open-turkey limit remove social-media
```

Notes:

- The first version measures IPv4 network traffic, not browser tabs. Background traffic to a limited domain counts the same as deliberate use.
- Daily limits are meant for site blocks. A limited block must contain at least one site.
- Apps inside a limited block are allowed while the site quota remains and are killed once the quota is exhausted.

### 4) Editing a block's lists

Remove a domain from a block:

```bash
open-turkey block remove-site social-media instagram.com
```

Notes:

- If the block is **active**, Open Turkey re-applies the layers so the change takes effect immediately.
- If the block is **active and locked**, you **cannot** edit its sites/apps. Run `open-turkey unlock <block>` first (which deactivates it), make your changes, then activate again.

Remove an app (process) from a block:

```bash
open-turkey block remove-app social-media discord
```

Remove an entire block (must be inactive):

```bash
open-turkey block remove social-media
```

## Service (daemon) and logs

The daemon is managed by `systemd`:

```bash
systemctl status open-turkey
systemctl restart open-turkey
journalctl -u open-turkey -f
```

## Uninstall

```bash
sudo make uninstall
```

## License

MIT — see [LICENSE](LICENSE).
