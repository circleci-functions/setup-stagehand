// Package helper embeds stagehand-ci.ts, the file users copy into their
// project, so the acceptance suite tests the very file the README points to.
package helper

import _ "embed"

// Source is stagehand-ci.ts.
//
//go:embed stagehand-ci.ts
var Source []byte
