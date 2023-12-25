//go:build !linux && !openbsd && !freebsd

/* SPDX-License-Identifier: MIT
 *
  * Copyright (C) 2017-2023 WireGuard LLC. All Rights Reserved.
  * Copyright (C) 2023 Synthesis Labs. All Rights Reserved.
 */

package conn

func (s *StdNetBind) SetMark(mark uint32) error {
	return nil
}
