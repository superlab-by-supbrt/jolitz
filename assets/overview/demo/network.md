# Network

All servers sit in 203.0.113.0/24 behind the office firewall;
ci.example.com reaches them over WireGuard. backup.example.com
is not managed by jolitz.

- Link: web1.example.com -> db1.example.com (postgresql 5432)
- Link: ci.example.com -> web1.example.com (deploy, ssh 2222)
- Link: db1.example.com -> backup.example.com (pgbackrest, ssh)
