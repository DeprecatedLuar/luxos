// Package config owns the shape of CONFIG_DIR: where host, machine and user
// folders live, the files luxos creates or syncs there, the scope links, and
// writing files as their owner. It never interprets a unit; what a unit is
// and which units a host builds belong to internal/modules. Every directory is
// a parameter and output goes through a Reporter; nothing here resolves paths
// or prints directly.
package config
