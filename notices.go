// SPDX-License-Identifier: AGPL-3.0-only

package main

import _ "embed"

// thirdPartyNotices is THIRD-PARTY-NOTICES.md, compiled into the binary.
//
// Embedded rather than shipped as a file beside it, because the runtime
// image is distroless: it contains this binary and nothing else, so a
// notices file that lived on disk would simply not be in the artefact
// anyone actually receives. MIT, BSD-3-Clause, ISC and Apache-2.0 all
// require their copyright notices and licence texts to accompany a
// *binary* distribution, and Apache-2.0 s4(d) requires any NOTICE file
// to be passed along -- embedding is what makes that literally true
// here.
//
// Served by internal/api and linked from the About dialog, so a user of
// a running instance can read them without having to obtain the source
// separately. Generated and CI-verified; see
// tools/licenses/generate-notices.mjs.
//
//go:embed THIRD-PARTY-NOTICES.md
var thirdPartyNotices string
