package cli

import "errors"

// ErrNotImplemented is returned by subcommands that have been scaffolded
// but whose real implementation lands in a later phase of IMPL-0001.
var ErrNotImplemented = errors.New("not implemented")
