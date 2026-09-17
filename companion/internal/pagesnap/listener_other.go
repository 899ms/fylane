//go:build !darwin && !linux && !windows

package pagesnap

import "context"

const supported = false

func listenerPIDs(context.Context, int) ([]int, error) { return nil, ErrUnsupported }
