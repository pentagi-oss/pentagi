//go:build generate
// +build generate

// This file maintains development dependencies required for code generation.
// It ensures that gqlgen and related tools remain in go.mod.
package graph

import (
	_ "github.com/99designs/gqlgen"
	_ "github.com/99designs/gqlgen/graphql/introspection"
)
