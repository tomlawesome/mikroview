//go:build tools

// Package tools pins the gosec build used by security:gosec (#1453).
package tools

import _ "github.com/securego/gosec/v2/cmd/gosec"
