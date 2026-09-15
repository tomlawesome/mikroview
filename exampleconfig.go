// SPDX-License-Identifier: AGPL-3.0-only

package main

import _ "embed"

// exampleConfigYAML is deploy/config.example.yaml, compiled into the
// binary.
//
// Embedded for the same reason THIRD-PARTY-NOTICES.md is (see
// notices.go): the runtime image is distroless, so a file that only
// lived on disk at repo root would never reach anyone who actually runs
// this. config.MissingSettings (#1218) reads it to render the "N new
// settings are available" notice's exact paste-ready YAML -- the same
// text a developer reading the repo sees, never a second copy hand-kept
// in sync with it.
//
// internal/config cannot embed this itself: go:embed can only reach
// files in or below the embedding file's own directory, and
// deploy/config.example.yaml sits outside internal/config's. This file
// lives at the repo root instead, where deploy/ is a subdirectory, and
// passes the bytes down to internal/config as a plain argument.
//
//go:embed deploy/config.example.yaml
var exampleConfigYAML []byte
