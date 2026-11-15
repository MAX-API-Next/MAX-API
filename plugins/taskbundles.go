// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// The embedded scripts are unmodified QuantumNous official plugins,
// separately licensed under Apache-2.0; see tasks/LICENSE and SOURCES.json.
package plugins

import "embed"

// TaskSources contains immutable official sources. Nothing is installed or
// activated automatically; an administrator must select and confirm a version.
//
//go:embed tasks/*/*/plugin.js SOURCES.json tasks/LICENSE runtime-licenses/*
var TaskSources embed.FS
