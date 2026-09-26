package migrations

import "embed"

// Files contains every database migration shipped with iDeploy.
//
//go:embed *.sql
var Files embed.FS
