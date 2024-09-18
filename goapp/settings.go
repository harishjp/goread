package goread

import (
	"time"
)

const (
	UpdateMin         = time.Minute * 20
	UpdateMax         = time.Hour * 12
	UpdateDefault     = time.Hour * 3
	UpdateFraction    = 0.5
	UpdateJitter      = time.Minute * 3
	UpdateLongFactor  = 20
	NewIntervalWeight = 0.2
)
