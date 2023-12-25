//go:build !linux

package device

import (
	"github.com/syntlabs/cyanide-go/conn"
	"github.com/syntlabs/cyanide-go/rwcancel"
)

func (device *Device) startRouteListener(bind conn.Bind) (*rwcancel.RWCancel, error) {
	return nil, nil
}
