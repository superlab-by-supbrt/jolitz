package parse

// The fixtures of tests/memory-tools-test.sh, copied verbatim so
// the Go parser and the awk of bin/jolitz-overview read the same
// bytes. The <script> headline and the quote/backslash entry are
// part of the contract: they must survive parsing unchanged and
// leave the templates escaped.

const web1Memory = `# web1.example.com
- IP: 203.0.113.10
- OS: Debian 12 (Bookworm)
- Distro family: debian
- Tags: prod, web
- SSH port: 2222
- Owner: alice
- Services: nginx 1.26, postgresql 17
- Last connected: 2026-10-28
- Database password: in /etc/app/db.env (0600 root)
`

const web1Changelog = `[2026-06-11 09:50] [alice as root]
app.example.com now deploys blue/green <script>alert(1)</script>
— because deploys interrupted visitors
  Detail: templated app@.service (blue :4003,
  green :4005).
  Rollback: re-enable app.service, restore vhost
  backup from /var/backups/jolitz/.
  Verify: 135 probes, all 200.
  Flags: migrations must now stay
  backward-compatible.
[2026-06-12 08:00] [alice as root] nginx reloaded (auto, policy)
[2026-06-13 08:00] [alice as root] quoted "value" and back\slash
`

const db1Memory = `# db1.example.com
- IP: 203.0.113.20
- OS: Debian 11 (Bullseye)
- Distro family: debian
- Tags: prod, Database
- Last connected: 2025-01-01
`

const db1Todo = `# To-do
- [x] Back up pg_hba.conf
- [ ] Upgrade to Debian 12
`

const db1Changelog = `[2025-01-01 10:00] [bob as root] PostgreSQL 15 installed
[2025-01-02] forgot the timestamp format
`

const macMemory = `# mac.local
- OS: macOS 15.1 Sequoia
- Mode: local
- Last connected: yesterday
`

const network = `# Network
All servers sit in one VLAN; db1 is reached over the private IP.
- Link: web1.example.com -> db1.example.com (postgresql 5432)
`

const auditNew = `{"date":"2026-10-28 09:00","hosts":{
 "web1.example.com":{"sshd.PermitRootLogin":"no","time.ntp":"chrony"},
 "db1.example.com":{"sshd.PermitRootLogin":"yes","time.ntp":"unknown(needs-root)"}},
 "skipped":{"mac.local":"macOS not covered"}}
`
