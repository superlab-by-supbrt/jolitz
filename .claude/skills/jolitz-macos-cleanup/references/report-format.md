# macOS Cleanup Report Format

Use this structure for the orphans mode.

```
## macOS Cleanup Report: hostname

Date: YYYY-MM-DD HH:MM
Scanned: 392 apps, 41 Team IDs, 3 paths unreadable

### Leftovers (verified)

AdGuard            176 MB   6 entries
Fantastical        110 MB  11 entries
OrbStack             1 MB   2 entries   root helper
TeamViewer           0 MB   3 receipts

### Needs a decision

Mozilla            Firefox is installed, holds its profiles
Arc                no app, name only

### Caches (rebuilt on demand)

ms-playwright      933 MB
pip                119 MB

### Kept

Microsoft AutoUpdate helper   used by Office
Viscosity helper              used by Viscosity
com.user.restart_exchangesyncd   own launchd job
```

For the uninstall mode:

```
## Uninstall: Brave Browser on hostname

App        /Applications/Brave Browser.app   559 MB
Running    no
Exact      6 entries   847 MB
By name    BraveSoftware   788 MB   profiles and passwords
Receipts   none
```

## Rules

- Group leftovers by app, largest first. One line per app.
- Put the verification result in the line when it matters:
  `root helper`, `launchd job loaded`, `holds passwords`.
- **Kept** lists only `vendor` and `custom` entries whose owner
  the verification found. Leave out `apple` and `installed`.
- List paths only when the user asks, or per app before removal.
- Sizes from the scanner's `bytes`, in MB or GB.
