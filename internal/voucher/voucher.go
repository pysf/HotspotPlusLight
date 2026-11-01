// Package voucher will generate batches and print data (BR-15/16/23). Phase 0 skeleton.
package voucher

import "hotspotpluslight/internal/clock"

type Service struct{ clock clock.Clock }

func New(clk clock.Clock) *Service { return &Service{clock: clk} }
